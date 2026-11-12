// Package index builds global and per-project JSON/Markdown indexes from
// canonical documents. Indexes always exclude stale documents; stale files stay
// on disk and can return to active later. Related links are computed read-only:
// indexes never merge or rewrite documents.
package index

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"devlog-learn/internal/cache"
	"devlog-learn/internal/extract"
	"devlog-learn/internal/manifest"
	"devlog-learn/internal/render"
)

// SchemaVersion covers the index JSON shape.
const SchemaVersion = 1

// MaxRelated caps the read-only related list attached to each entry.
const MaxRelated = 5

// Related is a read-only association between two documents.
type Related struct {
	ID      string   `json:"id"`
	Title   string   `json:"title"`
	Score   int      `json:"score"`
	Reasons []string `json:"reasons"`
}

// DocEntry is one document's index record.
type DocEntry struct {
	ID            string    `json:"id"`
	Title         string    `json:"title"`
	Slug          string    `json:"slug"`
	Project       string    `json:"project"`
	Source        string    `json:"source"`
	KnowledgeType string    `json:"knowledge_type"`
	Status        string    `json:"status"`
	ReviewStatus  string    `json:"review_status"`
	Lifecycle     string    `json:"lifecycle"`
	OccurredAt    time.Time `json:"occurred_at"`
	Confidence    float64   `json:"confidence"`
	Tags          []string  `json:"tags,omitempty"`
	Stack         []string  `json:"stack,omitempty"`
	Path          string    `json:"path"`
	Related       []Related `json:"related,omitempty"`
}

// ProjectEntry summarizes one project in the global index.
type ProjectEntry struct {
	Name   string `json:"name"`
	Docs   int    `json:"documents"`
	Index  string `json:"index"`
	Readme string `json:"readme"`
}

// Document is the on-disk index.json shape for both global and project indexes.
type Document struct {
	SchemaVersion int            `json:"schema_version"`
	GeneratedAt   time.Time      `json:"generated_at"`
	Project       string         `json:"project,omitempty"`
	Total         int            `json:"total"`
	Projects      []ProjectEntry `json:"projects,omitempty"`
	Docs          []DocEntry     `json:"docs"`
}

// Report lists the files written by Write.
type Report struct {
	Docs          int
	Projects      int
	GlobalJSON    string
	GlobalReadme  string
	ProjectJSON   []string
	ProjectReadme []string
	Pruned        int
	PruneSkipped  int
	Warnings      []string
}

// WriteOptions controls output maintenance while building indexes.
type WriteOptions struct {
	Prune bool
}

// Write renders index.json and README.md globally and for every project.
// Stale documents are filtered defensively even when the caller already used
// cache.Store.ListActiveDocs.
func Write(outDir string, infos []cache.DocInfo, now time.Time) (Report, error) {
	return WriteWithOptions(outDir, infos, now, WriteOptions{})
}

// WriteWithOptions renders indexes and optionally prunes stale index files.
func WriteWithOptions(outDir string, infos []cache.DocInfo, now time.Time, opts WriteOptions) (Report, error) {
	docs := activeDocs(infos)
	plans := render.PlanDocs(docs)

	pathByID := make(map[string]string, len(plans))
	allDocs := make([]*extract.Doc, 0, len(plans))
	for _, plan := range plans {
		pathByID[plan.Doc.ID] = plan.Path
		allDocs = append(allDocs, plan.Doc)
	}

	entries := make([]DocEntry, 0, len(plans))
	for _, plan := range plans {
		entries = append(entries, newDocEntry(plan.Doc, plan.Path, RelatedDocs(plan.Doc, allDocs)))
	}
	sortDocEntries(entries)

	groups := groupProjects(entries)
	global := Document{
		SchemaVersion: SchemaVersion,
		GeneratedAt:   now.UTC(),
		Total:         len(entries),
		Projects:      groups.projects,
		Docs:          entries,
	}

	report := Report{Docs: len(entries), Projects: len(groups.projects)}
	generated := make([]manifest.GeneratedFile, 0, 2+2*len(groups.groups))
	var errs []error

	globalJSON := filepath.Join(outDir, "index.json")
	if err := writeJSON(globalJSON, global); err != nil {
		errs = append(errs, err)
	} else {
		generated = append(generated, manifest.GeneratedFile{Path: "index.json"})
	}
	report.GlobalJSON = globalJSON

	globalReadme := filepath.Join(outDir, "README.md")
	if err := writeFile(globalReadme, []byte(globalReadmeMarkdown(global))); err != nil {
		errs = append(errs, err)
	} else {
		generated = append(generated, manifest.GeneratedFile{Path: "README.md"})
	}
	report.GlobalReadme = globalReadme

	for _, group := range groups.groups {
		projectEntries := make([]DocEntry, 0, len(entries))
		for _, entry := range entries {
			if render.SafeProject(entry.Project) == group.dir {
				projectEntries = append(projectEntries, entry)
			}
		}
		projectDoc := Document{
			SchemaVersion: SchemaVersion,
			GeneratedAt:   now.UTC(),
			Project:       group.name,
			Total:         len(projectEntries),
			Docs:          projectEntries,
		}
		jsonPath := filepath.Join(outDir, filepath.FromSlash(group.dir), "index.json")
		if err := writeJSON(jsonPath, projectDoc); err != nil {
			errs = append(errs, err)
			continue
		}
		report.ProjectJSON = append(report.ProjectJSON, jsonPath)
		generated = append(generated, manifest.GeneratedFile{
			Path: filepath.ToSlash(filepath.Join(group.dir, "index.json")),
		})

		readmePath := filepath.Join(outDir, filepath.FromSlash(group.dir), "README.md")
		if err := writeFile(readmePath, []byte(projectReadmeMarkdown(projectDoc))); err != nil {
			errs = append(errs, err)
			continue
		}
		report.ProjectReadme = append(report.ProjectReadme, readmePath)
		generated = append(generated, manifest.GeneratedFile{
			Path: filepath.ToSlash(filepath.Join(group.dir, "README.md")),
		})
	}

	if len(errs) > 0 {
		opts.Prune = false
	}
	manifestReport, manifestErr := manifest.Reconcile(outDir, "index", generated, opts.Prune, now.UTC())
	report.Pruned = manifestReport.Pruned
	report.PruneSkipped = manifestReport.Skipped
	report.Warnings = append(report.Warnings, manifestReport.Warnings...)
	errs = append(errs, manifestErr)
	return report, errors.Join(errs...)
}

