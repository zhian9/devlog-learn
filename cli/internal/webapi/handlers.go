package webapi

import (
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"devlog-learn/internal/cache"
	"devlog-learn/internal/cursor"
	"devlog-learn/internal/digest"
	"devlog-learn/internal/export"
	"devlog-learn/internal/extract"
	"devlog-learn/internal/index"
	"devlog-learn/internal/ingest"
	"devlog-learn/internal/questions"
	"devlog-learn/internal/render"
	"devlog-learn/internal/search"
	"devlog-learn/internal/version"
)

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"version": version.ToolVersion,
	})
}

func (s *Server) handleConfig(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, ConfigResponse{
		ToolVersion: version.ToolVersion,
		CacheDir:    s.opts.CacheDir,
		CursorPath:  s.opts.CursorPath,
		OutDir:      s.opts.OutDir,
		Addr:        s.opts.Addr,
		LocalOnly:   true,
		ClaudeBin:   s.opts.ClaudeBin,
		CodexBin:    s.opts.CodexBin,
		ClaudeDir:   s.claudeDir(),
		CodexDir:    s.codexDir(),
	})
}

func (s *Server) handleStatus(w http.ResponseWriter, _ *http.Request) {
	infos, warnings := s.store.ListDocs()
	response := StatusResponse{
		ToolVersion:   version.ToolVersion,
		SchemaVersion: version.SchemaVersion,
		GeneratedAt:   time.Now().UTC(),
		Cache:         StatusCache{Dir: s.opts.CacheDir},
		Cursor:        StatusCursor{Path: s.opts.CursorPath},
		Outputs:       StatusOutputs{Dir: s.opts.OutDir},
		Knowledge:     map[string]int{},
	}
	response.Warnings = append(response.Warnings, warnings...)

	active := make([]cache.DocInfo, 0, len(infos))
	var newestDoc *time.Time
	for _, info := range infos {
		doc := info.Doc
		if doc == nil {
			continue
		}
		if doc.Lifecycle == extract.LifecycleStale {
			response.Cache.Stale++
			continue
		}
		active = append(active, info)
		response.Cache.Active++
		switch strings.ToLower(strings.TrimSpace(doc.ReviewStatus)) {
		case extract.ReviewApproved:
			response.Cache.Approved++
		case extract.ReviewRejected:
			response.Cache.Rejected++
		default:
			response.Cache.Pending++
		}
		kind := strings.TrimSpace(doc.KnowledgeType)
		if kind == "" {
			kind = "unknown"
		}
		response.Knowledge[kind]++
		if stat, err := os.Stat(info.Path); err == nil {
			if newestDoc == nil || stat.ModTime().After(*newestDoc) {
				modified := stat.ModTime()
				newestDoc = &modified
			}
		}
	}

	if info, err := os.Stat(s.opts.CacheDir); err == nil && info.IsDir() {
		response.Cache.Exists = true
		response.Cache.SizeBytes = dirSize(s.opts.CacheDir)
	}

	state, err := questions.Load(questions.Path(s.opts.CacheDir))
	if err != nil {
		response.Warnings = append(response.Warnings, err.Error())
		state = questions.New()
	}
	for _, entry := range questions.Collect(active, state, questions.Filter{Status: questions.StatusAll}) {
		if entry.Status == questions.StatusResolved {
			response.Questions.Resolved++
		} else {
			response.Questions.Open++
		}
	}

	response.Cursor = inspectCursor(s.opts.CursorPath, &response.Warnings)
	response.Outputs = inspectOutputs(s.opts.OutDir, newestDoc)
	response.Projects = search.ListProjects(active)
	response.Recent = search.Search(active, search.Options{Limit: 6})
	response.Providers = []StatusProvider{
		inspectProvider("claude-code", s.opts.ClaudeBin, s.claudeDir(), false),
		inspectProvider("codex", s.opts.CodexBin, s.codexDir(), true),
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleProjects(w http.ResponseWriter, _ *http.Request) {
	infos, warnings := s.store.ListActiveDocs()
	writeJSON(w, http.StatusOK, map[string]any{
		"projects": search.ListProjects(infos),
		"warnings": warnings,
	})
}

func (s *Server) handleDocs(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	infos, warnings := s.store.ListDocs()
	lifecycle := strings.ToLower(strings.TrimSpace(query.Get("lifecycle")))
	if lifecycle == "" {
		lifecycle = "active"
	}

	results := search.Search(infos, search.Options{
		Query:        strings.TrimSpace(query.Get("q")),
		Project:      strings.TrimSpace(query.Get("project")),
		Tags:         query["tag"],
		Review:       strings.TrimSpace(query.Get("review")),
		IncludeStale: lifecycle == "all" || lifecycle == "stale",
		All:          true,
		OutDir:       s.opts.OutDir,
	})

	if lifecycle == "stale" {
		filtered := results[:0]
		for _, item := range results {
			if item.Lifecycle == extract.LifecycleStale {
				filtered = append(filtered, item)
			}
		}
		results = filtered
	}

	switch strings.ToLower(strings.TrimSpace(query.Get("sort"))) {
	case "confidence":
		sort.SliceStable(results, func(i, j int) bool {
			if results[i].Confidence != results[j].Confidence {
				return results[i].Confidence > results[j].Confidence
			}
			return results[i].OccurredAt.After(results[j].OccurredAt)
		})
	case "title":
		sort.SliceStable(results, func(i, j int) bool {
			return strings.ToLower(results[i].Title) < strings.ToLower(results[j].Title)
		})
	}

	total := len(results)
	offset := positiveInt(query.Get("offset"), 0)
	limit := positiveInt(query.Get("limit"), 0)
	if offset > len(results) {
		offset = len(results)
	}
	results = results[offset:]
	if limit > 0 && limit < len(results) {
		results = results[:limit]
	}
	writeJSON(w, http.StatusOK, DocListResponse{Total: total, Docs: results, Warnings: warnings})
}

func (s *Server) handleDoc(w http.ResponseWriter, r *http.Request) {
	id, ok := docID(w, r)
	if !ok {
		return
	}
	doc, err := s.store.LoadDoc(id)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, os.ErrNotExist) {
			status = http.StatusNotFound
		}
		writeError(w, status, err.Error())
		return
	}

	allInfos, warnings := s.store.ListDocs()
	docs := make([]*extract.Doc, 0, len(allInfos))
	for _, info := range allInfos {
		if info.Doc != nil {
			docs = append(docs, info.Doc)
		}
	}
	markdownPath := ""
	for _, plan := range render.PlanDocs(docs) {
		if plan.Doc.ID == doc.ID {
			markdownPath = plan.Path
			break
		}
	}
	learnPath := ""
	for _, plan := range render.LearnPlans(docs) {
		if plan.Doc.ID == doc.ID {
			learnPath = plan.Path
			break
		}
	}

	activeInfos, _ := s.store.ListActiveDocs()
	activeDocs := make([]*extract.Doc, 0, len(activeInfos))
	for _, info := range activeInfos {
		if info.Doc != nil {
			activeDocs = append(activeDocs, info.Doc)
		}
	}

	state, qerr := questions.Load(questions.Path(s.opts.CacheDir))
	if qerr != nil {
		warnings = append(warnings, qerr.Error())
		state = questions.New()
	}
	entries := questions.Collect(activeInfos, state, questions.Filter{
		DocID:  doc.ID,
		Status: questions.StatusAll,
	})

	writeJSON(w, http.StatusOK, DocDetailResponse{
		Doc:               doc,
		MarkdownPath:      markdownPath,
		LearnMarkdownPath: learnPath,
		Related:           index.RelatedDocs(doc, activeDocs),
		Questions:         entries,
		Warnings:          warnings,
	})
}

