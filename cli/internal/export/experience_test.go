package export

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"devlog-learn/internal/cache"
	"devlog-learn/internal/extract"
	"devlog-learn/internal/manifest"
)

func TestRenderExperiencesWritesProjectAndOverview(t *testing.T) {
	store := exportStore(t)
	out := t.TempDir()

	report, err := RenderExperiences(store, out, nil, ExperienceRenderOptions{})
	if err != nil {
		t.Fatalf("RenderExperiences: %v", err)
	}
	if report.Written != 2 {
		t.Fatalf("written = %d, paths = %v", report.Written, report.Paths)
	}
	for _, rel := range []string{"demo/EXPERIENCE.md", "EXPERIENCE.md"} {
		path := filepath.Join(out, filepath.FromSlash(rel))
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("missing %s: %v", rel, err)
		}
	}
	overview, err := os.ReadFile(filepath.Join(out, "EXPERIENCE.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(overview), "demo") {
		t.Fatalf("overview = %s", overview)
	}

	loaded, err := manifest.Load(out)
	if err != nil {
		t.Fatalf("manifest.Load: %v", err)
	}
	for rel, record := range loaded.Files {
		if record.Profile != ProfileExperience {
			t.Fatalf("%s profile = %q", rel, record.Profile)
		}
	}
}

func TestRenderExperiencesPrunesOnlyExperienceFiles(t *testing.T) {
	store := exportStore(t)
	out := t.TempDir()

	agentReport, err := Render(store, out, false)
	if err != nil {
		t.Fatalf("Render agent: %v", err)
	}
	agentPath := ""
	for _, path := range agentReport.Paths {
		if strings.Contains(path, string(filepath.Separator)+"demo"+string(filepath.Separator)) {
			agentPath = path
			break
		}
	}
	if agentPath == "" {
		t.Fatal("agent output path not found")
	}

	// Add a second project, render it, then remove its Doc and prune.
	extra := exportDoc("extra1", "extra-project", extract.LifecycleActive, exportTime())
	if err := store.SaveDoc(extra); err != nil {
		t.Fatal(err)
	}
	if _, err := RenderExperiences(store, out, nil, ExperienceRenderOptions{}); err != nil {
		t.Fatalf("first render: %v", err)
	}
	extraPath := filepath.Join(out, "extra-project", "EXPERIENCE.md")
	if _, err := os.Stat(extraPath); err != nil {
		t.Fatalf("extra experience missing: %v", err)
	}
	if err := os.Remove(filepath.Join(store.Root, "docs", extra.ID+".json")); err != nil {
		t.Fatal(err)
	}

	report, err := RenderExperiences(store, out, nil, ExperienceRenderOptions{Prune: true})
	if err != nil {
		t.Fatalf("prune render: %v", err)
	}
	if report.Pruned == 0 {
		t.Fatalf("nothing pruned: %+v", report)
	}
	if _, err := os.Stat(extraPath); !os.IsNotExist(err) {
		t.Fatalf("stale experience still exists: %v", err)
	}
	if _, err := os.Stat(agentPath); err != nil {
		t.Fatalf("agent output was removed: %v", err)
	}
}

func TestRenderExperiencesProjectNamedExperiencesDoesNotCollide(t *testing.T) {
	store := exportStore(t)
	if err := store.SaveDoc(exportDoc("exp1", "experiences", extract.LifecycleActive, exportTime())); err != nil {
		t.Fatal(err)
	}
	out := t.TempDir()
	if _, err := RenderExperiences(store, out, nil, ExperienceRenderOptions{}); err != nil {
		t.Fatalf("RenderExperiences: %v", err)
	}
	if _, err := os.Stat(filepath.Join(out, "EXPERIENCE.md")); err != nil {
		t.Fatalf("overview missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(out, "experiences", "EXPERIENCE.md")); err != nil {
		t.Fatalf("project experience missing: %v", err)
	}
}

func TestRenderExperiencesProjectFilter(t *testing.T) {
	store := exportStore(t)
	if err := store.SaveDoc(exportDoc("exp2", "other", extract.LifecycleActive, exportTime())); err != nil {
		t.Fatal(err)
	}
	out := t.TempDir()
	report, err := RenderExperiences(store, out, nil, ExperienceRenderOptions{Project: "demo"})
	if err != nil {
		t.Fatalf("RenderExperiences: %v", err)
	}
	if _, err := os.Stat(filepath.Join(out, "demo", "EXPERIENCE.md")); err != nil {
		t.Fatalf("demo missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(out, "other", "EXPERIENCE.md")); !os.IsNotExist(err) {
		t.Fatalf("other should not be rendered: %v", err)
	}
	if report.Written != 2 {
		t.Fatalf("written = %d", report.Written)
	}
}

func TestRenderExperiencesProjectFilterDoesNotPruneOthers(t *testing.T) {
	store := exportStore(t)
	if err := store.SaveDoc(exportDoc("exp3", "other", extract.LifecycleActive, exportTime())); err != nil {
		t.Fatal(err)
	}
	out := t.TempDir()
	if _, err := RenderExperiences(store, out, nil, ExperienceRenderOptions{}); err != nil {
		t.Fatalf("initial render: %v", err)
	}
	otherPath := filepath.Join(out, "other", "EXPERIENCE.md")
	if _, err := os.Stat(otherPath); err != nil {
		t.Fatalf("other experience missing: %v", err)
	}

	report, err := RenderExperiences(store, out, nil, ExperienceRenderOptions{Project: "demo", Prune: true})
	if err != nil {
		t.Fatalf("filtered render: %v", err)
	}
	if _, err := os.Stat(otherPath); err != nil {
		t.Fatalf("filtered render pruned another project: %v", err)
	}
	if !strings.Contains(strings.Join(report.Warnings, " "), "--prune is ignored") {
		t.Fatalf("missing prune warning: %+v", report.Warnings)
	}
}

func TestRenderExperiencesEmptyCache(t *testing.T) {
	store, err := cache.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	out := t.TempDir()
	report, err := RenderExperiences(store, out, nil, ExperienceRenderOptions{})
	if err != nil {
		t.Fatalf("RenderExperiences: %v", err)
	}
	if report.Written != 1 {
		t.Fatalf("written = %d", report.Written)
	}
	data, err := os.ReadFile(filepath.Join(out, "EXPERIENCE.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "暂无经历") {
		t.Fatalf("overview = %s", data)
	}
}
