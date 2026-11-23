package experience

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"devlog-learn/internal/cache"
	"devlog-learn/internal/extract"
	"devlog-learn/internal/segment"
)

var testTime = time.Date(2025, 8, 1, 10, 0, 0, 0, time.UTC)

func testDoc(id, project, title string, at time.Time) *extract.Doc {
	return &extract.Doc{
		SchemaVersion: extract.SchemaVersion,
		ID:            id,
		Slug:          extract.MakeSlug(title, at),
		Title:         title,
		Summary:       "summary of " + title,
		Project:       project,
		Source:        "claude-code",
		KnowledgeType: "bug_fix",
		Status:        "resolved",
		ReviewStatus:  extract.ReviewPending,
		Lifecycle:     extract.LifecycleActive,
		OccurredAt:    at,
		EndedAt:       at.Add(10 * time.Minute),
		Difficulty:    "high",
		LearningValue: "high",
		Stack:         []string{"Go", "PostgreSQL"},
		Tags:          []string{"backend"},
		Confidence:    0.8,
		Problem:       extract.Problem{Statement: "Something broke in " + title},
		RootCause:     extract.RootCause{Text: "root cause of " + title, Known: true, Confidence: "high"},
		Solution:      []extract.Step{{Order: 1, Action: "fix " + title}},
		Concepts:      []extract.Concept{{Name: "caching", Stack: "Go"}},
		RejectedHypotheses: []extract.RejectedHypothesis{
			{Hypothesis: "network issue", WhyRejected: "local reproduction"},
		},
		BlindSpots:    []extract.BlindSpot{{Point: "connection pooling", Kind: "framework_knowledge"}},
		Pitfalls:      []extract.Pitfall{{Trap: "stale cache", Why: "cache not invalidated", Avoid: "clear cache"}},
		Lessons:       []string{"measure before optimizing"},
		OpenQuestions: []string{"why did CI miss it?"},
		Validation:    extract.Validation{EvidenceChecked: 2, EvidenceVerified: 2},
		Provenance: extract.Provenance{
			SessionID:   "session-" + id,
			SegmentID:   id,
			SegmentHash: "hash-" + id,
		},
	}
}

func seedExperienceStore(t *testing.T, docs []*extract.Doc, cwds map[string]string) *cache.Store {
	t.Helper()
	store, err := cache.Open(t.TempDir())
	if err != nil {
		t.Fatalf("cache.Open: %v", err)
	}
	for _, doc := range docs {
		if err := store.SaveDoc(doc); err != nil {
			t.Fatalf("SaveDoc %s: %v", doc.ID, err)
		}
		seg := segment.Segment{
			ID:        doc.ID,
			Hash:      doc.Provenance.SegmentHash,
			Source:    doc.Source,
			SessionID: doc.Provenance.SessionID,
			Project:   doc.Project,
			Title:     doc.Title,
			Start:     doc.OccurredAt,
			End:       doc.EndedAt,
		}
		if cwd := cwds[doc.ID]; cwd != "" {
			seg.Cwd = cwd
		}
		if err := store.SaveSegment(seg); err != nil {
			t.Fatalf("SaveSegment %s: %v", doc.ID, err)
		}
	}
	return store
}

func infosFor(t *testing.T, store *cache.Store) []cache.DocInfo {
	t.Helper()
	infos, warnings := store.ListActiveDocs()
	if len(warnings) > 0 {
		t.Fatalf("ListActiveDocs warnings: %v", warnings)
	}
	return infos
}

func TestBuildGroupsDocsAndSuggestsKind(t *testing.T) {
	first := testDoc("d1", "internship-tracker", "Fix cache", testTime)
	first.LearningValue = "high"
	second := testDoc("d2", "internship-tracker", "Tune query", testTime.Add(-time.Hour))
	second.LearningValue = "low"
	other := testDoc("d3", "other", "Other work", testTime)
	store := seedExperienceStore(t, []*extract.Doc{first, second, other}, map[string]string{
		"d1": `C:\Users\alice\work\internship-tracker`,
		"d2": `C:\Users\alice\work\internship-tracker`,
	})

	exps := Build(infosFor(t, store), store, NewState(), testTime)
	if len(exps) != 2 || exps[0].Project != "internship-tracker" || exps[1].Project != "other" {
		t.Fatalf("experiences = %+v", exps)
	}
	exp := exps[0]
	if exp.Kind != KindInternship || exp.KindSource != KindSourceSuggestion {
		t.Fatalf("kind = %s/%s", exp.Kind, exp.KindSource)
	}
	if exp.Docs != 2 || exp.Sessions != 2 {
		t.Fatalf("docs/sessions = %d/%d", exp.Docs, exp.Sessions)
	}
	if exp.ProjectPath == "" || len(exp.Paths) != 1 {
		t.Fatalf("paths = %+v", exp.Paths)
	}
	if len(exp.Highlights) == 0 || exp.Highlights[0].DocIDs[0] != "d1" {
		t.Fatalf("highlights = %+v", exp.Highlights)
	}
	if len(exp.ReviewChecklist) == 0 {
		t.Fatal("checklist is empty")
	}
	for _, item := range exp.ReviewChecklist {
		if !strings.HasPrefix(item.ID, "ex_") {
			t.Fatalf("unstable checklist id %q", item.ID)
		}
	}
}