func (s *Server) handleMarkdown(w http.ResponseWriter, r *http.Request) {
	id, ok := docID(w, r)
	if !ok {
		return
	}
	doc, err := s.store.LoadDoc(id)
	if err != nil {
		writeError(w, statusForLoad(err), err.Error())
		return
	}
	profile := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("profile")))
	var content string
	switch profile {
	case "", export.ProfileAgent:
		content = render.Markdown(doc)
	case export.ProfileLearning:
		content = render.LearnMarkdown(doc)
	default:
		writeError(w, http.StatusBadRequest, "profile must be agent or learning")
		return
	}
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(content))
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	id, ok := docID(w, r)
	if !ok {
		return
	}
	segment, err := s.store.LoadSegment(id)
	if err != nil {
		writeError(w, statusForLoad(err), err.Error())
		return
	}
	requested := parseSeqs(r.URL.Query()["seq"])
	response := EventsResponse{
		SegmentID:  segment.ID,
		Source:     segment.Source,
		SessionID:  segment.SessionID,
		Project:    segment.Project,
		Title:      segment.Title,
		Start:      segment.Start,
		End:        segment.End,
		EventCount: segment.EventCount,
		Events:     []EventView{},
	}
	const maxEvents = 500
	for _, event := range segment.Events {
		if len(requested) > 0 && !requested[event.Seq] {
			continue
		}
		response.Events = append(response.Events, eventView(event))
		if len(response.Events) >= maxEvents {
			break
		}
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleReview(w http.ResponseWriter, r *http.Request) {
	id, ok := docID(w, r)
	if !ok {
		return
	}
	var request ReviewRequest
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	lock, ok := s.acquireCacheWriteLock(w)
	if !ok {
		return
	}
	defer lock.Close()
	s.write.Lock()
	defer s.write.Unlock()

	doc, err := s.store.LoadDoc(id)
	if err != nil {
		writeError(w, statusForLoad(err), err.Error())
		return
	}
	if status := strings.TrimSpace(request.Status); status != "" {
		if !extract.ValidReviewStatus(status) {
			writeError(w, http.StatusBadRequest, "status must be pending, approved, or rejected")
			return
		}
		doc.ReviewStatus = extract.NormalizeReviewStatus(status)
	}
	if request.Note != nil {
		doc.ReviewNote = strings.TrimSpace(*request.Note)
	}
	if err := s.store.SaveDoc(doc); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, doc)
}

