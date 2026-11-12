package export

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"devlog-learn/internal/cache"
	"devlog-learn/internal/extract"
	"devlog-learn/internal/index"
)

func exportTime() time.Time { return time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC) }

func exportDoc(id, project string, lifecycle string, at time.Time) *extract.Doc {
	return &extract.Doc{
		SchemaVersion: extract.SchemaVersion,
		ID:            id,
		Slug:          extract.MakeSlug("Doc "+id, at),
		Title:         "Doc " + id,
		Summary:       "summary",
		Project:       project,
		Source:        "claude-code",
		KnowledgeType: "bug_fix",
		Status:        "resolved",
		ReviewStatus:  extract.ReviewPending,
		Lifecycle:     lifecycle,
		OccurredAt:    at,
		Tags:          []string{"go"},
		Stack:         []string{"go"},
		Confidence:    0.7,
	}
}

func exportStore(t *testing.T) *cache.Store {
	t.Helper()
	store, err := cache.Open(t.TempDir())
	if err != nil {
		t.Fatalf("cache.Open: %v", err)
	}
	if err := store.SaveDoc(exportDoc("active1", "demo", extract.LifecycleActive, exportTime())); err != nil {
		t.Fatalf("SaveDoc active1: %v", err)
	}
	if err := store.SaveDoc(exportDoc("active2", "demo", extract.LifecycleActive, exportTime().Add(-time.Hour))); err != nil {
		t.Fatalf("SaveDoc active2: %v", err)
	}
	if err := store.SaveDoc(exportDoc("stale1", "demo", extract.LifecycleStale, exportTime().Add(-2*time.Hour))); err != nil {
		t.Fatalf("SaveDoc stale1: %v", err)
	}
	return store
}

func TestRenderExcludesStaleByDefault(t *testing.T) {
	store := exportStore(t)
	out := t.TempDir()
	report, err := Render(store, out, false)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if report.Written != 2 || report.StaleSkipped != 1 || len(report.Paths) != 2 {
		t.Fatalf("report = %+v", report)
	}
	for _, path := range report.Paths {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("rendered path %s: %v", path, err)
		}
	}
	all, err := Render(store, out, true)
	if err != nil {
		t.Fatalf("Render includeStale: %v", err)
	}
	if all.Written != 3 || all.StaleSkipped != 0 {
		t.Fatalf("include-stale report = %+v", all)
	}
}

func TestRenderLearningProfileWritesLearnFiles(t *testing.T) {
	store := exportStore(t)
	out := t.TempDir()

	agent, err := RenderProfile(store, out, false, ProfileAgent)
	if err != nil {
		t.Fatalf("RenderProfile agent: %v", err)
	}
	learn, err := RenderProfile(store, out, false, ProfileLearning)
	if err != nil {
		t.Fatalf("RenderProfile learning: %v", err)
	}
	if agent.Written != 2 || learn.Written != 2 {
		t.Fatalf("agent=%+v learn=%+v", agent, learn)
	}
	for _, path := range learn.Paths {
		if !strings.HasSuffix(path, ".learn.md") {
			t.Fatalf("learning path = %q", path)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if !strings.Contains(string(data), "## 症状") && !strings.Contains(string(data), "# Doc ") {
			t.Fatalf("unexpected learning output in %s:\n%s", path, data)
		}
	}
	for _, path := range agent.Paths {
		if strings.HasSuffix(path, ".learn.md") {
			t.Fatalf("agent path unexpectedly learned: %q", path)
		}
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("agent output missing after learning render: %v", err)
		}
	}
	if _, err := RenderProfile(store, out, false, "unknown"); err == nil {
		t.Fatal("unknown profile should fail")
	}
}

func TestRenderPruneRemovesStaleManagedFile(t *testing.T) {
	store := exportStore(t)
	out := t.TempDir()
	first, err := RenderProfile(store, out, false, ProfileAgent)
	if err != nil {
		t.Fatal(err)
	}
	if first.Written != 2 {
		t.Fatalf("initial render = %+v", first)
	}

	stale := exportDoc("active1", "demo", extract.LifecycleStale, exportTime())
	if err := store.SaveDoc(stale); err != nil {
		t.Fatal(err)
	}
	second, err := RenderProfileWithOptions(store, out, RenderProfileOptions{
		Profile: ProfileAgent,
		Prune:   true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if second.Written != 1 || second.Pruned != 1 {
		t.Fatalf("pruned render = %+v", second)
	}
	for _, path := range first.Paths {
		_, statErr := os.Stat(path)
		if strings.Contains(path, "active1") {
			if !os.IsNotExist(statErr) {
				t.Fatalf("stale managed file still exists: %s", path)
			}
			continue
		}
		if statErr != nil {
			t.Fatalf("active file missing: %s: %v", path, statErr)
		}
	}
}

func TestBuildIndexExcludesStale(t *testing.T) {
	store := exportStore(t)
	out := t.TempDir()
	report, warnings, err := BuildIndex(store, out, exportTime())
	if err != nil {
		t.Fatalf("BuildIndex: %v", err)
	}
	if len(warnings) != 0 || report.Docs != 2 {
		t.Fatalf("report = %+v warnings = %v", report, warnings)
	}
	data, err := os.ReadFile(filepath.Join(out, "index.json"))
	if err != nil {
		t.Fatalf("read index.json: %v", err)
	}
	var doc index.Document
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("decode index.json: %v", err)
	}
	if doc.Total != 2 || len(doc.Docs) != 2 {
		t.Fatalf("index = %+v", doc)
	}
	for _, entry := range doc.Docs {
		if entry.ID == "stale1" {
			t.Fatalf("stale doc leaked into index: %+v", entry)
		}
	}
}

func TestBuildIndexPruneRemovesStaleProjectFiles(t *testing.T) {
	store := exportStore(t)
	out := t.TempDir()
	if _, _, err := BuildIndex(store, out, exportTime()); err != nil {
		t.Fatal(err)
	}
	projectJSON := filepath.Join(out, "demo", "index.json")
	projectReadme := filepath.Join(out, "demo", "README.md")
	for _, path := range []string{projectJSON, projectReadme} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("initial index file %s: %v", path, err)
		}
	}

	for _, id := range []string{"active1", "active2"} {
		doc := exportDoc(id, "demo", extract.LifecycleStale, exportTime())
		if err := store.SaveDoc(doc); err != nil {
			t.Fatal(err)
		}
	}
	report, _, err := BuildIndexWithOptions(store, out, exportTime().Add(time.Hour), index.WriteOptions{
		Prune: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.Docs != 0 || report.Pruned != 2 {
		t.Fatalf("report = %+v", report)
	}
	for _, path := range []string{projectJSON, projectReadme} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("stale index file still exists: %s: %v", path, err)
		}
	}
}

func TestExportNilStore(t *testing.T) {
	if _, err := Render(nil, t.TempDir(), false); err == nil {
		t.Fatal("Render(nil) succeeded")
	}
	if _, _, err := BuildIndex(nil, t.TempDir(), exportTime()); err == nil {
		t.Fatal("BuildIndex(nil) succeeded")
	}
}