func activeDocs(infos []cache.DocInfo) []*extract.Doc {
	docs := make([]*extract.Doc, 0, len(infos))
	for _, info := range infos {
		if info.Doc == nil || info.Doc.Lifecycle == extract.LifecycleStale {
			continue
		}
		docs = append(docs, info.Doc)
	}
	return docs
}

func newDocEntry(doc *extract.Doc, relPath string, related []Related) DocEntry {
	return DocEntry{
		ID:            doc.ID,
		Title:         doc.Title,
		Slug:          doc.Slug,
		Project:       doc.Project,
		Source:        doc.Source,
		KnowledgeType: doc.KnowledgeType,
		Status:        doc.Status,
		ReviewStatus:  doc.ReviewStatus,
		Lifecycle:     doc.Lifecycle,
		OccurredAt:    doc.OccurredAt,
		Confidence:    doc.Confidence,
		Tags:          doc.Tags,
		Stack:         doc.Stack,
		Path:          relPath,
		Related:       related,
	}
}

func sortDocEntries(entries []DocEntry) {
	sort.SliceStable(entries, func(i, j int) bool {
		a, b := entries[i].OccurredAt, entries[j].OccurredAt
		if !a.Equal(b) {
			if a.IsZero() {
				return false
			}
			if b.IsZero() {
				return true
			}
			return a.After(b)
		}
		return entries[i].ID < entries[j].ID
	})
}

type projectGroup struct {
	dir  string
	name string
}

type projectGroups struct {
	projects []ProjectEntry
	groups   []projectGroup
}

func groupProjects(entries []DocEntry) projectGroups {
	byDir := map[string]*ProjectEntry{}
	for _, entry := range entries {
		dir := render.SafeProject(entry.Project)
		item, ok := byDir[dir]
		if !ok {
			name := strings.TrimSpace(entry.Project)
			if name == "" {
				name = dir
			}
			item = &ProjectEntry{
				Name:   name,
				Index:  path.Join(dir, "index.json"),
				Readme: path.Join(dir, "README.md"),
			}
			byDir[dir] = item
		}
		item.Docs++
	}

	dirs := make([]string, 0, len(byDir))
	for dir := range byDir {
		dirs = append(dirs, dir)
	}
	sort.Strings(dirs)

	out := projectGroups{groups: make([]projectGroup, 0, len(dirs))}
	for _, dir := range dirs {
		item := byDir[dir]
		out.projects = append(out.projects, *item)
		out.groups = append(out.groups, projectGroup{dir: dir, name: item.Name})
	}
	sort.Slice(out.projects, func(i, j int) bool { return out.projects[i].Name < out.projects[j].Name })
	return out
}

// Related computes a deterministic, read-only association list. Shared tags
// weigh most; shared stack, knowledge type and project add smaller signals.
func RelatedDocs(target *extract.Doc, docs []*extract.Doc) []Related {
	if target == nil {
		return nil
	}
	tags := lowerSet(target.Tags)
	stack := lowerSet(target.Stack)
	var out []Related

	for _, other := range docs {
		if other == nil || other.ID == target.ID {
			continue
		}
		score := 0
		reasons := make([]string, 0, 4)

		for _, tag := range sortedShared(tags, lowerSet(other.Tags)) {
			score += 3
			reasons = append(reasons, "tag:"+tag)
		}
		for _, item := range sortedShared(stack, lowerSet(other.Stack)) {
			score += 2
			reasons = append(reasons, "stack:"+item)
		}
		if target.KnowledgeType != "" && strings.EqualFold(strings.TrimSpace(target.KnowledgeType), strings.TrimSpace(other.KnowledgeType)) {
			score++
			reasons = append(reasons, "type:"+strings.ToLower(strings.TrimSpace(target.KnowledgeType)))
		}
		if strings.TrimSpace(target.Project) != "" && strings.EqualFold(strings.TrimSpace(target.Project), strings.TrimSpace(other.Project)) {
			score++
			reasons = append(reasons, "project:"+strings.TrimSpace(target.Project))
		}
		if score == 0 {
			continue
		}
		out = append(out, Related{ID: other.ID, Title: other.Title, Score: score, Reasons: reasons})
	}

	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return out[i].ID < out[j].ID
	})
	if len(out) > MaxRelated {
		out = out[:MaxRelated]
	}
	return out
}

