package pipeline

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"devlog-learn/internal/cache"
	"devlog-learn/internal/cursor"
	"devlog-learn/internal/extract"
	"devlog-learn/internal/ingest"
	"devlog-learn/internal/prompts"
	"devlog-learn/internal/redact"
	"devlog-learn/internal/segment"
	"devlog-learn/internal/version"
)

// ClientFor creates (or returns) the LLM client for a provider. It is called
// sequentially before extraction starts; implementations need not be
// concurrency-safe themselves beyond Client.Complete.
type ClientFor func(ctx context.Context, source string) (extract.Client, error)

// Selection explicitly limits which prepared segments are extracted. Session
// selectors use "source:sessionID"; a bare session ID is accepted when it is
// unambiguous. Segment selectors are exact SegmentIDs. When a selection is
// present, RunOptions.Segments is ignored, cursor skipping is disabled so every
// candidate can be matched, and cache entries are still reused.
type Selection struct {
	Sessions []string
	Segments []string
}

// Empty reports whether no explicit selector was supplied.
func (s Selection) Empty() bool {
	return len(s.Sessions) == 0 && len(s.Segments) == 0
}

// ProgressEvent is an optional, serialized progress notification emitted by
// Run. The local web UI uses it to stream job status; CLI callers leave
// OnProgress nil and see no behavior change.
type ProgressEvent struct {
	Phase     string `json:"phase"` // scan, selected, segment
	Total     int    `json:"total"`
	Done      int    `json:"done"`
	SegmentID string `json:"segment_id,omitempty"`
	Title     string `json:"title,omitempty"`
	Source    string `json:"source,omitempty"`
	Project   string `json:"project,omitempty"`
	Cached    bool   `json:"cached,omitempty"`
	Failed    bool   `json:"failed,omitempty"`
	Error     string `json:"error,omitempty"`
}

// RunOptions controls incremental extraction.
type RunOptions struct {
	Load        LoadOptions
	Segment     segment.Options
	Redact      redact.Config
	Build       extract.BuildOptions
	CacheDir    string
	CursorPath  string
	Full        bool // full scan, ignore cursor; cache may still be reused
	Force       bool // implies Full; ignore cache and call the model again
	Concurrency int
	Segments    int // 0 means every prepared segment; ignored when Selection is set
	Selection   Selection

	ClientFor  ClientFor
	RawDir     string
	KeepRaw    bool
	Logger     *slog.Logger
	Now        func() time.Time
	OnProgress func(ProgressEvent)

	// CacheLock is an optional, already-acquired exclusive cache lock. The
	// caller that supplied it remains responsible for releasing it. When nil,
	// Run acquires and releases the lock itself.
	CacheLock *cache.Lock
}

// RunReport is one segment's outcome, in deterministic discovery order.
type RunReport struct {
	Segment     segment.Segment
	Bytes       int
	Doc         *extract.Doc
	Cached      bool
	CacheKey    string
	Repairs     int
	PromptBytes int
	Duration    time.Duration
	Usage       extract.Usage
	Err         error
}

// RunResult aggregates one incremental extraction run.
type RunResult struct {
	Files          int
	Skipped        int
	Parsed         int
	Excluded       int
	FailedFiles    int
	Segments       int
	Selected       int
	CacheHits      int
	Extracted      int
	FailedSegments int

	Usage               extract.Usage
	UsageUnknownReports int

	Reports       []RunReport
	Drops         []Drop
	Warnings      []string
	Redactions    map[string]int
	StaleMarked   []string
	Restored      []string
	SourceMissing []string
}

type runJob struct {
	file   *runFile
	ps     PreparedSegment
	source string
}

type runFile struct {
	key           string
	ref           ingest.FileRef
	fp            cursor.Fingerprint
	segments      []PreparedSegment
	segmentIDs    []string
	selectedIndex []int
}

type providerRuntime struct {
	meta      cache.Meta
	extractor *extract.Extractor
	err       error
}

type runner struct {
	opt      RunOptions
	store    *cache.Store
	redactor *redact.Redactor
	log      *slog.Logger
	now      time.Time
	docs     []cache.DocInfo
	docsByID map[string]*cache.DocInfo
	result   *RunResult

	progressMu   sync.Mutex
	progressDone int64
}

func (r *runner) emitProgress(event ProgressEvent) {
	if r.opt.OnProgress == nil {
		return
	}
	r.progressMu.Lock()
	defer r.progressMu.Unlock()
	r.opt.OnProgress(event)
}

