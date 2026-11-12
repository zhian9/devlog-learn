package index

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"devlog-learn/internal/cache"
	"devlog-learn/internal/extract"
)

var indexTime = time.Date(2026, 5, 2, 9, 0, 0, 0, time.UTC)

func indexDoc(id, project, title string, at time.Time, tags ...string) *extract.Doc {
	return &extract.Doc{
		SchemaVersion: 1,
		ID:            id,
		Slug:          extract.MakeSlug(title, at),
		Title:         title,
		Summary:       "Summary for " + title,
		Project:       project,
		Source:        "claude-code",
		KnowledgeType: "bug_fix",
		Status:        "resolved",
		ReviewStatus:  extract.ReviewPending,
		Lifecycle:     extract.LifecycleActive,
		OccurredAt:    at,
		Tags:          tags,
		Stack:         []string{"go"},
		Confidence:    0.7,
	}
}

func saveDocs(t *testing.T, docs ...*extract.Doc) *cache.Store {
	t.Helper()
	store, err := cache.Open(t.TempDir())
	if err != nil {
		t.Fatalf("cache.Open: %v", err)
	}
	for _, doc := range docs {
		if err := store.SaveDoc(doc); err != nil {
			t.Fatalf("SaveDoc %s: %v", doc.ID, err)
		}
	}
	return store
}

func readIndex(t *testing.T, path string) Document {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var doc Document
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return doc
}

func TestWriteExcludesStaleAndWritesProjectIndexes(t *testing.T) {
	activeA := indexDoc("aaa111", "Demo Project", "Fix build", indexTime, "build")
	activeB := indexDoc("bbb222", "Demo Project", "Tune cache", indexTime.Add(-time.Hour), "cache")
	stale := indexDoc("ccc333", "Demo Project", "Removed topic", indexTime.Add(-2*time.Hour), "removed")
	stale.Lifecycle = extract.LifecycleStale
	stale.StaleReason = cache.StaleSegmentRemoved
	activeOther := indexDoc("ddd444", "Other", "Ship release", indexTime.Add(-3*time.Hour), "release")

	store := saveDocs(t, activeA, activeB, stale, activeOther)
	infos, warnings := store.ListDocs()
	if len(warnings) != 0 {
		t.Fatalf("warnings: %v", warnings)
	}

	out := t.TempDir()
	report, err := Write(out, infos, indexTime)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if report.Docs != 3 || report.Projects != 2 {
		t.Fatalf("report = %+v", report)
	}
	global := readIndex(t, filepath.Join(out, "index.json"))
	if global.Total != 3 || len(global.Docs) != 3 {
		t.Fatalf("global index = %+v", global)
	}
	for _, entry := range global.Docs {
		if entry.ID == stale.ID {
			t.Fatalf("stale doc leaked into index: %+v", entry)
		}
		if entry.Lifecycle == extract.LifecycleStale {
			t.Fatalf("stale lifecycle leaked: %+v", entry)
		}
		if !strings.Contains(entry.Path, "/") && entry.ID != activeOther.ID {
			t.Fatalf("path should be project-relative: %q", entry.Path)
		}
	}
	if global.Docs[0].ID != activeA.ID {
		t.Fatalf("newest doc should sort first, got %s", global.Docs[0].ID)
	}

	readme, err := os.ReadFile(filepath.Join(out, "README.md"))
	if err != nil {
		t.Fatalf("read global README: %v", err)
	}
	if strings.Contains(string(readme), stale.Title) {
		t.Fatalf("stale title leaked into README:\n%s", readme)
	}
	for _, want := range []string{"Active documents: 3", "Demo-Project/index.json", activeA.Title, activeOther.Title} {
		if !strings.Contains(string(readme), want) {
			t.Errorf("global README missing %q:\n%s", want, readme)
		}
	}

	projectJSON := filepath.Join(out, "Demo-Project", "index.json")
	project := readIndex(t, projectJSON)
	if project.Project != "Demo Project" || project.Total != 2 {
		t.Fatalf("project index = %+v", project)
	}
	projectReadme, err := os.ReadFile(filepath.Join(out, "Demo-Project", "README.md"))
	if err != nil {
		t.Fatalf("read project README: %v", err)
	}
	if !strings.Contains(string(projectReadme), "("+filepath.Base(project.Docs[0].Path)+")") {
		t.Fatalf("project README should use base-name links:\n%s", projectReadme)
	}
	if strings.Contains(string(projectReadme), stale.Title) {
		t.Fatalf("stale title leaked into project README:\n%s", projectReadme)
	}

	// Stale docs remain on disk.
	if _, err := store.LoadDoc(stale.ID); err != nil {
		t.Fatalf("stale doc was removed from disk: %v", err)
	}
}

func TestWriteEmptyIndex(t *testing.T) {
	out := t.TempDir()
	report, err := Write(out, nil, indexTime)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if report.Docs != 0 || report.Projects != 0 {
		t.Fatalf("report = %+v", report)
	}
	global := readIndex(t, filepath.Join(out, "index.json"))
	if global.Total != 0 || len(global.Docs) != 0 {
		t.Fatalf("global = %+v", global)
	}
}

func TestRelatedDocsScoringAndFiltering(t *testing.T) {
	target := indexDoc("t1", "demo", "Target", indexTime, "go", "ci")
	target.Stack = []string{"windows"}

	best := indexDoc("a1", "demo", "Best", indexTime, "go", "ci")
	best.Stack = []string{"windows"}
	middle := indexDoc("b1", "other", "Middle", indexTime, "go")
	middle.Stack = []string{"windows"}
	unrelated := indexDoc("c1", "other", "Unrelated", indexTime, "design")
	unrelated.KnowledgeType = "learning"
	unrelated.Stack = []string{"figma"}

	got := RelatedDocs(target, []*extract.Doc{target, best, middle, unrelated})
	if len(got) != 2 {
		t.Fatalf("related = %+v", got)
	}
	if got[0].ID != best.ID || got[0].Score <= got[1].Score {
		t.Fatalf("expected best first: %+v", got)
	}
	if got[0].Score != 3*2+2+1+1 {
		t.Fatalf("best score = %d, want 10", got[0].Score)
	}
	for _, item := range got {
		if item.ID == target.ID {
			t.Fatalf("target included in its own related list")
		}
	}
	if !containsReason(got[0].Reasons, "tag:go") || !containsReason(got[0].Reasons, "stack:windows") {
		t.Fatalf("reasons = %v", got[0].Reasons)
	}
}

func TestRelatedDocsDeterministicAndCapped(t *testing.T) {
	target := indexDoc("t1", "demo", "Target", indexTime, "go")
	var docs []*extract.Doc
	docs = append(docs, target)
	for _, id := range []string{"z9", "a1", "m5", "b2", "c3", "d4", "e5"} {
		docs = append(docs, indexDoc(id, "demo", "Doc "+id, indexTime, "go"))
	}
	first := RelatedDocs(target, docs)
	second := RelatedDocs(target, docs)
	if len(first) != MaxRelated {
		t.Fatalf("related count = %d", len(first))
	}
	for i := range first {
		if first[i].ID != second[i].ID {
			t.Fatalf("non-deterministic order")
		}
	}
	// Equal scores must break ties by ID.
	for i := 1; i < len(first); i++ {
		if first[i-1].Score == first[i].Score && first[i-1].ID > first[i].ID {
			t.Fatalf("ties not sorted by id: %+v", first)
		}
	}
}

func containsReason(reasons []string, want string) bool {
	for _, reason := range reasons {
		if reason == want {
			return true
		}
	}
	return false
}