func TestManualMetadataOverridesSuggestion(t *testing.T) {
	doc := testDoc("d1", "internship-tracker", "Fix cache", testTime)
	store := seedExperienceStore(t, []*extract.Doc{doc}, nil)
	state := NewState()
	state.Set("internship-tracker", Metadata{
		Kind:         KindProject,
		Organization: "ACME",
		Role:         "Backend Intern",
		Start:        "2025-07",
		End:          "2025-09",
		Headline:     "Internship work",
		Confirmed:    true,
	})

	exp := Build(infosFor(t, store), store, state, testTime)[0]
	if exp.Kind != KindProject || exp.KindSource != KindSourceManual {
		t.Fatalf("kind = %s/%s", exp.Kind, exp.KindSource)
	}
	if exp.Organization != "ACME" || exp.Role != "Backend Intern" {
		t.Fatalf("metadata = %+v", exp)
	}
	if exp.Period != "2025-07 ~ 2025-09" {
		t.Fatalf("period = %q", exp.Period)
	}
}

func TestSuggestKindWordBoundaries(t *testing.T) {
	cases := []struct {
		project string
		paths   []string
		want    string
	}{
		{"internal-tool", nil, ""},
		{"my-intern-project", nil, KindInternship},
		{"placement-tracker", nil, KindInternship},
		{"x", []string{"~/work/internship/backend"}, KindInternship},
		{"x", []string{"~/work/实习/backend"}, KindInternship},
	}
	for _, tc := range cases {
		if got := SuggestKind(tc.project, tc.paths); got != tc.want {
			t.Fatalf("SuggestKind(%q, %v) = %q, want %q", tc.project, tc.paths, got, tc.want)
		}
	}
}

func TestBuildWarnsOnMultipleDirectories(t *testing.T) {
	first := testDoc("d1", "demo", "One", testTime)
	second := testDoc("d2", "demo", "Two", testTime)
	store := seedExperienceStore(t, []*extract.Doc{first, second}, map[string]string{
		"d1": `C:\work\demo-a`,
		"d2": `C:\work\demo-b`,
	})

	exp := Build(infosFor(t, store), store, NewState(), testTime)[0]
	if len(exp.Paths) != 2 {
		t.Fatalf("paths = %+v", exp.Paths)
	}
	if len(exp.Warnings) == 0 || !strings.Contains(strings.Join(exp.Warnings, " "), "源目录") {
		t.Fatalf("warnings = %+v", exp.Warnings)
	}
}

func TestBuildMissingSegmentIsNonFatal(t *testing.T) {
	doc := testDoc("d1", "demo", "One", testTime)
	doc.Provenance.SegmentID = "missing"
	store := seedExperienceStore(t, []*extract.Doc{doc}, nil)

	exp := Build(infosFor(t, store), store, NewState(), testTime)[0]
	if exp.ProjectPath != "" || exp.Docs != 1 {
		t.Fatalf("experience = %+v", exp)
	}
}

func TestChecklistIsStableAndCapped(t *testing.T) {
	var docs []*extract.Doc
	for i := 0; i < 20; i++ {
		doc := testDoc("d"+string(rune('a'+i)), "demo", "Doc "+string(rune('A'+i)), testTime.Add(time.Duration(i)*time.Minute))
		docs = append(docs, doc)
	}
	store := seedExperienceStore(t, docs, nil)
	infos := infosFor(t, store)
	first := Build(infos, store, NewState(), testTime)[0]
	second := Build(infos, store, NewState(), testTime)[0]
	if len(first.ReviewChecklist) > maxChecklistItems {
		t.Fatalf("checklist = %d, cap = %d", len(first.ReviewChecklist), maxChecklistItems)
	}
	a, _ := json.Marshal(first.ReviewChecklist)
	b, _ := json.Marshal(second.ReviewChecklist)
	if string(a) != string(b) {
		t.Fatal("checklist is not deterministic")
	}
}

func TestFindWith(t *testing.T) {
	exps := []Experience{{Project: "Foo"}, {Project: "foo"}, {Project: "bar"}}
	if exp, err := FindWith(exps, "bar", nil); err != nil || exp.Project != "bar" {
		t.Fatalf("exact find = %+v, %v", exp, err)
	}
	if _, err := FindWith(exps, "Foo", nil); err != nil {
		t.Fatalf("exact case-sensitive find: %v", err)
	}
	if _, err := FindWith(exps, "FOO", nil); err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("ambiguous error = %v", err)
	}
	if _, err := FindWith(exps, "missing", nil); err == nil {
		t.Fatal("missing project did not error")
	}

	safeName := func(value string) string { return strings.ReplaceAll(value, " ", "-") }
	safe := []Experience{{Project: "Demo Project"}}
	if exp, err := FindWith(safe, "demo-project", safeName); err != nil || exp.Project != "Demo Project" {
		t.Fatalf("safe-name find = %+v, %v", exp, err)
	}
}

func TestSummarySlicesAreNeverNil(t *testing.T) {
	summary := Summarize(Experience{Project: "demo", Kind: KindProject, KindLabel: "项目经历"})
	data, err := json.Marshal(summary)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if strings.Contains(text, `"sources":null`) || strings.Contains(text, `"stack":null`) {
		t.Fatalf("nil slices leaked: %s", text)
	}
}
