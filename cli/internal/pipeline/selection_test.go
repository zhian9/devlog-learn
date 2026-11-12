package pipeline

import (
	"context"
	"strings"
	"testing"
	"time"

	"devlog-learn/internal/cache"
	"devlog-learn/internal/cursor"
	"devlog-learn/internal/redact"
	"devlog-learn/internal/segment"
)

func testJob(source, session, segmentID string) runJob {
	return runJob{
		source: source,
		ps: PreparedSegment{Segment: segment.Segment{
			Source: source, SessionID: session, ID: segmentID,
		}},
	}
}

func jobIDs(jobs []runJob) []string {
	out := make([]string, 0, len(jobs))
	for _, job := range jobs {
		out = append(out, job.ps.Segment.ID)
	}
	return out
}

func TestSelectJobsWithoutSelectionHonoursLimit(t *testing.T) {
	jobs := []runJob{
		testJob("claude-code", "s1", "a"),
		testJob("claude-code", "s1", "b"),
		testJob("claude-code", "s2", "c"),
	}
	got, err := selectJobs(jobs, Selection{}, 2)
	if err != nil {
		t.Fatalf("selectJobs: %v", err)
	}
	if strings.Join(jobIDs(got), ",") != "a,b" {
		t.Fatalf("ids = %v", jobIDs(got))
	}
	all, err := selectJobs(jobs, Selection{}, 0)
	if err != nil || len(all) != 3 {
		t.Fatalf("all = %v, err = %v", jobIDs(all), err)
	}
}

func TestSelectJobsBySessionSelectorAndBareID(t *testing.T) {
	jobs := []runJob{
		testJob("claude-code", "s1", "a"),
		testJob("claude-code", "s1", "b"),
		testJob("codex", "s2", "c"),
	}
	got, err := selectJobs(jobs, Selection{Sessions: []string{"claude-code:s1"}}, 1)
	if err != nil {
		t.Fatalf("selectJobs: %v", err)
	}
	if strings.Join(jobIDs(got), ",") != "a,b" {
		t.Fatalf("explicit session must ignore the segments cap: %v", jobIDs(got))
	}
	bare, err := selectJobs(jobs, Selection{Sessions: []string{"s2"}}, 0)
	if err != nil {
		t.Fatalf("bare selectJobs: %v", err)
	}
	if strings.Join(jobIDs(bare), ",") != "c" {
		t.Fatalf("bare ids = %v", jobIDs(bare))
	}
}

func TestSelectJobsBySegmentIDAndUnion(t *testing.T) {
	jobs := []runJob{
		testJob("claude-code", "s1", "a"),
		testJob("claude-code", "s1", "b"),
		testJob("codex", "s2", "c"),
	}
	got, err := selectJobs(jobs, Selection{
		Sessions: []string{"claude-code:s1"},
		Segments: []string{"a", "c"},
	}, 0)
	if err != nil {
		t.Fatalf("selectJobs: %v", err)
	}
	if strings.Join(jobIDs(got), ",") != "a,b,c" {
		t.Fatalf("union must keep discovery order and dedupe: %v", jobIDs(got))
	}
}

func TestSelectJobsRejectsUnknownAndAmbiguous(t *testing.T) {
	jobs := []runJob{
		testJob("claude-code", "same", "a"),
		testJob("codex", "same", "b"),
	}
	if _, err := selectJobs(jobs, Selection{Sessions: []string{"same"}}, 0); err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("ambiguous error = %v", err)
	}
	if _, err := selectJobs(jobs, Selection{Sessions: []string{"claude-code:missing"}}, 0); err == nil || !strings.Contains(err.Error(), "matched no segments") {
		t.Fatalf("unknown session error = %v", err)
	}
	if _, err := selectJobs(jobs, Selection{Segments: []string{"missing"}}, 0); err == nil || !strings.Contains(err.Error(), "matched no segments") {
		t.Fatalf("unknown segment error = %v", err)
	}
}

func TestRunExplicitSelectionLeavesUnselectedCursorUntouched(t *testing.T) {
	h := newRunHarness(t)
	pathA := writeSessionFile(t, h.root, "a.jsonl", "Fix build A", runNow.Add(-3*time.Hour), runNow.Add(-3*time.Hour+10*time.Second))
	pathB := writeSessionFile(t, h.root, "b.jsonl", "Fix build B", runNow.Add(-2*time.Hour), runNow.Add(-2*time.Hour+10*time.Second))
	pathC := writeSessionFile(t, h.root, "c.jsonl", "Fix build C", runNow.Add(-time.Hour), runNow.Add(-time.Hour+10*time.Second))

	first := h.run(t, func(opt *RunOptions) {
		opt.Selection = Selection{Sessions: []string{"claude-code:a"}}
	})
	if first.Selected != 1 || first.Extracted != 1 || h.client.callCount() != 1 {
		t.Fatalf("first = %+v calls=%d", first, h.client.callCount())
	}
	state := loadCursor(t, h.cursor)
	if fp := state.Files[cursor.NormalizePath(pathA)]; fp.Status != cursor.StatusOK {
		t.Fatalf("selected file status = %q", fp.Status)
	}
	for _, path := range []string{pathB, pathC} {
		if _, ok := state.Files[cursor.NormalizePath(path)]; ok {
			t.Fatalf("unselected file %s must keep its (absent) cursor entry", path)
		}
	}

	second := h.run(t, nil)
	if second.Extracted != 2 || second.Skipped != 1 {
		t.Fatalf("second = %+v", second)
	}
	if h.client.callCount() != 3 {
		t.Fatalf("calls = %d, want 3", h.client.callCount())
	}
}