// Run performs cursor-aware discovery, parsing, segmentation, redaction, cache
// lookup, concurrent extraction, and stale-document maintenance. It never
// updates a file's cursor entry to "ok" unless every expected segment finished.
func Run(ctx context.Context, opt RunOptions) (*RunResult, error) {
	opt = normalizeRunOptions(opt)
	if opt.CacheLock == nil {
		lock, err := cache.TryLock(opt.CacheDir)
		if err != nil {
			return nil, err
		}
		defer lock.Close()
		opt.CacheLock = lock
	}
	explicitSelection := !opt.Selection.Empty()
	redactor, err := redact.New(opt.Redact)
	if err != nil {
		return nil, err
	}
	store, err := cache.Open(opt.CacheDir)
	if err != nil {
		return nil, err
	}
	if opt.RawDir == "" {
		opt.RawDir = store.RawDir()
	}

	r := &runner{
		opt:      opt,
		store:    store,
		redactor: redactor,
		log:      opt.Logger,
		now:      opt.Now().UTC(),
		result: &RunResult{
			Drops:      []Drop{},
			Warnings:   []string{},
			Redactions: map[string]int{},
		},
	}
	var errs []error

	r.emitProgress(ProgressEvent{Phase: "scan"})

	state, cursorErr := cursor.Load(opt.CursorPath)
	if cursorErr != nil {
		r.result.Warnings = append(r.result.Warnings, fmt.Sprintf("cursor: %v; starting with an empty cursor", cursorErr))
		state = cursor.NewState()
	}
	versionsMatch := state.VersionsMatch()

	disc := discover(opt.Load)
	r.result.Files = len(disc.files)
	r.result.Warnings = append(r.result.Warnings, disc.warnings...)
	if disc.err != nil {
		errs = append(errs, disc.err)
	}

	docs, docWarnings := store.ListDocs()
	r.docs = docs
	r.result.Warnings = append(r.result.Warnings, docWarnings...)
	r.docsByID = make(map[string]*cache.DocInfo, len(docs))
	for i := range r.docs {
		info := &r.docs[i]
		r.docsByID[info.ID] = info
	}

	var files []*runFile
	var jobs []runJob

	for _, file := range disc.files {
		if err := ctx.Err(); err != nil {
			errs = append(errs, err)
			break
		}
		ref := file.ref
		key := cursor.NormalizePath(ref.Path)
		sha, size, modNS, hashErr := cursor.HashFile(ref.Path)
		fp := cursor.Fingerprint{
			Source:           ref.Source,
			Size:             size,
			ModTimeNS:        modNS,
			SHA256:           sha,
			ParserVersion:    version.ParserVersion,
			SegmenterVersion: version.SegmenterVersion,
			// Keep the previous association on failure so a later deletion can
			// still mark the file's documents source_missing.
			SegmentIDs: append([]string(nil), state.Files[key].SegmentIDs...),
		}
		if hashErr != nil {
			r.result.FailedFiles++
			errs = append(errs, fmt.Errorf("hash session file %q: %w", ref.Path, hashErr))
			state.MarkError(key, fp, hashErr, r.now)
			continue
		}
		if !opt.Full && !explicitSelection && versionsMatch && state.CanSkip(key, sha) {
			r.result.Skipped++
			continue
		}

		sess, parseErr := file.source.Parse(ref, ingest.ParseOptions{
			IncludeSubagent: opt.Load.IncludeSubagents,
		})
		if parseErr != nil {
			r.result.FailedFiles++
			errs = append(errs, parseErr)
			state.MarkError(key, fp, parseErr, r.now)
			continue
		}
		r.result.Parsed++
		r.result.Warnings = append(r.result.Warnings, sessionWarnings(sess)...)

		if sess.Excluded {
			r.result.Excluded++
			// Keep the previous segment association so deletion detection still
			// works; exclusion is a runtime option, not proof the docs vanished.
			state.MarkOK(key, fp, fp.SegmentIDs, r.now)
			continue
		}

		segs, drops := segment.Split(sess, opt.Segment)
		for _, drop := range drops {
			r.result.Drops = append(r.result.Drops, makeDrop(drop))
		}

		work := &runFile{key: key, ref: ref, fp: fp}
		previousIDs := append([]string(nil), state.Files[key].SegmentIDs...)
		for _, seg := range segs {
			work.segmentIDs = append(work.segmentIDs, seg.ID)
		}

		for _, seg := range segs {
			if err := ctx.Err(); err != nil {
				break
			}
			redacted, stats := r.redactor.Segment(seg)
			mergeRedactions(r.result.Redactions, stats)

			if !segment.HasContent(redacted) {
				r.result.Drops = append(r.result.Drops, Drop{
					Source:        seg.Source,
					Project:       seg.Project,
					SessionID:     seg.SessionID,
					SegmentID:     seg.ID,
					Start:         seg.Start,
					End:           seg.End,
					Reasons:       []segment.DropReason{segment.DropEmptyAfterRedact},
					Detail:        "segment content is empty after redaction",
					EventCount:    seg.EventCount,
					UserTextCount: seg.UserTextCount,
				})
				continue
			}

			segment.SetHash(&redacted)
			ps := PreparedSegment{
				Segment:    redacted,
				Redactions: stats,
				Bytes:      segment.ByteSize(redacted),
			}
			work.segments = append(work.segments, ps)
			if err := r.store.SaveSegment(redacted); err != nil {
				errs = append(errs, err)
			}
		}
		r.result.Segments += len(work.segments)
		if err := ctx.Err(); err != nil {
			// A canceled run must not derive stale state from a partial split.
			errs = append(errs, err)
			break
		}

		// Stale maintenance is allowed once the file parsed and segmented
		// successfully, independent of whether extraction later fails. The
		// cursor's previous SegmentIDs are the authoritative file association;
		// Doc.Provenance.LogFile may be redacted for privacy.
		r.applyFileLifecycle(previousIDs, work.segmentIDs)

		files = append(files, work)
	}

	// Build the global job list in deterministic order, then honour --segments.
	for _, work := range files {
		source := work.ref.Source
		if source == "" && !strings.EqualFold(strings.TrimSpace(opt.Load.Provider), "all") {
			source = strings.ToLower(strings.TrimSpace(opt.Load.Provider))
		}
		for _, ps := range work.segments {
			jobs = append(jobs, runJob{file: work, ps: ps, source: source})
		}
	}
	selected, selErr := selectJobs(jobs, opt.Selection, opt.Segments)
	if selErr != nil {
		return r.result, errors.Join(append(errs, selErr)...)
	}
	for i, job := range selected {
		job.file.selectedIndex = append(job.file.selectedIndex, i)
	}
	r.result.Selected = len(selected)
	r.emitProgress(ProgressEvent{Phase: "selected", Total: len(selected)})

	providers := r.prepareProviders(ctx, selected)

	reports := make([]RunReport, len(selected))
	done := make([]bool, len(selected))
	workers := min(opt.Concurrency, len(selected))
	var wg sync.WaitGroup
	var next int64 = -1
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				if ctx.Err() != nil {
					return
				}
				index := int(atomic.AddInt64(&next, 1))
				if index >= len(selected) {
					return
				}
				reports[index], done[index] = r.process(ctx, selected[index], providers)
				report := reports[index]
				event := ProgressEvent{
					Phase:     "segment",
					Total:     len(selected),
					Done:      int(atomic.AddInt64(&r.progressDone, 1)),
					SegmentID: report.Segment.ID,
					Title:     report.Segment.Title,
					Source:    report.Segment.Source,
					Project:   report.Segment.Project,
					Cached:    report.Cached,
					Failed:    report.Err != nil,
				}
				if report.Err != nil {
					event.Error = report.Err.Error()
				}
				r.emitProgress(event)
			}
		}()
	}
	wg.Wait()

	for i := range reports {
		if !done[i] && reports[i].Err == nil {
			if err := ctx.Err(); err != nil {
				reports[i].Err = err
			} else {
				reports[i].Err = errors.New("segment was not processed")
			}
		}
	}

	r.result.Reports = reports
	for _, report := range reports {
		r.result.Usage.Add(report.Usage)
		switch {
		case report.Err != nil:
			r.result.FailedSegments++
			errs = append(errs, report.Err)
		case report.Cached:
			r.result.CacheHits++
		case report.Doc != nil:
			r.result.Extracted++
			if !report.Usage.Known {
				r.result.UsageUnknownReports++
			}
		}
	}

	// Cursor updates: only a fully successful file becomes "ok". Files with
	// unselected segments are also left as errors so nothing is silently lost.
	for _, work := range files {
		r.updateCursor(state, work, reports, explicitSelection)
	}
	if err := state.Save(opt.CursorPath, r.now); err != nil {
		errs = append(errs, err)
	}

	r.detectSourceMissing(disc.scanned, state.Files)

	return r.result, errors.Join(errs...)
}

