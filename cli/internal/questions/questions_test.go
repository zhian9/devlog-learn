package questions

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"devlog-learn/internal/cache"
	"devlog-learn/internal/extract"
)

var questionTime = time.Date(2026, 11, 11, 10, 0, 0, 0, time.UTC)

func questionDoc(id, project string, questions ...string) *extract.Doc {
	return &extract.Doc{
		SchemaVersion: 1,
		ID:            id,
		Title:         "Doc " + id,
		Project:       project,
		ReviewStatus:  extract.ReviewPending,
		Lifecycle:     extract.LifecycleActive,
		OccurredAt:    questionTime,
		OpenQuestions: questions,
	}
}

func infos(docs ...*extract.Doc) []cache.DocInfo {
	out := make([]cache.DocInfo, 0, len(docs))
	for _, doc := range docs {
		out = append(out, cache.DocInfo{ID: doc.ID, Path: doc.ID + ".json", Doc: doc})
	}
	return out
}

func TestNewIDIsStableAndFormattingInsensitive(t *testing.T) {
	a := NewID("doc1", "Why does render fail?")
	b := NewID("doc1", "  WHY   does render fail? ")
	c := NewID("doc1", "Why does index fail?")
	d := NewID("doc2", "Why does render fail?")
	if a != b {
		t.Fatalf("formatting changed id: %q != %q", a, b)
	}
	if a == c || a == d {
		t.Fatalf("distinct questions/doc ids collided: %q %q %q", a, c, d)
	}
	if !strings.HasPrefix(a, "q_") || len(a) != 14 {
		t.Fatalf("unexpected id format %q", a)
	}
}

func TestCollectDefaultsToOpenAndJoinsResolvedState(t *testing.T) {
	doc := questionDoc("doc1", "cli", "First question?", "Second question?")
	state := New()
	first := NewID(doc.ID, "First question?")
	state.Items[first] = Item{DocID: doc.ID, Question: "First question?", Status: StatusResolved, Note: "fixed", ResolvedAt: questionTime}

	open := Collect(infos(doc), state, Filter{})
	if len(open) != 1 || open[0].Question != "Second question?" || open[0].Status != StatusOpen {
		t.Fatalf("open = %+v", open)
	}

	all := Collect(infos(doc), state, Filter{Status: "all"})
	if len(all) != 2 {
		t.Fatalf("all = %+v", all)
	}
	var resolved *Entry
	for i := range all {
		if all[i].ID == first {
			resolved = &all[i]
		}
	}
	if resolved == nil || resolved.Status != StatusResolved || resolved.Note != "fixed" {
		t.Fatalf("resolved entry = %+v", resolved)
	}

	only := Collect(infos(doc), state, Filter{Status: "resolved"})
	if len(only) != 1 || only[0].ID != first {
		t.Fatalf("resolved filter = %+v", only)
	}
}

func TestCollectFiltersProjectReviewDocAndStale(t *testing.T) {
	alpha := questionDoc("a1", "Alpha", "Alpha question?")
	alpha.ReviewStatus = extract.ReviewApproved
	beta := questionDoc("b1", "Beta", "Beta question?")
	stale := questionDoc("s1", "Alpha", "Stale question?")
	stale.Lifecycle = extract.LifecycleStale
	list := infos(alpha, beta, stale)

	if got := Collect(list, New(), Filter{Project: "alpha"}); len(got) != 1 || got[0].DocID != "a1" {
		t.Fatalf("project filter = %+v", got)
	}
	if got := Collect(list, New(), Filter{Project: "Alpha"}); len(got) != 1 || got[0].DocID != "a1" {
		t.Fatalf("case-insensitive project filter = %+v", got)
	}
	if got := Collect(list, New(), Filter{Review: extract.ReviewApproved}); len(got) != 1 || got[0].DocID != "a1" {
		t.Fatalf("review filter = %+v", got)
	}
	if got := Collect(list, New(), Filter{Review: "any"}); len(got) != 2 {
		t.Fatalf("any review filter = %+v", got)
	}
	if got := Collect(list, New(), Filter{DocID: "b1"}); len(got) != 1 || got[0].DocID != "b1" {
		t.Fatalf("doc filter = %+v", got)
	}
	for _, entry := range Collect(list, New(), Filter{Status: "all"}) {
		if entry.DocID == "s1" {
			t.Fatalf("stale doc leaked into questions: %+v", entry)
		}
	}
}

func TestCollectDeduplicatesRepeatedQuestions(t *testing.T) {
	doc := questionDoc("doc1", "cli", "Same?", "Same?")
	got := Collect(infos(doc), New(), Filter{Status: "all"})
	if len(got) != 1 {
		t.Fatalf("duplicate question not collapsed: %+v", got)
	}
}

func TestResolveReopenRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, FileName)
	doc := questionDoc("doc1", "cli", "Need a fix?")
	entry := Collect(infos(doc), New(), Filter{})[0]

	state := New()
	state.Resolve(entry, "  done in v0.9  ", questionTime)
	if err := state.Save(path, questionTime); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	item, ok := loaded.Items[entry.ID]
	if !ok || item.Note != "done in v0.9" || item.Status != StatusResolved {
		t.Fatalf("item = %+v", item)
	}
	if !loaded.Reopen(entry.ID) {
		t.Fatal("Reopen reported no entry")
	}
	if loaded.Reopen(entry.ID) {
		t.Fatal("second Reopen should report no entry")
	}
	if err := loaded.Save(path, questionTime); err != nil {
		t.Fatalf("Save after reopen: %v", err)
	}
	loaded, err = Load(path)
	if err != nil {
		t.Fatalf("Load after reopen: %v", err)
	}
	if _, ok := loaded.Items[entry.ID]; ok {
		t.Fatalf("reopened item still present: %+v", loaded.Items)
	}
}

func TestLoadMissingCorruptAndVersion(t *testing.T) {
	dir := t.TempDir()
	state, err := Load(filepath.Join(dir, "missing.json"))
	if err != nil || len(state.Items) != 0 {
		t.Fatalf("missing load = %+v, %v", state, err)
	}

	corrupt := filepath.Join(dir, "corrupt.json")
	if err := os.WriteFile(corrupt, []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(corrupt); err == nil {
		t.Fatal("corrupt state accepted")
	}

	versioned := filepath.Join(dir, "version.json")
	if err := os.WriteFile(versioned, []byte(`{"version":99,"items":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(versioned); err == nil || !strings.Contains(err.Error(), "version") {
		t.Fatalf("version error = %v", err)
	}
}

func TestResolvedIDs(t *testing.T) {
	state := New()
	state.Items["q_a"] = Item{Status: StatusResolved}
	state.Items["q_b"] = Item{Status: "other"}
	ids := state.ResolvedIDs()
	if !ids["q_a"] || ids["q_b"] {
		t.Fatalf("ResolvedIDs = %+v", ids)
	}
}
