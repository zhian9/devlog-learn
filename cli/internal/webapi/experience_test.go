package webapi

import (
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"devlog-learn/internal/cache"
	"devlog-learn/internal/experience"
	"devlog-learn/internal/extract"
)

func TestExperiencesListAndDetail(t *testing.T) {
	server, cacheDir, _ := newTestServer(t)

	response := get(t, server, "/api/experiences")
	var list ExperiencesResponse
	decode(t, response, &list)
	if list.Count != 1 || len(list.Experiences) != 1 || list.Experiences[0].Project != "demo" {
		t.Fatalf("list = %+v", list)
	}
	if list.Experiences[0].MarkdownPath != "demo/EXPERIENCE.md" {
		t.Fatalf("markdown path = %q", list.Experiences[0].MarkdownPath)
	}

	response = get(t, server, "/api/experiences/demo")
	var detail experience.Experience
	decode(t, response, &detail)
	if detail.Project != "demo" || detail.Kind != experience.KindProject {
		t.Fatalf("detail = %+v", detail)
	}
	if len(detail.ReviewChecklist) == 0 {
		t.Fatal("review checklist is empty")
	}
	if detail.Paths == nil || detail.Stack == nil || detail.DocRefs == nil {
		t.Fatalf("detail contains nil slices: %+v", detail)
	}

	if _, err := os.Stat(filepath.Join(cacheDir, "experiences.json")); !os.IsNotExist(err) {
		t.Fatalf("GET created experiences.json: %v", err)
	}
}

func TestExperiencesEmptyListSerializesAsArray(t *testing.T) {
	server, err := New(Options{
		Addr:     "127.0.0.1:0",
		CacheDir: t.TempDir(),
		Token:    "test-token",
	})
	if err != nil {
		t.Fatal(err)
	}
	response := get(t, server, "/api/experiences")
	var raw map[string]json.RawMessage
	decode(t, response, &raw)
	if string(raw["experiences"]) != "[]" {
		t.Fatalf("experiences = %s", raw["experiences"])
	}
}

func TestExperienceDetailCJKAndErrors(t *testing.T) {
	server, cacheDir, _ := newTestServer(t)
	store, err := cache.Open(cacheDir)
	if err != nil {
		t.Fatal(err)
	}
	doc := fixtureDoc("cccccccccccccccc", "CJK doc", extract.LifecycleActive)
	doc.Project = "项目 一"
	if err := store.SaveDoc(doc); err != nil {
		t.Fatal(err)
	}

	response := get(t, server, "/api/experiences/"+url.PathEscape("项目 一"))
	var detail experience.Experience
	decode(t, response, &detail)
	if detail.Project != "项目 一" {
		t.Fatalf("detail project = %q", detail.Project)
	}

	response = get(t, server, "/api/experiences/missing")
	if response.Code != http.StatusNotFound {
		t.Fatalf("missing status = %d body = %s", response.Code, response.Body.String())
	}

	response = get(t, server, "/api/experiences/"+strings.Repeat("a", 201))
	if response.Code != http.StatusBadRequest {
		t.Fatalf("long name status = %d body = %s", response.Code, response.Body.String())
	}
}

func TestExportWritesExperience(t *testing.T) {
	server, _, _ := newTestServer(t)
	response := post(t, server, "/api/export", `{"profile":"agent","experience":true}`)
	var result ExportResponse
	decode(t, response, &result)
	if result.ExperienceWritten == 0 {
		t.Fatalf("experience_written = %d, warnings = %v", result.ExperienceWritten, result.Warnings)
	}
	if _, err := os.Stat(filepath.Join(server.opts.OutDir, "EXPERIENCE.md")); err != nil {
		t.Fatalf("EXPERIENCE.md missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(server.opts.OutDir, "demo", "EXPERIENCE.md")); err != nil {
		t.Fatalf("project EXPERIENCE.md missing: %v", err)
	}
	// The experience output is written before the index, so its README link is present.
	readme, err := os.ReadFile(filepath.Join(server.opts.OutDir, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(readme), "[EXPERIENCE.md](EXPERIENCE.md)") {
		t.Fatalf("index README missing experience link:\n%s", readme)
	}
}

func TestExperienceDetailAmbiguous(t *testing.T) {
	server, cacheDir, _ := newTestServer(t)
	store, err := cache.Open(cacheDir)
	if err != nil {
		t.Fatal(err)
	}
	first := fixtureDoc("dddddddddddddddd", "Upper", extract.LifecycleActive)
	first.Project = "Foo"
	second := fixtureDoc("eeeeeeeeeeeeeeee", "Lower", extract.LifecycleActive)
	second.Project = "foo"
	for _, doc := range []*extract.Doc{first, second} {
		if err := store.SaveDoc(doc); err != nil {
			t.Fatal(err)
		}
	}

	response := get(t, server, "/api/experiences/FOO")
	if response.Code != http.StatusConflict {
		t.Fatalf("ambiguous status = %d body = %s", response.Code, response.Body.String())
	}
}
