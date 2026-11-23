// Package experience aggregates already-extracted Docs into project-level
// experience summaries (项目经历 / 实习经验).
//
// It is read-only with respect to Docs: it never rewrites a Doc and never calls
// a model. The deterministic aggregate is always available; an optional model
// synthesis is cached separately and attached when present.
package experience

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"

	"devlog-learn/internal/cache"
	"devlog-learn/internal/extract"
)

// Experience kinds. Only project and internship are suggested automatically;
// work and course are available through explicit configuration.
const (
	KindProject    = "project"
	KindInternship = "internship"
	KindWork       = "work"
	KindCourse     = "course"
)

// Where the effective kind came from.
const (
	KindSourceManual     = "manual"
	KindSourceSuggestion = "suggestion"
	KindSourceDefault    = "default"
)

const (
	// MaxPaths caps the distinct source directories shown for one project.
	MaxPaths = 5
	// UnknownProject is used when a Doc has no project name.
	UnknownProject = "unknown-project"

	maxHighlights     = 8
	maxChallenges     = 6
	maxLessons        = 10
	maxBlindSpots     = 10
	maxTimeline       = 100
	maxDocRefs        = 50
	maxChecklistDocs  = 12
	maxChecklistItems = 40
)

// Metadata is the human-owned description of one project experience.
type Metadata struct {
	Kind         string `json:"kind,omitempty"`
	Organization string `json:"organization,omitempty"`
	Role         string `json:"role,omitempty"`
	Start        string `json:"start,omitempty"`
	End          string `json:"end,omitempty"`
	Headline     string `json:"headline,omitempty"`
	Confirmed    bool   `json:"confirmed,omitempty"`
}

// DocRef is one contributing Doc in an Experience.
type DocRef struct {
	ID             string    `json:"id"`
	Title          string    `json:"title"`
	Summary        string    `json:"summary,omitempty"`
	KnowledgeType  string    `json:"knowledge_type"`
	Status         string    `json:"status"`
	LearningValue  string    `json:"learning_value,omitempty"`
	ReviewStatus   string    `json:"review_status"`
	OccurredAt     time.Time `json:"occurred_at,omitzero"`
	Confidence     float64   `json:"confidence"`
	SessionID      string    `json:"session_id,omitempty"`
	SegmentID      string    `json:"segment_id,omitempty"`
	EvidenceSeen   int       `json:"evidence_verified,omitempty"`
	EvidenceChecks int       `json:"evidence_checked,omitempty"`
}

// TimelineEntry is one dated item in the project history.
type TimelineEntry struct {
	Date   time.Time `json:"date,omitzero"`
	Title  string    `json:"title"`
	DocID  string    `json:"doc_id"`
	Kind   string    `json:"kind,omitempty"`
	Status string    `json:"status,omitempty"`
}

// Highlight is a deterministic summary line that points at one or more Docs.
type Highlight struct {
	Text         string   `json:"text"`
	DocIDs       []string `json:"doc_ids"`
	EvidenceSeen int      `json:"evidence_verified,omitempty"`
}

// ReviewItem is one static review prompt. There is no scheduling state.
type ReviewItem struct {
	ID       string `json:"id"`
	Text     string `json:"text"`
	DocID    string `json:"doc_id,omitempty"`
	DocTitle string `json:"doc_title,omitempty"`
	Kind     string `json:"kind"`
}

// Experience is the project-level aggregate. JSON field names are the Web API
// contract.
type Experience struct {
	Project     string   `json:"project"`
	ProjectPath string   `json:"project_path,omitempty"`
	Paths       []string `json:"paths"`

	Kind         string   `json:"kind"`
	KindLabel    string   `json:"kind_label"`
	KindSource   string   `json:"kind_source"`
	Organization string   `json:"organization,omitempty"`
	Role         string   `json:"role,omitempty"`
	Headline     string   `json:"headline,omitempty"`
	Overview     string   `json:"overview,omitempty"`
	Files        []string `json:"files"`

	Start  time.Time `json:"start,omitzero"`
	End    time.Time `json:"end,omitzero"`
	Period string    `json:"period,omitempty"`

	Docs     int      `json:"docs"`
	Sessions int      `json:"sessions"`
	Sources  []string `json:"sources"`
	Stack    []string `json:"stack"`

	Timeline        []TimelineEntry `json:"timeline"`
	Highlights      []Highlight     `json:"highlights"`
	Challenges      []Highlight     `json:"challenges"`
	Outcomes        []string        `json:"outcomes"`
	Lessons         []string        `json:"lessons"`
	BlindSpots      []ReviewItem    `json:"blind_spots"`
	Stories         []Highlight     `json:"stories"`
	ReviewChecklist []ReviewItem    `json:"review_checklist"`
	DocRefs         []DocRef        `json:"doc_refs"`
	Warnings        []string        `json:"warnings,omitempty"`

	Synthesized *Synthesis `json:"synthesized,omitempty"`
}

