// Package search provides deterministic lexical retrieval over extracted Docs.
//
// It is deliberately dependency-free: the corpus is small, so a weighted
// token-overlap score is enough and avoids embeddings or a vector store. Callers
// pass active documents from cache.Store.ListActiveDocs; stale documents are
// also filtered here defensively.
package search

import (
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"devlog-learn/internal/cache"
	"devlog-learn/internal/extract"
	"devlog-learn/internal/render"
	"devlog-learn/internal/segment"
)

const (
	// DefaultLimit is used when Options.Limit is zero.
	DefaultLimit = 5
	// MaxLimit caps one search response.
	MaxLimit = 50
)

// Options controls one search. An empty Query returns the most recent documents
// (useful for an agent's first look at a project). Project may be empty (all),
// "auto" (resolved by the caller), or a project name; it matches both the
// original and filesystem-safe form.
type Options struct {
	Query   string
	Project string
	Tags    []string
	Review  string
	Limit   int
	OutDir  string

	// IncludeStale opts into stale documents. It defaults to false so every
	// existing CLI/MCP caller keeps the active-only contract.
	IncludeStale bool
	// All ignores Limit and returns every match. The local UI uses it for its
	// document library; CLI/MCP callers keep their explicit limits.
	All bool
}

// Result is one ranked document. Fields mirror the stable Doc contract so an
// agent can decide whether to fetch the full document.
type Result struct {
	ID               string    `json:"id"`
	Title            string    `json:"title"`
	Project          string    `json:"project"`
	Summary          string    `json:"summary"`
	KnowledgeType    string    `json:"knowledge_type"`
	Status           string    `json:"status"`
	ReviewStatus     string    `json:"review_status"`
	Lifecycle        string    `json:"lifecycle,omitempty"`
	LearningValue    string    `json:"learning_value,omitempty"`
	Tags             []string  `json:"tags"`
	Stack            []string  `json:"stack"`
	Confidence       float64   `json:"confidence"`
	EvidenceChecked  int       `json:"evidence_checked"`
	EvidenceVerified int       `json:"evidence_verified"`
	OccurredAt       time.Time `json:"occurred_at"`
	MarkdownPath     string    `json:"markdown_path"`
	MarkdownFile     string    `json:"markdown_file,omitempty"`
	DocPath          string    `json:"doc_path"`
	Score            float64   `json:"score"`
	Matched          []string  `json:"matched,omitempty"`
}

// Project summarizes the documents available for one project.
type Project struct {
	Name     string `json:"name"`
	SafeName string `json:"safe_name"`
	Docs     int    `json:"docs"`
	Approved int    `json:"approved"`
	Pending  int    `json:"pending"`
	Rejected int    `json:"rejected"`
}

// Search ranks active documents against opts. It never returns stale documents.
func Search(infos []cache.DocInfo, opts Options) []Result {
	opts = normalizeOptions(opts)

	docs := make([]*extract.Doc, 0, len(infos))
	docPath := make(map[string]string, len(infos))
	for _, info := range infos {
		if info.Doc == nil {
			continue
		}
		if info.Doc.Lifecycle == extract.LifecycleStale && !opts.IncludeStale {
			continue
		}
		docs = append(docs, info.Doc)
		docPath[info.Doc.ID] = info.Path
	}

	// Use the same collision-free plan as render/index so every Markdown path
	// returned to an agent points at a real rendered file.
	planPath := make(map[string]string, len(docs))
	for _, plan := range render.PlanDocs(docs) {
		planPath[plan.Doc.ID] = plan.Path
	}

	results := make([]Result, 0, len(docs))
	for _, doc := range docs {
		if !matchesProject(opts.Project, doc) || !matchesTags(opts.Tags, doc) || !matchesReview(opts.Review, doc) {
			continue
		}
		score, matched := scoreDoc(opts.Query, doc)
		if strings.TrimSpace(opts.Query) != "" && score <= 0 {
			continue
		}
		markdownPath := planPath[doc.ID]
		markdownFile := ""
		if strings.TrimSpace(opts.OutDir) != "" && markdownPath != "" {
			markdownFile = filepath.Join(opts.OutDir, filepath.FromSlash(markdownPath))
		}
		results = append(results, Result{
			ID:               doc.ID,
			Title:            doc.Title,
			Project:          doc.Project,
			Summary:          doc.Summary,
			KnowledgeType:    doc.KnowledgeType,
			Status:           doc.Status,
			ReviewStatus:     doc.ReviewStatus,
			Lifecycle:        doc.Lifecycle,
			LearningValue:    doc.LearningValue,
			Tags:             nonNil(doc.Tags),
			Stack:            nonNil(doc.Stack),
			Confidence:       doc.Confidence,
			EvidenceChecked:  doc.Validation.EvidenceChecked,
			EvidenceVerified: doc.Validation.EvidenceVerified,
			OccurredAt:       doc.OccurredAt,
			MarkdownPath:     markdownPath,
			MarkdownFile:     markdownFile,
			DocPath:          docPath[doc.ID],
			Score:            score,
			Matched:          matched,
		})
	}

	sort.SliceStable(results, func(i, j int) bool {
		if results[i].Score != results[j].Score {
			return results[i].Score > results[j].Score
		}
		if !results[i].OccurredAt.Equal(results[j].OccurredAt) {
			return results[i].OccurredAt.After(results[j].OccurredAt)
		}
		return results[i].ID < results[j].ID
	})
	if !opts.All && len(results) > opts.Limit {
		results = results[:opts.Limit]
	}
	return results
}