func (s *Server) handleQuestions(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	allInfos, listWarnings := s.store.ListActiveDocs()
	state, err := questions.Load(questions.Path(s.opts.CacheDir))
	warnings := append([]string{}, listWarnings...)
	if err != nil {
		warnings = append(warnings, err.Error())
		state = questions.New()
	}
	status := strings.ToLower(strings.TrimSpace(query.Get("status")))
	if status == "" {
		status = questions.StatusOpen
	}
	entries := questions.Collect(allInfos, state, questions.Filter{
		Project: strings.TrimSpace(query.Get("project")),
		Review:  strings.TrimSpace(query.Get("review")),
		DocID:   strings.TrimSpace(query.Get("doc")),
		Status:  status,
	})
	all := questions.Collect(allInfos, state, questions.Filter{Status: questions.StatusAll})
	response := QuestionsResponse{Entries: entries, Warnings: warnings}
	for _, entry := range all {
		if entry.Status == questions.StatusResolved {
			response.Resolved++
		} else {
			response.Open++
		}
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleResolveQuestion(w http.ResponseWriter, r *http.Request) {
	s.mutateQuestion(w, r, true)
}

func (s *Server) handleReopenQuestion(w http.ResponseWriter, r *http.Request) {
	s.mutateQuestion(w, r, false)
}

func (s *Server) mutateQuestion(w http.ResponseWriter, r *http.Request, resolve bool) {
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing question id")
		return
	}
	var request NoteRequest
	if r.ContentLength != 0 {
		if err := decodeJSON(r, &request); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	lock, ok := s.acquireCacheWriteLock(w)
	if !ok {
		return
	}
	defer lock.Close()
	s.write.Lock()
	defer s.write.Unlock()

	path := questions.Path(s.opts.CacheDir)
	state, err := questions.Load(path)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if resolve {
		infos, _ := s.store.ListActiveDocs()
		var target *questions.Entry
		for _, entry := range questions.Collect(infos, state, questions.Filter{Status: questions.StatusAll}) {
			if entry.ID == id {
				copy := entry
				target = &copy
				break
			}
		}
		if target == nil {
			writeError(w, http.StatusNotFound, "unknown question id")
			return
		}
		state.Resolve(*target, strings.TrimSpace(request.Note), time.Now().UTC())
	} else if !state.Reopen(id) {
		writeError(w, http.StatusNotFound, "question is not resolved")
		return
	}
	if err := state.Save(path, time.Now().UTC()); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "resolved": resolve})
}