// Summary is the compact list representation used by CLI/Web.
type Summary struct {
	Project       string    `json:"project"`
	Kind          string    `json:"kind"`
	KindLabel     string    `json:"kind_label"`
	KindSource    string    `json:"kind_source"`
	SuggestedKind string    `json:"suggested_kind,omitempty"`
	Organization  string    `json:"organization,omitempty"`
	Role          string    `json:"role,omitempty"`
	Period        string    `json:"period,omitempty"`
	Start         time.Time `json:"start,omitzero"`
	End           time.Time `json:"end,omitzero"`
	DocCount      int       `json:"docs"`
	SessionCount  int       `json:"sessions"`
	Sources       []string  `json:"sources"`
	Stack         []string  `json:"stack"`
	HasSynthesis  bool      `json:"has_synthesis"`
}

// Errors returned by FindWith.
var (
	ErrNotFound  = errors.New("experience not found")
	ErrAmbiguous = errors.New("experience selector is ambiguous")
)

// ProjectName normalizes a Doc project name.
func ProjectName(project string) string {
	project = strings.TrimSpace(project)
	if project == "" {
		return UnknownProject
	}
	return project
}

// ValidKind reports whether kind is a supported experience kind.
func ValidKind(kind string) bool {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case KindProject, KindInternship, KindWork, KindCourse:
		return true
	}
	return false
}

// NormalizeKind returns the canonical kind, or "" when invalid.
func NormalizeKind(kind string) string {
	kind = strings.ToLower(strings.TrimSpace(kind))
	if ValidKind(kind) {
		return kind
	}
	return ""
}

// KindLabel returns the Chinese display label for a kind.
func KindLabel(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case KindInternship:
		return "实习经验"
	case KindWork:
		return "工作经历"
	case KindCourse:
		return "学习经历"
	default:
		return "项目经历"
	}
}

// Build groups active Docs by project and derives a deterministic experience
// aggregate for each. Stale Docs are ignored. A nil store simply means source
// paths are unavailable.
func Build(infos []cache.DocInfo, store *cache.Store, state *State, now time.Time) []Experience {
	_ = now // the deterministic baseline deliberately contains no render timestamp

	if state == nil {
		state = NewState()
	}
	groups := map[string][]*extract.Doc{}
	for _, info := range infos {
		doc := info.Doc
		if doc == nil || doc.Lifecycle == extract.LifecycleStale {
			continue
		}
		project := ProjectName(doc.Project)
		groups[project] = append(groups[project], doc)
	}

	paths := ResolveCwds(store, infos)
	projects := make([]string, 0, len(groups))
	for project := range groups {
		projects = append(projects, project)
	}
	sort.Strings(projects)

	out := make([]Experience, 0, len(projects))
	docsByProject := make(map[string][]*extract.Doc, len(projects))
	for _, project := range projects {
		docs := groups[project]
		sortDocs(docs)
		exp := buildOne(project, docs, paths[project], state)
		out = append(out, exp)
		docsByProject[project] = docs
	}

	if store != nil && strings.TrimSpace(store.Root) != "" {
		AttachCachedSynthesis(out, docsByProject, store.Root)
	}
	return out
}

// DocsForProject returns active Docs belonging to one project.
func DocsForProject(infos []cache.DocInfo, project string) []*extract.Doc {
	project = ProjectName(project)
	var out []*extract.Doc
	for _, info := range infos {
		doc := info.Doc
		if doc == nil || doc.Lifecycle == extract.LifecycleStale {
			continue
		}
		if ProjectName(doc.Project) == project {
			out = append(out, doc)
		}
	}
	sortDocs(out)
	return out
}

type pathInfo struct {
	cwds    []string
	warning string
}