func normalizeRunOptions(opt RunOptions) RunOptions {
	if opt.Concurrency <= 0 {
		opt.Concurrency = 2
	}
	if opt.Segments < 0 {
		opt.Segments = 0
	}
	if opt.Force {
		opt.Full = true
	}
	if opt.Logger == nil {
		opt.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	if opt.Now == nil {
		opt.Now = time.Now
	}
	if opt.CacheDir == "" {
		opt.CacheDir = filepath.Join(".devlog-cache")
	}
	if opt.CursorPath == "" {
		opt.CursorPath = ".devlog-cursor.json"
	}
	return opt
}

func (r *runner) prepareProviders(ctx context.Context, jobs []runJob) map[string]*providerRuntime {
	providers := map[string]*providerRuntime{}
	rulesHash := r.redactor.Hash()
	metaBase := cache.Meta{
		SchemaVersion:    extract.SchemaVersion,
		ParserVersion:    version.ParserVersion,
		SegmenterVersion: version.SegmenterVersion,
		RedactionVersion: version.RedactionVersion,
		BuildVersion:     version.BuildVersion,
		PromptVersion:    prompts.PromptVersion,
		PromptHash:       prompts.Hash(),
		RulesHash:        rulesHash,
	}
	for _, job := range jobs {
		if _, ok := providers[job.source]; ok {
			continue
		}
		if r.opt.ClientFor == nil {
			providers[job.source] = &providerRuntime{err: errors.New("no LLM client factory configured")}
			continue
		}
		client, err := r.opt.ClientFor(ctx, job.source)
		if err != nil {
			providers[job.source] = &providerRuntime{err: err}
			continue
		}
		meta := metaBase
		meta.Provider = client.Name()
		meta.Model = clientModel(client)
		meta.CLIVersion = "unknown"
		if ctxErr := ctx.Err(); ctxErr != nil {
			providers[job.source] = &providerRuntime{err: ctxErr}
			continue
		}
		if value, err := client.Version(ctx); err == nil && strings.TrimSpace(value) != "" {
			meta.CLIVersion = strings.TrimSpace(value)
		}
		extractor := extract.NewExtractor(client, extract.ExtractorOptions{
			Build:    r.opt.Build,
			Redactor: r.redactor,
			RawDir:   r.opt.RawDir,
			KeepRaw:  r.opt.KeepRaw,
			Logger:   r.log,
			Now:      r.opt.Now,
		})
		providers[job.source] = &providerRuntime{meta: meta, extractor: extractor}
	}
	return providers
}

func clientModel(client extract.Client) string {
	if named, ok := client.(interface{ Model() string }); ok {
		if value := strings.TrimSpace(named.Model()); value != "" {
			return value
		}
	}
	return "default"
}

func (r *runner) process(ctx context.Context, job runJob, providers map[string]*providerRuntime) (RunReport, bool) {
	seg := job.ps.Segment
	report := RunReport{Segment: seg, Bytes: job.ps.Bytes}
	if err := ctx.Err(); err != nil {
		report.Err = err
		return report, false
	}
	provider, ok := providers[job.source]
	if !ok || provider.err != nil {
		if provider != nil && provider.err != nil {
			report.Err = fmt.Errorf("prepare %s client: %w", job.source, provider.err)
		} else {
			report.Err = fmt.Errorf("no LLM client for provider %q", job.source)
		}
		return report, true
	}

	key := cache.Key(provider.meta, seg)
	report.CacheKey = key
	if !r.opt.Force {
		doc, err := r.store.Lookup(key, seg, provider.meta)
		if err == nil {
			report.Doc = doc
			report.Cached = true
			if _, ensureErr := r.store.EnsureDoc(doc); ensureErr != nil {
				r.log.Warn("cannot materialize cached doc", "segment", seg.ID, "error", ensureErr)
			}

			return report, true
		}
		r.log.Debug("cache miss", "segment", seg.ID, "key", key, "reason", err)
	}

	started := time.Now()
	result, err := provider.extractor.Extract(ctx, seg)
	if err != nil {
		report.Err = err
		return report, true
	}
	report.Doc = result.Doc
	report.Repairs = result.Repairs
	report.PromptBytes = result.Prompt.Bytes
	report.Duration = time.Since(started)
	report.Usage = result.Usage
	if result.Doc != nil {
		result.Doc.Provenance.CacheKey = key
	}
	if err := r.store.Save(key, seg, provider.meta, result.Doc, r.now); err != nil {
		report.Err = err
		return report, true
	}
	// The cache entry above is canonical extraction output. Human review state
	// belongs to the docs/ file, so merge it afterwards and before SaveDoc.
	r.mergeReviewState(result.Doc)
	if err := r.store.SaveDoc(result.Doc); err != nil {
		report.Err = err
		return report, true
	}
	return report, true
}

// mergeReviewState preserves a human decision only when the segment content is
// unchanged. Doc.Normalize always resets review fields, so re-extraction of a
// changed segment must explicitly stay pending with an empty note.
func (r *runner) mergeReviewState(doc *extract.Doc) {
	if doc == nil {
		return
	}
	info, ok := r.docsByID[doc.ID]
	if !ok || info.Doc == nil {
		return
	}
	old := info.Doc
	if old.Provenance.SegmentHash != doc.Provenance.SegmentHash {
		return
	}
	if extract.ValidReviewStatus(old.ReviewStatus) {
		doc.ReviewStatus = extract.NormalizeReviewStatus(old.ReviewStatus)
	}
	doc.ReviewNote = old.ReviewNote
}

func (r *runner) applyFileLifecycle(previousIDs, currentIDs []string) {
	current := make(map[string]bool, len(currentIDs))
	for _, id := range currentIDs {
		current[id] = true
	}
	seen := map[string]bool{}
	for _, id := range previousIDs {
		if current[id] || seen[id] {
			continue
		}
		seen[id] = true
		info, ok := r.docsByID[id]
		if !ok || info.Doc == nil || info.Doc.Lifecycle == extract.LifecycleStale {
			continue
		}
		doc := info.Doc
		staled := r.now
		doc.Lifecycle = extract.LifecycleStale
		doc.StaleReason = cache.StaleSegmentRemoved
		doc.StaledAt = &staled
		if err := r.store.SaveDoc(doc); err != nil {
			r.result.Warnings = append(r.result.Warnings, fmt.Sprintf("mark doc %s stale: %v", doc.ID, err))
			continue
		}
		r.result.StaleMarked = append(r.result.StaleMarked, doc.ID)
	}
	for _, id := range currentIDs {
		info, ok := r.docsByID[id]
		if !ok || info.Doc == nil || info.Doc.Lifecycle != extract.LifecycleStale {
			continue
		}
		doc := info.Doc
		doc.Lifecycle = extract.LifecycleActive
		doc.StaleReason = ""
		doc.StaledAt = nil
		if err := r.store.SaveDoc(doc); err != nil {
			r.result.Warnings = append(r.result.Warnings, fmt.Sprintf("restore doc %s: %v", doc.ID, err))
			continue
		}
		r.result.Restored = append(r.result.Restored, doc.ID)
	}
}

// detectSourceMissing marks docs whose tracked source file disappeared. Only
// providers that were actually scanned participate, so an unavailable root or
// a --since exclusion cannot stale documents by accident.
func (r *runner) detectSourceMissing(scanned map[string]bool, stateFiles map[string]cursor.Fingerprint) {
	for _, key := range sortedKeys(stateFiles) {
		fp := stateFiles[key]
		if !scanned[fp.Source] {
			continue
		}
		if _, err := os.Stat(key); err == nil || !errors.Is(err, os.ErrNotExist) {
			continue
		}
		for _, id := range fp.SegmentIDs {
			info, ok := r.docsByID[id]
			if !ok || info.Doc == nil || info.Doc.Lifecycle == extract.LifecycleStale {
				continue
			}
			doc := info.Doc
			staled := r.now
			doc.Lifecycle = extract.LifecycleStale
			doc.StaleReason = cache.StaleSourceMissing
			doc.StaledAt = &staled
			if err := r.store.SaveDoc(doc); err != nil {
				r.result.Warnings = append(r.result.Warnings, fmt.Sprintf("mark doc %s source_missing: %v", doc.ID, err))
				continue
			}
			r.result.SourceMissing = append(r.result.SourceMissing, doc.ID)
		}
	}
}

// selectJobs applies an explicit selection or the --segments cap. Explicit
// selection ignores the cap: choosing a session means choosing all of its
// prepared segments. Unknown or ambiguous selectors are reported before any
// model call.
func selectJobs(jobs []runJob, sel Selection, limit int) ([]runJob, error) {
	if sel.Empty() {
		if limit > 0 && len(jobs) > limit {
			return jobs[:limit], nil
		}
		return jobs, nil
	}

	sessionSelector := make(map[string]string)
	ambiguous := make(map[string][]string)
	for _, job := range jobs {
		sessionID := job.ps.Segment.SessionID
		if sessionID == "" {
			continue
		}
		selector := job.source + ":" + sessionID
		if previous, ok := sessionSelector[sessionID]; ok {
			if previous != selector {
				ambiguous[sessionID] = appendUnique(ambiguous[sessionID], previous, selector)
			}
			continue
		}
		sessionSelector[sessionID] = selector
	}

	wantSessions := make(map[string]bool)
	var unknown []string
	for _, raw := range sel.Sessions {
		value := strings.TrimSpace(raw)
		if value == "" {
			continue
		}
		if strings.Contains(value, ":") {
			wantSessions[value] = true
			continue
		}
		if choices := ambiguous[value]; len(choices) > 0 {
			return nil, fmt.Errorf("session selector %q is ambiguous; use one of: %s", value, strings.Join(choices, ", "))
		}
		if selector, ok := sessionSelector[value]; ok {
			wantSessions[selector] = true
			continue
		}
		unknown = append(unknown, value)
	}

	wantSegments := make(map[string]bool)
	for _, raw := range sel.Segments {
		value := strings.TrimSpace(raw)
		if value != "" {
			wantSegments[value] = true
		}
	}

	matchedSessions := make(map[string]bool)
	matchedSegments := make(map[string]bool)
	selected := make([]runJob, 0, len(jobs))
	for _, job := range jobs {
		selector := job.source + ":" + job.ps.Segment.SessionID
		match := false
		if wantSessions[selector] {
			match = true
			matchedSessions[selector] = true
		}
		if wantSegments[job.ps.Segment.ID] {
			match = true
			matchedSegments[job.ps.Segment.ID] = true
		}
		if match {
			selected = append(selected, job)
		}
	}

	for selector := range wantSessions {
		if !matchedSessions[selector] {
			unknown = append(unknown, selector)
		}
	}
	for id := range wantSegments {
		if !matchedSegments[id] {
			unknown = append(unknown, id)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		unknown = slices.Compact(unknown)
		return nil, fmt.Errorf("selection matched no segments for: %s (run \"devlog-learn sessions\" to list selectors)", strings.Join(unknown, ", "))
	}
	return selected, nil
}

func appendUnique(values []string, additions ...string) []string {
	for _, addition := range additions {
		if !slices.Contains(values, addition) {
			values = append(values, addition)
		}
	}
	return values
}

func sortedKeys(files map[string]cursor.Fingerprint) []string {
	keys := make([]string, 0, len(files))
	for key := range files {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func (r *runner) updateCursor(state *cursor.State, work *runFile, reports []RunReport, explicitSelection bool) {
	// Explicit selection is a targeted operation: files that were not selected
	// keep their previous cursor entry. Marking them "error" would force a
	// re-parse on every default run.
	if explicitSelection && len(work.selectedIndex) == 0 {
		return
	}
	var cause error
	if len(work.selectedIndex) < len(work.segments) {
		if explicitSelection {
			cause = fmt.Errorf("explicit selection: %d of %d prepared segments were not selected",
				len(work.segments)-len(work.selectedIndex), len(work.segments))
		} else {
			cause = fmt.Errorf("segment limit: %d of %d prepared segments were not processed", len(work.segments)-len(work.selectedIndex), len(work.segments))
		}
	}
	for _, index := range work.selectedIndex {
		if index >= len(reports) {
			continue
		}
		if err := reports[index].Err; err != nil {
			if cause == nil {
				cause = err
			}
			break
		}
	}
	if cause != nil {
		r.result.FailedFiles++
		fp := work.fp
		fp.SegmentIDs = append([]string(nil), work.segmentIDs...)
		state.MarkError(work.key, fp, cause, r.now)
		return
	}
	state.MarkOK(work.key, work.fp, work.segmentIDs, r.now)
}

func sessionWarnings(sess *ingest.Session) []string {
	if sess == nil {
		return nil
	}
	out := make([]string, 0, len(sess.Warnings)+1)
	for _, warning := range sess.Warnings {
		out = append(out, fmt.Sprintf("%s:%d: %s", warning.File, warning.Line, warning.Message))
	}
	if sess.Stats.WarningsDropped > 0 {
		out = append(out, fmt.Sprintf("%s: %d additional parse warnings suppressed", sess.File, sess.Stats.WarningsDropped))
	}
	return out
}