func (s *Server) handleExport(w http.ResponseWriter, r *http.Request) {
	var request ExportRequest
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	profile := strings.ToLower(strings.TrimSpace(request.Profile))
	switch profile {
	case "", "both":
		profile = "both"
	case export.ProfileAgent, export.ProfileLearning:
	default:
		writeError(w, http.StatusBadRequest, "profile must be agent, learning, or both")
		return
	}

	lock, ok := s.acquireCacheWriteLock(w)
	if !ok {
		return
	}
	defer lock.Close()
	s.write.Lock()
	defer s.write.Unlock()

	response := ExportResponse{Profile: profile, OutDir: s.opts.OutDir}
	renderProfile := func(name string) error {
		report, err := export.RenderProfileWithOptions(s.store, s.opts.OutDir, export.RenderProfileOptions{
			IncludeStale: request.IncludeStale,
			Profile:      name,
			Prune:        request.Prune,
		})
		if err != nil {
			return err
		}
		response.Rendered += report.Written
		response.StaleSkipped += report.StaleSkipped
		response.Pruned += report.Pruned
		response.PruneSkipped += report.PruneSkipped
		response.Paths = append(response.Paths, report.Paths...)
		response.Warnings = append(response.Warnings, report.Warnings...)
		return nil
	}
	switch profile {
	case export.ProfileAgent:
		if err := renderProfile(export.ProfileAgent); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	case export.ProfileLearning:
		if err := renderProfile(export.ProfileLearning); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	default:
		if err := renderProfile(export.ProfileAgent); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if err := renderProfile(export.ProfileLearning); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	indexReport, warnings, err := export.BuildIndexWithOptions(s.store, s.opts.OutDir, time.Now().UTC(), index.WriteOptions{
		Prune: request.Prune,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.IndexDocs = indexReport.Docs
	response.IndexProjects = indexReport.Projects
	response.Pruned += indexReport.Pruned
	response.PruneSkipped += indexReport.PruneSkipped
	response.Warnings = append(response.Warnings, warnings...)

	if request.Digest {
		digestResult, digestErr := digest.Write(s.store, digest.Options{
			OutDir:  s.opts.OutDir,
			Project: request.DigestProject,
			Review:  request.DigestReview,
			Limit:   request.DigestLimit,
			Force:   request.ForceDigest,
		}, time.Now().UTC())
		if digestErr != nil {
			response.Warnings = append(response.Warnings, "AGENTS.md: "+digestErr.Error())
		} else {
			response.DigestPath = digestResult.Path
			response.DigestDocs = digestResult.Docs
			response.Warnings = append(response.Warnings, digestResult.Warnings...)
		}
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) claudeDir() string {
	if strings.TrimSpace(s.opts.ClaudeDir) != "" {
		return s.opts.ClaudeDir
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude", "projects")
}

func (s *Server) codexDir() string {
	if strings.TrimSpace(s.opts.CodexDir) != "" {
		return s.opts.CodexDir
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".codex", "sessions")
}

func inspectCursor(path string, warnings *[]string) StatusCursor {
	out := StatusCursor{Path: path}
	if strings.TrimSpace(path) == "" {
		return out
	}
	if _, err := os.Stat(path); err == nil {
		out.Exists = true
	}
	state, err := cursor.Load(path)
	if err != nil {
		*warnings = append(*warnings, err.Error())
		return out
	}
	out.Files = len(state.Files)
	for _, fp := range state.Files {
		if fp.Status != cursor.StatusOK {
			out.Errors++
		}
	}
	out.VersionsMatch = state.VersionsMatch()
	if !state.UpdatedAt.IsZero() {
		updated := state.UpdatedAt
		out.UpdatedAt = &updated
	}
	return out
}

func inspectOutputs(outDir string, newestDoc *time.Time) StatusOutputs {
	out := StatusOutputs{Dir: outDir}

	indexPath := filepath.Join(outDir, "index.json")
	if stat, err := os.Stat(indexPath); err == nil {
		out.IndexExists = true
		generated := stat.ModTime().UTC()
		if data, readErr := os.ReadFile(indexPath); readErr == nil {
			var parsed struct {
				GeneratedAt time.Time `json:"generated_at"`
			}
			if json.Unmarshal(data, &parsed) == nil && !parsed.GeneratedAt.IsZero() {
				generated = parsed.GeneratedAt
			}
		}
		out.IndexGeneratedAt = &generated
		out.IndexStale = newestDoc != nil && generated.Before(*newestDoc)
	}

	digestPath := filepath.Join(outDir, "AGENTS.md")
	if stat, err := os.Stat(digestPath); err == nil {
		out.DigestExists = true
		modified := stat.ModTime().UTC()
		out.DigestModifiedAt = &modified
		out.DigestStale = newestDoc != nil && modified.Before(*newestDoc)
	}
	return out
}

func inspectProvider(name, bin, dir string, codex bool) StatusProvider {
	out := StatusProvider{Name: name, Bin: bin, Dir: dir}
	if strings.TrimSpace(bin) != "" {
		if _, err := exec.LookPath(bin); err == nil {
			out.BinFound = true
		}
	}
	if info, err := os.Stat(dir); err == nil && info.IsDir() {
		out.DirExists = true
		out.SessionFiles = countSessionFiles(dir, codex)
	}
	return out
}

func countSessionFiles(root string, codex bool) int {
	count := 0
	_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return nil
		}
		if !strings.EqualFold(filepath.Ext(path), ".jsonl") {
			return nil
		}
		if codex && !strings.HasPrefix(filepath.Base(path), "rollout-") {
			return nil
		}
		count++
		return nil
	})
	return count
}

func dirSize(root string) int64 {
	var total int64
	_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return nil
		}
		if info, statErr := entry.Info(); statErr == nil {
			total += info.Size()
		}
		return nil
	})
	return total
}