// ResolveCwds recovers source directories from cached Segments. Docs do not
// carry Cwd, and a Segment may be missing, so every failure is non-fatal.
func ResolveCwds(store *cache.Store, infos []cache.DocInfo) map[string]pathInfo {
	out := map[string]pathInfo{}
	if store == nil || strings.TrimSpace(store.Root) == "" {
		return out
	}
	if info, err := os.Stat(filepath.Join(store.Root, "segments")); err != nil || !info.IsDir() {
		return out
	}

	// One successful Segment load per session is enough; failed Segment IDs are
	// remembered so a large cache without segments/ cannot cause repeated reads.
	sessionDone := map[string]bool{}
	triedSegment := map[string]bool{}
	byProject := map[string]map[string]bool{}

	for _, info := range infos {
		doc := info.Doc
		if doc == nil || doc.Lifecycle == extract.LifecycleStale {
			continue
		}
		project := ProjectName(doc.Project)
		if byProject[project] == nil {
			byProject[project] = map[string]bool{}
		}
		sessionKey := strings.TrimSpace(doc.Provenance.SessionID)
		if sessionKey == "" {
			sessionKey = "doc:" + doc.ID
		}
		if sessionDone[sessionKey] {
			continue
		}
		segmentID := strings.TrimSpace(doc.Provenance.SegmentID)
		if segmentID == "" {
			segmentID = strings.TrimSpace(doc.ID)
		}
		if segmentID == "" || triedSegment[segmentID] {
			continue
		}
		triedSegment[segmentID] = true

		seg, err := store.LoadSegment(segmentID)
		if err != nil {
			continue
		}
		cwd := strings.TrimSpace(seg.Cwd)
		if cwd == "" {
			continue
		}
		sessionDone[sessionKey] = true
		byProject[project][cwd] = true
	}

	for project, set := range byProject {
		cwds := make([]string, 0, len(set))
		for cwd := range set {
			cwds = append(cwds, cwd)
		}
		sort.Strings(cwds)
		total := len(cwds)
		if len(cwds) > MaxPaths {
			cwds = cwds[:MaxPaths]
		}
		info := pathInfo{cwds: cwds}
		if total > 1 {
			info.warning = fmt.Sprintf("项目 %q 对应 %d 个源目录；按项目名聚合，完整目录见 paths", project, total)
		}
		out[project] = info
	}
	return out
}

// AttachCachedSynthesis is implemented in synthesize.go. It is declared here
// only as documentation of the Build contract.

func buildOne(project string, docs []*extract.Doc, info pathInfo, state *State) Experience {
	exp := Experience{
		Project:         project,
		Paths:           nonNilStrings(info.cwds),
		Files:           []string{},
		Timeline:        []TimelineEntry{},
		Highlights:      []Highlight{},
		Challenges:      []Highlight{},
		Outcomes:        []string{},
		Lessons:         []string{},
		BlindSpots:      []ReviewItem{},
		Stories:         []Highlight{},
		ReviewChecklist: []ReviewItem{},
		DocRefs:         []DocRef{},
		Sources:         []string{},
		Stack:           []string{},
		Warnings:        []string{},
	}
	if len(info.cwds) > 0 {
		exp.ProjectPath = info.cwds[0]
	}
	if info.warning != "" {
		exp.Warnings = append(exp.Warnings, info.warning)
	}
	if project == UnknownProject {
		exp.Warnings = append(exp.Warnings, "部分文档没有项目名，已归入 unknown-project")
	}

	exp.Kind, exp.KindSource = resolveKind(project, info.cwds, state)
	exp.KindLabel = KindLabel(exp.Kind)
	if meta, ok := state.Get(project); ok {
		exp.Organization = strings.TrimSpace(meta.Organization)
		exp.Role = strings.TrimSpace(meta.Role)
		exp.Headline = strings.TrimSpace(meta.Headline)
		exp.Period = formatPeriod(meta.Start, meta.End)
	}

	sessions := map[string]bool{}
	sources := map[string]bool{}
	stackCount := map[string]*stackItem{}
	typeCount := map[string]int{}
	resolved, followUp, unknownStatus := 0, 0, 0
	approved, pending, rejected := 0, 0, 0
	evidenceVerified, evidenceChecked := 0, 0

	for _, doc := range docs {
		if doc == nil {
			continue
		}
		if !doc.OccurredAt.IsZero() {
			if exp.Start.IsZero() || doc.OccurredAt.Before(exp.Start) {
				exp.Start = doc.OccurredAt
			}
			if exp.End.IsZero() || doc.OccurredAt.After(exp.End) {
				exp.End = doc.OccurredAt
			}
		}
		if session := strings.TrimSpace(doc.Provenance.SessionID); session != "" {
			sessions[session] = true
		}
		if source := strings.TrimSpace(doc.Source); source != "" {
			sources[source] = true
		}
		for _, item := range doc.Stack {
			item = strings.TrimSpace(item)
			if item == "" {
				continue
			}
			key := strings.ToLower(item)
			entry := stackCount[key]
			if entry == nil {
				entry = &stackItem{label: item}
				stackCount[key] = entry
			}
			entry.count++
		}
		if kind := strings.TrimSpace(doc.KnowledgeType); kind != "" {
			typeCount[kind]++
		}
		switch strings.ToLower(strings.TrimSpace(doc.Status)) {
		case "resolved":
			resolved++
		case "unresolved", "workaround":
			followUp++
		default:
			unknownStatus++
		}
		switch strings.ToLower(strings.TrimSpace(doc.ReviewStatus)) {
		case extract.ReviewApproved:
			approved++
		case extract.ReviewRejected:
			rejected++
		default:
			pending++
		}
		evidenceVerified += doc.Validation.EvidenceVerified
		evidenceChecked += doc.Validation.EvidenceChecked
	}

	exp.Docs = len(docs)
	exp.Sessions = len(sessions)
	exp.Sources = sortedKeys(sources)
	exp.Stack = sortedStack(stackCount)
	if exp.Period == "" {
		exp.Period = formatSpan(exp.Start, exp.End)
	}

	for _, doc := range docs {
		if doc == nil {
			continue
		}
		if len(exp.Timeline) < maxTimeline {
			exp.Timeline = append(exp.Timeline, TimelineEntry{
				Date:   doc.OccurredAt,
				Title:  strings.TrimSpace(doc.Title),
				DocID:  doc.ID,
				Kind:   doc.KnowledgeType,
				Status: doc.Status,
			})
		}
		if len(exp.DocRefs) < maxDocRefs {
			exp.DocRefs = append(exp.DocRefs, newDocRef(doc))
		}
	}

	substantive := make([]*extract.Doc, 0, len(docs))
	for _, doc := range docs {
		if isSubstantive(doc) {
			substantive = append(substantive, doc)
		}
	}
	if len(substantive) == 0 && len(docs) > 0 {
		exp.Warnings = append(exp.Warnings, "没有检测到实质工作文档（缺少命令、文件改动或诊断）；关键工作与建模内容将为空")
	}

	ranked := rankDocs(substantive)
	exp.Highlights = buildHighlights(ranked, maxHighlights)
	exp.Challenges = buildChallenges(ranked, maxChallenges)
	exp.Stories = buildStories(ranked, 2)
	exp.Lessons = collectLessons(substantive, maxLessons)
	exp.BlindSpots = collectBlindSpots(substantive, maxBlindSpots)
	exp.Files = collectFiles(substantive, 12)
	exp.ReviewChecklist = buildChecklist(project, ranked, exp.Kind)
	exp.Overview = buildOverview(len(docs), len(substantive), exp.Period, exp.Stack, resolved, followUp, fileCount(exp.Files))
	exp.Outcomes = buildOutcomes(len(docs), len(substantive), resolved, followUp, unknownStatus, typeCount,
		approved, pending, rejected, evidenceVerified, evidenceChecked)

	if exp.Docs == 0 {
		exp.Warnings = append(exp.Warnings, "该项目没有可用的 active 文档")
	}
	return exp
}