func lowerSet(values []string) map[string]bool {
	out := make(map[string]bool, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value != "" {
			out[value] = true
		}
	}
	return out
}

func sortedShared(a, b map[string]bool) []string {
	var out []string
	for value := range a {
		if b[value] {
			out = append(out, value)
		}
	}
	sort.Strings(out)
	return out
}

func globalReadmeMarkdown(doc Document) string {
	var b strings.Builder
	b.WriteString("# devlog-learn knowledge index\n\n")
	fmt.Fprintf(&b, "Generated: %s\n\n", doc.GeneratedAt.UTC().Format(time.RFC3339))
	fmt.Fprintf(&b, "Active documents: %d\n\n", doc.Total)
	fmt.Fprintf(&b, "Projects: %d\n\n", len(doc.Projects))
	b.WriteString("Stale documents are excluded from this index but remain in the cache.\n\n")

	b.WriteString("## Projects\n\n")
	if len(doc.Projects) == 0 {
		b.WriteString("_No projects._\n\n")
	} else {
		b.WriteString("| Project | Documents | Index |\n|---|---:|---|\n")
		for _, project := range doc.Projects {
			fmt.Fprintf(&b, "| %s | %d | [index.json](%s) |\n",
				cell(project.Name), project.Docs, project.Index)
		}
		b.WriteString("\n")
	}

	b.WriteString("## Documents\n\n")
	writeReadmeDocTable(&b, doc.Docs, true)
	return b.String()
}

func projectReadmeMarkdown(doc Document) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", cell(doc.Project))
	fmt.Fprintf(&b, "Generated: %s\n\n", doc.GeneratedAt.UTC().Format(time.RFC3339))
	fmt.Fprintf(&b, "Active documents: %d\n\n", doc.Total)
	b.WriteString("## Documents\n\n")
	writeReadmeDocTable(&b, doc.Docs, false)
	return b.String()
}

func writeReadmeDocTable(b *strings.Builder, entries []DocEntry, includeProject bool) {
	if len(entries) == 0 {
		b.WriteString("_No active documents._\n")
		return
	}
	if includeProject {
		b.WriteString("| Date | Title | Project | Type | Status | Review | Confidence |\n")
		b.WriteString("|---|---|---|---|---|---|---:|\n")
	} else {
		b.WriteString("| Date | Title | Type | Status | Review | Confidence | Tags |\n")
		b.WriteString("|---|---|---|---|---|---:|---|\n")
	}
	for _, entry := range entries {
		date := ""
		if !entry.OccurredAt.IsZero() {
			date = entry.OccurredAt.Format("2006-01-02")
		}
		link := "[" + linkText(entry.Title) + "](" + entry.Path + ")"
		if includeProject {
			fmt.Fprintf(b, "| %s | %s | %s | %s | %s | %s | %.2f |\n",
				date, link, cell(entry.Project), cell(entry.KnowledgeType), cell(entry.Status),
				cell(entry.ReviewStatus), entry.Confidence)
			continue
		}
		// Project indexes link to the Markdown file by base name.
		link = "[" + linkText(entry.Title) + "](" + path.Base(entry.Path) + ")"
		fmt.Fprintf(b, "| %s | %s | %s | %s | %s | %.2f | %s |\n",
			date, link, cell(entry.KnowledgeType), cell(entry.Status),
			cell(entry.ReviewStatus), entry.Confidence, cell(strings.Join(entry.Tags, ", ")))
	}
	b.WriteString("\n")
}

func cell(value string) string {
	value = strings.TrimSpace(value)
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.ReplaceAll(value, "\n", "<br>")
	value = strings.ReplaceAll(value, "|", `\|`)
	if value == "" {
		return "-"
	}
	return value
}

func linkText(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "Untitled"
	}
	value = strings.ReplaceAll(value, "[", `\[`)
	value = strings.ReplaceAll(value, "]", `\]`)
	return value
}

func writeJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("encode %s: %w", path, err)
	}
	data = append(data, '\n')
	return writeFile(path, data)
}

func writeFile(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create directory %s: %w", dir, err)
	}
	f, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp file for %s: %w", path, err)
	}
	tmp := f.Name()
	cleanup := func() {
		_ = f.Close()
		_ = os.Remove(tmp)
	}
	if _, err := f.Write(data); err != nil {
		cleanup()
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("close %s: %w", path, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename %s: %w", path, err)
	}
	return nil
}
