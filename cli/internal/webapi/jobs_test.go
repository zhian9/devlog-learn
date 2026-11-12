package webapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"devlog-learn/internal/cache"
	"devlog-learn/internal/extract"
	"devlog-learn/internal/pipeline"
	"devlog-learn/internal/segment"
)

func TestJobManagerPublishesAndFinishes(t *testing.T) {
	var manager jobManager
	release := make(chan struct{})
	snapshot, err := manager.start(func(ctx context.Context, progress func(pipeline.ProgressEvent)) (*pipeline.RunResult, error) {
		progress(pipeline.ProgressEvent{Phase: "scan"})
		progress(pipeline.ProgressEvent{Phase: "selected", Total: 1})
		select {
		case <-release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		progress(pipeline.ProgressEvent{
			Phase: "segment", Total: 1, Done: 1,
			SegmentID: "seg-1", Title: "fixture segment", Source: "claude-code", Project: "demo",
		})
		return &pipeline.RunResult{
			Selected:  1,
			Extracted: 1,
			Reports: []pipeline.RunReport{{
				Segment: segment.Segment{ID: "seg-1", Title: "fixture segment", Project: "demo", Source: "claude-code"},
				Doc:     &extract.Doc{Confidence: 0.7, Validation: extract.Validation{EvidenceChecked: 2, EvidenceVerified: 1}},
			}},
		}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.ID == "" || snapshot.Status != jobRunning {
		t.Fatalf("snapshot = %+v", snapshot)
	}

	close(release)
	waitFor(t, func() bool {
		current, ok := manager.snapshot(snapshot.ID, true)
		return ok && current.Status == jobDone
	})

	finished, ok := manager.snapshot(snapshot.ID, true)
	if !ok {
		t.Fatal("finished job missing")
	}
	if finished.Total != 1 || finished.Extracted != 1 || finished.Failed != 0 {
		t.Fatalf("counts = %+v", finished)
	}
	if finished.Result == nil || len(finished.Result.Reports) != 1 {
		t.Fatalf("result = %+v", finished.Result)
	}
	last := finished.Events[len(finished.Events)-1]
	if last.Phase != jobDone {
		t.Fatalf("last event = %+v", last)
	}
	for index, event := range finished.Events {
		if event.Seq != index {
			t.Fatalf("event %d has seq %d", index, event.Seq)
		}
	}
	if latest := manager.latest(true); latest == nil || latest.ID != snapshot.ID {
		t.Fatalf("latest = %+v", latest)
	}
}

func TestJobManagerBusyAndCancel(t *testing.T) {
	var manager jobManager
	started := make(chan struct{})
	snapshot, err := manager.start(func(ctx context.Context, _ func(pipeline.ProgressEvent)) (*pipeline.RunResult, error) {
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	<-started

	if _, err := manager.start(func(context.Context, func(pipeline.ProgressEvent)) (*pipeline.RunResult, error) {
		return nil, nil
	}); !errors.Is(err, errJobRunning) {
		t.Fatalf("second start error = %v, want errJobRunning", err)
	}

	if !manager.cancel(snapshot.ID) {
		t.Fatal("cancel returned false")
	}
	waitFor(t, func() bool {
		current, ok := manager.snapshot(snapshot.ID, true)
		return ok && current.Status == jobCanceled
	})
	finished, _ := manager.snapshot(snapshot.ID, false)
	if !strings.Contains(finished.Error, "取消") {
		t.Fatalf("cancel error = %q", finished.Error)
	}
}

func TestJobManagerCancelAllAndWait(t *testing.T) {
	var manager jobManager
	started := make(chan struct{})
	if _, err := manager.start(func(ctx context.Context, _ func(pipeline.ProgressEvent)) (*pipeline.RunResult, error) {
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	}); err != nil {
		t.Fatal(err)
	}
	<-started

	manager.cancelAll()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := manager.wait(ctx); err != nil {
		t.Fatalf("wait after cancelAll: %v", err)
	}
}

func TestSessionsEndpoint(t *testing.T) {
	root := t.TempDir()
	server, err := New(Options{
		Addr:       "127.0.0.1:0",
		CacheDir:   filepath.Join(root, "cache"),
		CursorPath: filepath.Join(root, "cursor.json"),
		OutDir:     filepath.Join(root, "out"),
		Token:      "test-token",
		ClaudeDir:  filepath.Join("..", "ingest", "testdata", "claude"),
	})
	if err != nil {
		t.Fatal(err)
	}
	response := get(t, server, "/api/sessions?provider=claude-code&since=all")
	var list pipeline.SessionList
	decode(t, response, &list)
	if len(list.Sessions) == 0 {
		t.Fatal("expected at least one fixture session")
	}
	if list.SegmentCount == 0 {
		t.Fatal("expected prepared segments")
	}
	total := 0
	for _, session := range list.Sessions {
		if session.Selector == "" || session.SegmentCount == 0 {
			t.Fatalf("unexpected session: %+v", session)
		}
		total += len(session.Segments)
	}
	if total != list.SegmentCount {
		t.Fatalf("segments in sessions = %d, list total = %d", total, list.SegmentCount)
	}
}

func TestExtractJobWithFakeClient(t *testing.T) {
	root := t.TempDir()
	server, err := New(Options{
		Addr:       "127.0.0.1:0",
		CacheDir:   filepath.Join(root, "cache"),
		CursorPath: filepath.Join(root, "cursor.json"),
		OutDir:     filepath.Join(root, "out"),
		Token:      "test-token",
		ClaudeDir:  filepath.Join("..", "ingest", "testdata", "claude"),
		ClientFor: func(context.Context, string) (extract.Client, error) {
			return fakeAPIClient{}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	response := get(t, server, "/api/sessions?provider=claude-code&since=all")
	var list pipeline.SessionList
	decode(t, response, &list)
	if len(list.Sessions) == 0 {
		t.Fatal("no fixture sessions")
	}
	selector := list.Sessions[0].Selector

	response = post(t, server, "/api/jobs/extract",
		`{"provider":"claude-code","since":"all","sessions":["`+selector+`"],"concurrency":1,"timeout_seconds":60}`)
	var started struct {
		Job JobSnapshot `json:"job"`
	}
	decode(t, response, &started)
	if started.Job.ID == "" || started.Job.Status != jobRunning {
		t.Fatalf("start = %+v", started.Job)
	}

	waitFor(t, func() bool {
		snapshot, ok := server.jobs.snapshot(started.Job.ID, false)
		return ok && snapshot.Status != jobRunning
	})
	finished, _ := server.jobs.snapshot(started.Job.ID, true)
	if finished.Status != jobDone {
		t.Fatalf("job status = %s error = %s", finished.Status, finished.Error)
	}
	if finished.Extracted == 0 {
		t.Fatalf("no docs extracted: %+v", finished)
	}
	phases := map[string]bool{}
	for _, event := range finished.Events {
		phases[event.Phase] = true
	}
	for _, phase := range []string{"scan", "selected", "segment", "done"} {
		if !phases[phase] {
			t.Fatalf("missing %q progress event: %+v", phase, finished.Events)
		}
	}
	matches, err := filepath.Glob(filepath.Join(root, "cache", "docs", "*.json"))
	if err != nil || len(matches) == 0 {
		t.Fatalf("docs not written: %v %v", matches, err)
	}
}

func TestExtractJobRejectsBusyCache(t *testing.T) {
	root := t.TempDir()
	cacheDir := filepath.Join(root, "cache")
	lock, err := cache.TryLock(cacheDir)
	if err != nil {
		t.Fatalf("TryLock: %v", err)
	}
	defer lock.Close()

	server, err := New(Options{
		Addr:       "127.0.0.1:0",
		CacheDir:   cacheDir,
		CursorPath: filepath.Join(root, "cursor.json"),
		OutDir:     filepath.Join(root, "out"),
		Token:      "test-token",
	})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/jobs/extract", strings.NewReader(
		`{"provider":"all","since":"all","sessions":["claude-code:any"]}`,
	))
	request.Host = "127.0.0.1:8787"
	request.Header.Set("X-Devlog-Token", "test-token")
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusConflict {
		t.Fatalf("status = %d body = %s", response.Code, response.Body.String())
	}
}

type fakeAPIClient struct{}

func (fakeAPIClient) Name() string { return "claude-code" }

func (fakeAPIClient) Version(context.Context) (string, error) { return "fake-cli", nil }

func (fakeAPIClient) Capabilities(context.Context) (map[string]bool, error) {
	return map[string]bool{}, nil
}

func (fakeAPIClient) Complete(context.Context, extract.Request) (extract.Response, error) {
	return extract.Response{Text: `{
  "title": "Fake extraction",
  "summary": "A fake document produced without a model call.",
  "knowledge_type": "bug_fix",
  "problem": {"statement": "fake problem", "symptoms": ["fake symptom"], "error_signatures": ["fake error"]},
  "root_cause": {"text": "", "known": false, "confidence": "unknown"},
  "solution": [],
  "insights": [],
  "pitfalls": [],
  "verification": [],
  "lessons": [],
  "open_questions": [],
  "artifacts": {},
  "evidence": [],
  "confidence": 0.3
}`}, nil
}

func waitFor(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatal("condition not met before timeout")
		}
		time.Sleep(5 * time.Millisecond)
	}
}