func resolveKind(project string, paths []string, state *State) (string, string) {
	if state != nil {
		if meta, ok := state.Get(project); ok {
			if kind := NormalizeKind(meta.Kind); kind != "" {
				return kind, KindSourceManual
			}
		}
	}
	if kind := SuggestKind(project, paths); kind != "" {
		return kind, KindSourceSuggestion
	}
	return KindProject, KindSourceDefault
}

// SuggestKind derives a non-authoritative kind suggestion from the project name
// and source paths. It never returns work or course.
func SuggestKind(project string, paths []string) string {
	values := append([]string{project}, paths...)
	for _, value := range values {
		if strings.Contains(value, "实习") {
			return KindInternship
		}
	}
	for _, value := range values {
		for _, token := range pathTokens(value) {
			switch token {
			case "intern", "internship", "placement":
				return KindInternship
			}
		}
	}
	return ""
}

func pathTokens(value string) []string {
	return strings.FieldsFunc(strings.ToLower(value), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
}

func sortDocs(docs []*extract.Doc) {
	sort.SliceStable(docs, func(i, j int) bool {
		a, b := docs[i], docs[j]
		if !a.OccurredAt.Equal(b.OccurredAt) {
			if a.OccurredAt.IsZero() {
				return false
			}
			if b.OccurredAt.IsZero() {
				return true
			}
			return a.OccurredAt.Before(b.OccurredAt)
		}
		return a.ID < b.ID
	})
}

func rankDocs(docs []*extract.Doc) []*extract.Doc {
	ranked := make([]*extract.Doc, 0, len(docs))
	for _, doc := range docs {
		if doc != nil {
			ranked = append(ranked, doc)
		}
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		a, b := docRank(ranked[i]), docRank(ranked[j])
		if a != b {
			return a > b
		}
		if !ranked[i].OccurredAt.Equal(ranked[j].OccurredAt) {
			return ranked[i].OccurredAt.After(ranked[j].OccurredAt)
		}
		return ranked[i].ID < ranked[j].ID
	})
	return ranked
}

func docRank(doc *extract.Doc) int {
	score := 0
	switch strings.ToLower(strings.TrimSpace(doc.LearningValue)) {
	case "high":
		score += 30
	case "medium":
		score += 15
	}
	score += int(doc.Confidence * 10)
	score += doc.Validation.EvidenceVerified
	if strings.EqualFold(strings.TrimSpace(doc.ReviewStatus), extract.ReviewApproved) {
		score += 10
	}
	return score
}