func eventView(event ingest.Event) EventView {
	return EventView{
		Seq:           event.Seq,
		Kind:          string(event.Kind),
		Role:          event.Role,
		Time:          event.Time,
		Text:          event.Text,
		ToolName:      event.ToolName,
		ToolInput:     event.ToolInput,
		ToolInputText: event.ToolInputText,
		ToolResult:    event.ToolResult,
		IsError:       event.IsError,
		Truncated:     event.Truncated,
		Exec:          event.Exec,
	}
}

func parseSeqs(values []string) map[int]bool {
	out := map[int]bool{}
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			if n, err := strconv.Atoi(part); err == nil && n > 0 {
				out[n] = true
			}
		}
	}
	return out
}

func docID(w http.ResponseWriter, r *http.Request) (string, bool) {
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" || id == "." || id == ".." || filepath.Base(id) != id ||
		strings.ContainsAny(id, `/\`) {
		writeError(w, http.StatusBadRequest, "invalid document id")
		return "", false
	}
	return id, true
}

func statusForLoad(err error) int {
	if errors.Is(err, os.ErrNotExist) {
		return http.StatusNotFound
	}
	return http.StatusInternalServerError
}

func positiveInt(value string, fallback int) int {
	n, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || n < 0 {
		return fallback
	}
	return n
}