func TestRunExplicitSegmentSelectionPartialThenComplete(t *testing.T) {
	h := newRunHarness(t)
	path := writeSessionFile(t, h.root, "multi.jsonl", "Fix build A", runNow.Add(-3*time.Hour), runNow.Add(-3*time.Hour+10*time.Second))
	appendSessionLine(t, path,
		`{"type":"user","timestamp":"`+runNow.Add(-time.Hour).UTC().Format(time.RFC3339)+`","sessionId":"multi","cwd":"D:\\work\\demo","message":{"role":"user","content":"Another error in deploy"}}`)
	appendSessionLine(t, path,
		`{"type":"assistant","timestamp":"`+runNow.Add(-time.Hour+10*time.Second).UTC().Format(time.RFC3339)+`","sessionId":"multi","cwd":"D:\\work\\demo","message":{"role":"assistant","content":[{"type":"text","text":"Fixed it"}]}}`)

	prepared, err := Prepare(context.Background(), PrepareOptions{
		Load:    LoadOptions{Provider: "claude-code", ClaudeDir: h.root},
		Segment: segment.DefaultOptions(),
		Redact:  redact.DefaultConfig(),
	})
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if len(prepared.Segments) != 2 {
		t.Fatalf("prepared segments = %d, want 2", len(prepared.Segments))
	}
	idA, idB := prepared.Segments[0].Segment.ID, prepared.Segments[1].Segment.ID

	first := h.run(t, func(opt *RunOptions) {
		opt.Selection = Selection{Segments: []string{idA}}
	})
	if first.Selected != 1 || first.Extracted != 1 || h.client.callCount() != 1 {
		t.Fatalf("first = %+v calls=%d", first, h.client.callCount())
	}
	fp := loadCursor(t, h.cursor).Files[cursor.NormalizePath(path)]
	if fp.Status != cursor.StatusError || !strings.Contains(fp.LastError, "explicit selection") {
		t.Fatalf("partial selection cursor = %+v", fp)
	}

	second := h.run(t, func(opt *RunOptions) {
		opt.Selection = Selection{Segments: []string{idB}}
	})
	if second.Selected != 1 || second.Extracted != 1 || h.client.callCount() != 2 {
		t.Fatalf("second = %+v calls=%d", second, h.client.callCount())
	}

	complete := h.run(t, func(opt *RunOptions) {
		opt.Selection = Selection{Segments: []string{idA, idB}}
	})
	if complete.Selected != 2 || complete.CacheHits != 2 || complete.Extracted != 0 {
		t.Fatalf("complete = %+v", complete)
	}
	if h.client.callCount() != 2 {
		t.Fatalf("complete selection must reuse cache, calls = %d", h.client.callCount())
	}
	fp = loadCursor(t, h.cursor).Files[cursor.NormalizePath(path)]
	if fp.Status != cursor.StatusOK {
		t.Fatalf("complete selection cursor = %+v", fp)
	}
}

func TestRunUnknownSelectionFailsWithoutModelCalls(t *testing.T) {
	h := newRunHarness(t)
	writeSessionFile(t, h.root, "one.jsonl", "Fix build A", runNow.Add(-time.Hour), runNow.Add(-time.Hour+10*time.Second))

	result, err := Run(context.Background(), h.options(func(opt *RunOptions) {
		opt.Selection = Selection{Sessions: []string{"claude-code:missing"}}
	}))
	if err == nil || !strings.Contains(err.Error(), "matched no segments") {
		t.Fatalf("error = %v", err)
	}
	if result == nil || result.Selected != 0 {
		t.Fatalf("result = %+v", result)
	}
	if h.client.callCount() != 0 {
		t.Fatalf("calls = %d, want 0", h.client.callCount())
	}
	store := cacheStore(t, h)
	docs, _ := store.ListActiveDocs()
	if len(docs) != 0 {
		t.Fatalf("docs = %d, want 0", len(docs))
	}
}

func cacheStore(t *testing.T, h *runHarness) *cache.Store {
	t.Helper()
	store, err := cache.Open(h.cache)
	if err != nil {
		t.Fatalf("cache.Open: %v", err)
	}
	return store
}