func buildHighlights(ranked []*extract.Doc, limit int) []Highlight {
	out := make([]Highlight, 0, limit)
	for _, doc := range ranked {
		if len(out) >= limit {
			break
		}
		title := strings.TrimSpace(doc.Title)
		detail := firstSentence(doc.Summary, 140)
		if detail == "" {
			for _, step := range doc.Solution {
				if action := strings.TrimSpace(step.Action); meaningful(action) {
					detail = truncateRunes(action, 140)
					break
				}
			}
		}
		if detail == "" && meaningful(doc.Problem.Statement) {
			detail = firstSentence(doc.Problem.Statement, 140)
		}
		text := title
		if detail != "" {
			if text == "" {
				text = detail
			} else {
				text += "：" + detail
			}
		}
		if text == "" {
			continue
		}
		out = append(out, Highlight{
			Text:         truncateRunes(text, 220),
			DocIDs:       []string{doc.ID},
			EvidenceSeen: doc.Validation.EvidenceVerified,
		})
	}
	return out
}

func buildChallenges(ranked []*extract.Doc, limit int) []Highlight {
	out := make([]Highlight, 0, limit)
	for _, doc := range ranked {
		if len(out) >= limit {
			break
		}
		if !isChallenge(doc) {
			continue
		}
		text := challengeText(doc)
		if text == "" {
			continue
		}
		out = append(out, Highlight{
			Text:         text,
			DocIDs:       []string{doc.ID},
			EvidenceSeen: doc.Validation.EvidenceVerified,
		})
	}
	return out
}

func challengeText(doc *extract.Doc) string {
	candidates := []string{
		doc.RootCause.Text,
		firstPitfall(doc),
		doc.Problem.Statement,
		firstRejectedHypothesis(doc),
		firstBlindSpot(doc),
	}
	for _, candidate := range candidates {
		if meaningful(candidate) {
			return truncateRunes(strings.TrimSpace(candidate), 220)
		}
	}
	return ""
}

func isChallenge(doc *extract.Doc) bool {
	if doc == nil {
		return false
	}
	if !meaningful(challengeText(doc)) {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(doc.Difficulty), "high") {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(doc.Status)) {
	case "unresolved", "workaround":
		return true
	}
	if doc.RootCause.Known && meaningful(doc.RootCause.Text) {
		return true
	}
	if meaningful(firstPitfall(doc)) {
		return true
	}
	return meaningful(firstRejectedHypothesis(doc))
}

func firstPitfall(doc *extract.Doc) string {
	if doc == nil {
		return ""
	}
	for _, pitfall := range doc.Pitfalls {
		if trap := strings.TrimSpace(pitfall.Trap); meaningful(trap) {
			return trap
		}
	}
	return ""
}

func firstRejectedHypothesis(doc *extract.Doc) string {
	if doc == nil {
		return ""
	}
	for _, rejected := range doc.RejectedHypotheses {
		if hypothesis := strings.TrimSpace(rejected.Hypothesis); meaningful(hypothesis) {
			return hypothesis
		}
	}
	return ""
}

func firstBlindSpot(doc *extract.Doc) string {
	if doc == nil {
		return ""
	}
	for _, spot := range doc.BlindSpots {
		if point := strings.TrimSpace(spot.Point); meaningful(point) {
			return point
		}
	}
	return ""
}