// ListProjects summarizes active documents by project, sorted by name.
func ListProjects(infos []cache.DocInfo) []Project {
	byName := map[string]*Project{}
	for _, info := range infos {
		if info.Doc == nil || info.Doc.Lifecycle == extract.LifecycleStale {
			continue
		}
		doc := info.Doc
		name := strings.TrimSpace(doc.Project)
		if name == "" {
			name = "unknown-project"
		}
		project, ok := byName[name]
		if !ok {
			project = &Project{Name: name, SafeName: render.SafeProject(name)}
			byName[name] = project
		}
		project.Docs++
		switch strings.ToLower(strings.TrimSpace(doc.ReviewStatus)) {
		case extract.ReviewApproved:
			project.Approved++
		case extract.ReviewRejected:
			project.Rejected++
		default:
			project.Pending++
		}
	}
	out := make([]Project, 0, len(byName))
	for _, project := range byName {
		out = append(out, *project)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func normalizeOptions(opts Options) Options {
	if opts.Limit <= 0 {
		opts.Limit = DefaultLimit
	}
	if opts.Limit > MaxLimit {
		opts.Limit = MaxLimit
	}
	opts.Review = strings.ToLower(strings.TrimSpace(opts.Review))
	if opts.Review == "" {
		opts.Review = "any"
	}
	return opts
}

func matchesProject(project string, doc *extract.Doc) bool {
	project = strings.TrimSpace(project)
	if project == "" || project == "*" || strings.EqualFold(project, "all") {
		return true
	}
	return strings.EqualFold(project, strings.TrimSpace(doc.Project)) ||
		strings.EqualFold(project, render.SafeProject(doc.Project))
}

func matchesTags(tags []string, doc *extract.Doc) bool {
	if len(tags) == 0 {
		return true
	}
	have := make(map[string]bool, len(doc.Tags))
	for _, tag := range doc.Tags {
		have[strings.ToLower(strings.TrimSpace(tag))] = true
	}
	for _, tag := range tags {
		tag = strings.ToLower(strings.TrimSpace(tag))
		if tag == "" {
			continue
		}
		if !have[tag] {
			return false
		}
	}
	return true
}

func matchesReview(review string, doc *extract.Doc) bool {
	review = strings.ToLower(strings.TrimSpace(review))
	if review == "" || review == "any" || review == "*" {
		return true
	}
	return strings.EqualFold(review, strings.TrimSpace(doc.ReviewStatus))
}

// scoreDoc uses the same ASCII-word/CJK-bigram similarity as segmentation, so
// Chinese queries work without a separate tokenizer.
func scoreDoc(query string, doc *extract.Doc) (float64, []string) {
	normalizedQuery := normalize(query)
	if normalizedQuery == "" {
		return 0, nil
	}
	var score float64
	var matched []string
	add := func(name string, weight float64, text string) {
		if strings.TrimSpace(text) == "" {
			return
		}
		similarity := segment.Similarity(query, text)
		if similarity <= 0 {
			return
		}
		score += weight * similarity
		matched = appendUnique(matched, name)
	}

	add("title", 6, doc.Title)
	add("summary", 3, doc.Summary)
	add("tags", 5, strings.Join(doc.Tags, " "))
	add("stack", 2, strings.Join(doc.Stack, " "))
	add("problem", 2, problemText(doc))
	add("root_cause", 1.5, doc.RootCause.Text)
	add("diagnosis", 1.4, diagnosisText(doc))
	add("commands", 1.5, commandsText(doc))
	add("concepts", 2.2, conceptsText(doc))
	add("rejected_hypotheses", 1.6, rejectedHypothesesText(doc))
	add("solution", 1.2, solutionText(doc))
	add("insights", 1, insightsText(doc))
	add("lessons", 1, strings.Join(doc.Lessons, "\n"))
	add("open_questions", 0.5, strings.Join(doc.OpenQuestions, "\n"))
	add("type", 0.5, doc.KnowledgeType+" "+doc.Status)

	for _, tag := range doc.Tags {
		if contains(normalize(tag), normalizedQuery) {
			score += 2
			matched = appendUnique(matched, "tag:"+strings.ToLower(strings.TrimSpace(tag)))
		}
	}
	if contains(normalize(doc.Title), normalizedQuery) {
		score += 3
		matched = appendUnique(matched, "title:exact")
	}
	if contains(normalize(doc.Summary), normalizedQuery) {
		score += 1.5
		matched = appendUnique(matched, "summary:exact")
	}
	if contains(normalize(problemText(doc)+"\n"+strings.Join(doc.Lessons, "\n")), normalizedQuery) {
		score += 1
		matched = appendUnique(matched, "content:exact")
	}
	if contains(normalize(conceptsText(doc)), normalizedQuery) {
		score += 2
		matched = appendUnique(matched, "concepts:exact")
	}
	if contains(normalize(rejectedHypothesesText(doc)), normalizedQuery) {
		score += 1
		matched = appendUnique(matched, "rejected_hypotheses:exact")
	}

	return round2(score), matched
}

func problemText(doc *extract.Doc) string {
	parts := append([]string{doc.Problem.Statement}, doc.Problem.Symptoms...)
	parts = append(parts, doc.Problem.ErrorSignatures...)
	return strings.Join(parts, "\n")
}

func diagnosisText(doc *extract.Doc) string {
	parts := make([]string, 0, len(doc.Diagnosis)*6)
	for _, step := range doc.Diagnosis {
		parts = append(parts,
			step.Observation, step.Hypothesis, step.Action,
			step.Command, step.ExpectedSignal, step.Result, step.Conclusion,
		)
	}
	return strings.Join(parts, "\n")
}

func commandsText(doc *extract.Doc) string {
	parts := make([]string, 0, len(doc.Commands)*3)
	for _, command := range doc.Commands {
		parts = append(parts, command.Purpose, command.Command, command.ExpectedSignal)
	}
	return strings.Join(parts, "\n")
}

func conceptsText(doc *extract.Doc) string {
	parts := make([]string, 0, len(doc.Concepts)*6)
	for _, concept := range doc.Concepts {
		parts = append(parts,
			concept.Name, concept.Stack, concept.WhyRelevant,
			concept.Syntax, concept.Example, concept.DocsURL,
		)
	}
	return strings.Join(parts, "\n")
}

func rejectedHypothesesText(doc *extract.Doc) string {
	parts := make([]string, 0, len(doc.RejectedHypotheses)*2)
	for _, rejected := range doc.RejectedHypotheses {
		parts = append(parts, rejected.Hypothesis, rejected.WhyRejected)
	}
	return strings.Join(parts, "\n")
}

func solutionText(doc *extract.Doc) string {
	parts := make([]string, 0, len(doc.Solution)*3)
	for _, step := range doc.Solution {
		parts = append(parts, step.Action, step.Detail, step.Command)
	}
	return strings.Join(parts, "\n")
}

func insightsText(doc *extract.Doc) string {
	parts := make([]string, 0, len(doc.Insights)+len(doc.Pitfalls))
	for _, insight := range doc.Insights {
		parts = append(parts, insight.Text)
	}
	for _, pitfall := range doc.Pitfalls {
		parts = append(parts, pitfall.Trap, pitfall.Why, pitfall.Avoid)
	}
	return strings.Join(parts, "\n")
}

func normalize(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(value)), " "))
}

func contains(haystack, needle string) bool {
	return needle != "" && strings.Contains(haystack, needle)
}

func appendUnique(values []string, addition string) []string {
	if slices.Contains(values, addition) {
		return values
	}
	return append(values, addition)
}

func nonNil(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

func round2(value float64) float64 {
	return float64(int(value*100+0.5)) / 100
}