func buildOutcomes(docs, substantive, resolved, followUp, unknownStatus int, typeCount map[string]int,
	approved, pending, rejected, evidenceVerified, evidenceChecked int) []string {
	out := []string{
		fmt.Sprintf("文档 %d 篇，其中可复核工作文档 %d 篇；已解决 %d，待跟进 %d，状态未知 %d",
			docs, substantive, resolved, followUp, unknownStatus),
		fmt.Sprintf("人工审核：通过 %d，待审 %d，驳回 %d", approved, pending, rejected),
	}
	if evidenceChecked > 0 {
		out = append(out, fmt.Sprintf("证据核验：%d / %d", evidenceVerified, evidenceChecked))
	}
	if len(typeCount) > 0 {
		keys := make([]string, 0, len(typeCount))
		for key := range typeCount {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		parts := make([]string, 0, len(keys))
		for _, key := range keys {
			parts = append(parts, fmt.Sprintf("%s %d", key, typeCount[key]))
		}
		out = append(out, "知识类型："+strings.Join(parts, "，"))
	}
	return out
}

func buildStories(ranked []*extract.Doc, limit int) []Highlight {
	out := make([]Highlight, 0, limit)
	for _, doc := range ranked {
		if len(out) >= limit {
			break
		}
		problem := strings.TrimRight(firstSentence(doc.Problem.Statement, 160), "。；; ")
		actions := make([]string, 0, 3)
		for _, step := range doc.Solution {
			action := strings.TrimSpace(step.Action)
			if !meaningful(action) {
				continue
			}
			actions = append(actions, truncateRunes(action, 120))
			if len(actions) >= 3 {
				break
			}
		}
		if problem == "" && len(actions) == 0 {
			continue
		}
		result := storyResult(doc)
		var b strings.Builder
		if problem != "" {
			b.WriteString("背景/任务：")
			b.WriteString(problem)
		}
		if len(actions) > 0 {
			if b.Len() > 0 {
				b.WriteString("；")
			}
			b.WriteString("行动：")
			b.WriteString(strings.Join(actions, " → "))
		}
		if result != "" {
			if b.Len() > 0 {
				b.WriteString("；")
			}
			b.WriteString("结果：")
			b.WriteString(result)
		}
		out = append(out, Highlight{
			Text:         truncateRunes(b.String(), 420),
			DocIDs:       []string{doc.ID},
			EvidenceSeen: doc.Validation.EvidenceVerified,
		})
	}
	return out
}

func storyResult(doc *extract.Doc) string {
	if doc == nil {
		return ""
	}
	for _, check := range doc.Verification {
		if strings.EqualFold(strings.TrimSpace(check.Status), "passed") && meaningful(check.Check) {
			return "已验证：" + truncateRunes(strings.TrimSpace(check.Check), 160)
		}
	}
	if meaningful(doc.RootCause.Text) {
		return truncateRunes(strings.TrimSpace(doc.RootCause.Text), 160)
	}
	switch strings.ToLower(strings.TrimSpace(doc.Status)) {
	case "resolved":
		return "已解决"
	case "workaround":
		return "已有绕过方案"
	case "unresolved":
		return "仍未解决"
	}
	return "会话未记录明确的验证结果"
}

func collectLessons(docs []*extract.Doc, limit int) []string {
	seen := map[string]bool{}
	out := make([]string, 0, limit)
	for _, doc := range docs {
		for _, lesson := range doc.Lessons {
			lesson = strings.TrimSpace(lesson)
			key := strings.ToLower(lesson)
			if !meaningful(lesson) || seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, lesson)
			if len(out) >= limit {
				return out
			}
		}
	}
	return out
}

func collectBlindSpots(docs []*extract.Doc, limit int) []ReviewItem {
	seen := map[string]bool{}
	out := make([]ReviewItem, 0, limit)
	for _, doc := range docs {
		for _, spot := range doc.BlindSpots {
			point := strings.TrimSpace(spot.Point)
			key := strings.ToLower(point)
			if !meaningful(point) || seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, ReviewItem{
				ID:       reviewID(doc.Project, doc.ID, "blind_spot", point),
				Text:     point,
				DocID:    doc.ID,
				DocTitle: strings.TrimSpace(doc.Title),
				Kind:     "blind_spot",
			})
			if len(out) >= limit {
				return out
			}
		}
	}
	return out
}

func buildChecklist(project string, ranked []*extract.Doc, kind string) []ReviewItem {
	items := make([]ReviewItem, 0, maxChecklistItems)
	seen := map[string]bool{}
	add := func(category, text, docID, docTitle string) {
		text = strings.TrimSpace(text)
		if text == "" || len(items) >= maxChecklistItems {
			return
		}
		key := strings.ToLower(strings.Join([]string{category, docID, text}, "\x00"))
		if seen[key] {
			return
		}
		seen[key] = true
		items = append(items, ReviewItem{
			ID:       reviewID(project, docID, category, text),
			Text:     text,
			DocID:    docID,
			DocTitle: docTitle,
			Kind:     category,
		})
	}

	add("project", "用 2 分钟介绍项目背景、目标和技术栈", "", "")
	add("project", "画出核心数据流/架构，并说明关键取舍", "", "")
	add("project", "讲清一个最难的问题：现象 → 定位 → 根因 → 修复 → 验证", "", "")
	if kind == KindInternship || kind == KindWork {
		add("project", "说明你的角色、协作方式和交付结果", "", "")
	}

	for i, doc := range ranked {
		if i >= maxChecklistDocs {
			break
		}
		title := strings.TrimSpace(doc.Title)
		if title == "" {
			title = doc.ID
		}
		if meaningful(doc.RootCause.Text) {
			add("root_cause", fmt.Sprintf("不看文档，讲清「%s」的根因与验证方法", title), doc.ID, title)
		}
		for n, concept := range doc.Concepts {
			if n >= 2 {
				break
			}
			name := strings.TrimSpace(concept.Name)
			if !meaningful(name) {
				continue
			}
			add("concept", fmt.Sprintf("解释「%s」在本项目中的作用", name), doc.ID, title)
		}
		for n, rejected := range doc.RejectedHypotheses {
			if n >= 1 {
				break
			}
			hypothesis := strings.TrimSpace(rejected.Hypothesis)
			if !meaningful(hypothesis) {
				continue
			}
			add("rejected_hypothesis", fmt.Sprintf("说明为什么排除「%s」", hypothesis), doc.ID, title)
		}
		for _, spot := range doc.BlindSpots {
			point := strings.TrimSpace(spot.Point)
			if !meaningful(point) {
				continue
			}
			add("blind_spot", "补上："+point, doc.ID, title)
			break
		}
		for _, pitfall := range doc.Pitfalls {
			trap := strings.TrimSpace(pitfall.Trap)
			if !meaningful(trap) {
				continue
			}
			add("pitfall", fmt.Sprintf("下次如何避免「%s」", trap), doc.ID, title)
			break
		}
		for _, question := range doc.OpenQuestions {
			question = strings.TrimSpace(question)
			if !meaningfulQuestion(question) {
				continue
			}
			add("open_question", fmt.Sprintf("你能回答：「%s」吗", question), doc.ID, title)
			break
		}
	}
	return items
}

func reviewID(project, docID, category, text string) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{project, docID, category, strings.ToLower(strings.TrimSpace(text))}, "\x00")))
	return "ex_" + hex.EncodeToString(sum[:])[:12]
}

func newDocRef(doc *extract.Doc) DocRef {
	return DocRef{
		ID:             doc.ID,
		Title:          strings.TrimSpace(doc.Title),
		Summary:        strings.TrimSpace(doc.Summary),
		KnowledgeType:  strings.TrimSpace(doc.KnowledgeType),
		Status:         strings.TrimSpace(doc.Status),
		LearningValue:  strings.TrimSpace(doc.LearningValue),
		ReviewStatus:   strings.TrimSpace(doc.ReviewStatus),
		OccurredAt:     doc.OccurredAt,
		Confidence:     doc.Confidence,
		SessionID:      strings.TrimSpace(doc.Provenance.SessionID),
		SegmentID:      strings.TrimSpace(doc.Provenance.SegmentID),
		EvidenceSeen:   doc.Validation.EvidenceVerified,
		EvidenceChecks: doc.Validation.EvidenceChecked,
	}
}

// FindWith resolves a user-supplied project selector. Exact names win, then a
// unique case-insensitive match, then an optional unique safe-name match.
func FindWith(exps []Experience, selector string, safeName func(string) string) (*Experience, error) {
	selector = strings.TrimSpace(selector)
	if selector == "" {
		return nil, ErrNotFound
	}
	for i := range exps {
		if exps[i].Project == selector {
			return &exps[i], nil
		}
	}
	matches := matchProjects(exps, func(exp Experience) bool {
		return strings.EqualFold(exp.Project, selector)
	})
	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) > 1 {
		return nil, fmt.Errorf("%w: %s", ErrAmbiguous, projectNames(matches))
	}
	if safeName != nil {
		matches = matchProjects(exps, func(exp Experience) bool {
			return strings.EqualFold(safeName(exp.Project), selector)
		})
		if len(matches) == 1 {
			return matches[0], nil
		}
		if len(matches) > 1 {
			return nil, fmt.Errorf("%w: %s", ErrAmbiguous, projectNames(matches))
		}
	}
	available := make([]string, 0, len(exps))
	for _, exp := range exps {
		available = append(available, exp.Project)
	}
	sort.Strings(available)
	return nil, fmt.Errorf("%w: %s (available: %s)", ErrNotFound, selector, strings.Join(available, ", "))
}

// Find resolves exact or unique case-insensitive project names.
func Find(exps []Experience, selector string) (*Experience, error) {
	return FindWith(exps, selector, nil)
}

func matchProjects(exps []Experience, match func(Experience) bool) []*Experience {
	var out []*Experience
	for i := range exps {
		if match(exps[i]) {
			out = append(out, &exps[i])
		}
	}
	return out
}

func projectNames(exps []*Experience) string {
	names := make([]string, 0, len(exps))
	for _, exp := range exps {
		names = append(names, exp.Project)
	}
	sort.Strings(names)
	if len(names) == 0 {
		return "(none)"
	}
	return strings.Join(names, ", ")
}

// Summarize returns the compact list view. All slices are non-nil so the JSON
// API serializes [] rather than null.
func Summarize(exp Experience) Summary {
	return Summary{
		Project:       exp.Project,
		Kind:          exp.Kind,
		KindLabel:     exp.KindLabel,
		KindSource:    exp.KindSource,
		SuggestedKind: SuggestKind(exp.Project, exp.Paths),
		Organization:  exp.Organization,
		Role:          exp.Role,
		Period:        exp.Period,
		Start:         exp.Start,
		End:           exp.End,
		DocCount:      exp.Docs,
		SessionCount:  exp.Sessions,
		Sources:       nonNilStrings(exp.Sources),
		Stack:         nonNilStrings(exp.Stack),
		HasSynthesis:  exp.Synthesized != nil,
	}
}

// SummarizeAll returns compact views in project order.
func SummarizeAll(exps []Experience) []Summary {
	out := make([]Summary, 0, len(exps))
	for _, exp := range exps {
		out = append(out, Summarize(exp))
	}
	return out
}

type stackItem struct {
	label string
	count int
}

func sortedStack(values map[string]*stackItem) []string {
	items := make([]*stackItem, 0, len(values))
	for _, item := range values {
		items = append(items, item)
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].count != items[j].count {
			return items[i].count > items[j].count
		}
		return strings.ToLower(items[i].label) < strings.ToLower(items[j].label)
	})
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, item.label)
	}
	return out
}

func sortedKeys(values map[string]bool) []string {
	out := make([]string, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func nonNilStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

func formatPeriod(start, end string) string {
	start = strings.TrimSpace(start)
	end = strings.TrimSpace(end)
	switch {
	case start != "" && end != "" && start != end:
		return start + " ~ " + end
	case start != "":
		return start
	default:
		return end
	}
}

func formatSpan(start, end time.Time) string {
	if start.IsZero() && end.IsZero() {
		return ""
	}
	if start.IsZero() {
		return end.UTC().Format("2006-01")
	}
	if end.IsZero() {
		return start.UTC().Format("2006-01")
	}
	a := start.UTC().Format("2006-01")
	b := end.UTC().Format("2006-01")
	if a == b {
		return a
	}
	return a + " ~ " + b
}

func isSubstantive(doc *extract.Doc) bool {
	if doc == nil {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(doc.LearningValue), "high") {
		return true
	}
	if len(doc.Artifacts.Files) > 0 || len(doc.Artifacts.Commands) > 0 {
		return true
	}
	if len(doc.Commands) > 0 || len(doc.Diagnosis) > 0 {
		return true
	}
	if doc.Validation.EvidenceVerified >= 5 {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(doc.KnowledgeType)) {
	case "bug_fix", "development", "refactoring", "architecture":
		return len(doc.Solution) > 0 || meaningful(doc.RootCause.Text) || meaningful(firstPitfall(doc))
	default:
		return false
	}
}

func meaningful(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "unknown", "none", "null", "n/a", "na", "undefined", "-", "未知", "无", "不适用":
		return false
	}
	return true
}

func firstSentence(value string, limit int) string {
	value = strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	if value == "" {
		return ""
	}
	for _, separator := range []string{"。", "！", "？", "；", ". "} {
		if index := strings.Index(value, separator); index >= 0 {
			return truncateRunes(value[:index+len(separator)], limit)
		}
	}
	return truncateRunes(value, limit)
}

func meaningfulQuestion(question string) bool {
	if !meaningful(question) || len([]rune(question)) < 8 {
		return false
	}
	lower := strings.ToLower(question)
	for _, noise := range []string{"transcript", "无证据", "未记录", "无法判断", "是否被实际采纳", "无明确证据"} {
		if strings.Contains(lower, noise) {
			return false
		}
	}
	return true
}

func collectFiles(docs []*extract.Doc, limit int) []string {
	seen := map[string]bool{}
	out := make([]string, 0, limit)
	for _, doc := range docs {
		for _, file := range doc.Artifacts.Files {
			file = strings.TrimSpace(file)
			key := strings.ToLower(file)
			if file == "" || seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, file)
			if len(out) >= limit {
				return out
			}
		}
	}
	return out
}

func buildOverview(docs, substantive int, period string, stack []string, resolved, followUp, files int) string {
	parts := make([]string, 0, 5)
	if period != "" {
		parts = append(parts, period+" 期间")
	}
	parts = append(parts, fmt.Sprintf("共 %d 篇文档，其中 %d 篇包含可复核的工作内容", docs, substantive))
	if len(stack) > 0 {
		parts = append(parts, "主要技术栈 "+strings.Join(stack, "、"))
	}
	if resolved > 0 || followUp > 0 {
		parts = append(parts, fmt.Sprintf("已解决 %d 项，待跟进 %d 项", resolved, followUp))
	}
	if files > 0 {
		parts = append(parts, fmt.Sprintf("涉及 %d 个文件", files))
	}
	return strings.Join(parts, "；") + "。"
}

func fileCount(files []string) int {
	return len(files)
}

func truncateRunes(value string, limit int) string {
	runes := []rune(strings.TrimSpace(value))
	if limit <= 0 || len(runes) <= limit {
		return string(runes)
	}
	return string(runes[:limit]) + "…"
}
