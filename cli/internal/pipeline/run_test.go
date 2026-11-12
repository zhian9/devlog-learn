package pipeline

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"devlog-learn/internal/cache"
	"devlog-learn/internal/cursor"
	"devlog-learn/internal/extract"
	"devlog-learn/internal/redact"
	"devlog-learn/internal/segment"
)

var runNow = time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)

const runDocJSON = `{
  "title": "Fix build",
  "summary": "A summary.",
  "knowledge_type": "development",
  "status": "unknown",
  "confidence": 0.5
}`

type fakeRunClient struct {
	mu          sync.Mutex
	name        string
	version     string
	model       string
	responses   []string // optional per-call responses; runDocJSON when exhausted
	calls       int
	failNext    int
	inFlight    int
	maxInFlight int
	delay       time.Duration
	block       bool
	usage       extract.Usage
}

func newFakeRunClient() *fakeRunClient {
	return &fakeRunClient{name: "claude-code", version: "fake-cli 1.0", model: "fake-model"}
}

func (f *fakeRunClient) Name() string { return f.name }

func (f *fakeRunClient) Version(context.Context) (string, error) { return f.version, nil }

func (f *fakeRunClient) Capabilities(context.Context) (map[string]bool, error) {
	return map[string]bool{"--output-format": true}, nil
}

func (f *fakeRunClient) Model() string { return f.model }

func (f *fakeRunClient) Complete(ctx context.Context, _ extract.Request) (extract.Response, error) {
	f.mu.Lock()
	index := f.calls
	f.calls++
	fail := f.failNext > 0
	if fail {
		f.failNext--
	}
	text := runDocJSON
	if index < len(f.responses) {
		text = f.responses[index]
	}
	f.inFlight++
	if f.inFlight > f.maxInFlight {
		f.maxInFlight = f.inFlight
	}
	usage := f.usage
	f.mu.Unlock()

	defer func() {
		f.mu.Lock()
		f.inFlight--
		f.mu.Unlock()
	}()

	if f.block {
		<-ctx.Done()
		return extract.Response{}, ctx.Err()
	}
	if f.delay > 0 {
		select {
		case <-time.After(f.delay):
		case <-ctx.Done():
			return extract.Response{}, ctx.Err()
		}
	}
	if fail {
		return extract.Response{Stderr: "fake failure"}, errors.New("fake model failure")
	}
	return extract.Response{Text: text, Usage: usage}, nil
}

func (f *fakeRunClient) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func (f *fakeRunClient) peak() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.maxInFlight
}

type runHarness struct {
	root   string
	cache  string
	cursor string
	client *fakeRunClient
}

func newRunHarness(t *testing.T) *runHarness {
	t.Helper()
	root := t.TempDir()
	return &runHarness{
		root:   root,
		cache:  filepath.Join(t.TempDir(), "cache"),
		cursor: filepath.Join(t.TempDir(), "cursor.json"),
		client: newFakeRunClient(),
	}
}

func (h *runHarness) options(mutate func(*RunOptions)) RunOptions {
	opt := RunOptions{
		Load:        LoadOptions{Provider: "claude-code", ClaudeDir: h.root},
		Segment:     segment.DefaultOptions(),
		Redact:      redact.DefaultConfig(),
		CacheDir:    h.cache,
		CursorPath:  h.cursor,
		Concurrency: 2,
		ClientFor: func(context.Context, string) (extract.Client, error) {
			return h.client, nil
		},
		Now: func() time.Time { return runNow },
	}
	if mutate != nil {
		mutate(&opt)
	}
	return opt
}

func (h *runHarness) run(t *testing.T, mutate func(*RunOptions)) *RunResult {
	t.Helper()
	result, err := Run(context.Background(), h.options(mutate))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	return result
}

func writeSessionFile(t *testing.T, root, name, userText string, userAt, assistantAt time.Time) string {
	t.Helper()
	dir := filepath.Join(root, "demo-project")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	path := filepath.Join(dir, name)
	lines := []string{
		`{"type":"user","timestamp":"` + userAt.UTC().Format(time.RFC3339) + `","sessionId":"` + strings.TrimSuffix(name, ".jsonl") + `","cwd":"D:\\work\\demo","message":{"role":"user","content":"` + userText + `"}}`,
		`{"type":"assistant","timestamp":"` + assistantAt.UTC().Format(time.RFC3339) + `","sessionId":"` + strings.TrimSuffix(name, ".jsonl") + `","cwd":"D:\\work\\demo","message":{"role":"assistant","content":[{"type":"text","text":"Fixed it"}]}}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("write session: %v", err)
	}
	return path
}

func loadCursor(t *testing.T, path string) *cursor.State {
	t.Helper()
	state, err := cursor.Load(path)
	if err != nil {
		t.Fatalf("cursor.Load: %v", err)
	}
	return state
}

func TestRunIncrementalSkipsUnchangedFiles(t *testing.T) {
	h := newRunHarness(t)
	path := writeSessionFile(t, h.root, "one.jsonl", "Fix build A", runNow.Add(-3*time.Hour), runNow.Add(-3*time.Hour+10*time.Second))

	first := h.run(t, nil)
	if first.Parsed != 1 || first.Skipped != 0 || first.Extracted != 1 {
		t.Fatalf("first result = %+v", first)
	}
	if h.client.callCount() != 1 {
		t.Fatalf("calls = %d, want 1", h.client.callCount())
	}

	second := h.run(t, nil)
	if second.Skipped != 1 || second.Parsed != 0 || second.Selected != 0 {
		t.Fatalf("second result = %+v", second)
	}
	if h.client.callCount() != 1 {
		t.Fatalf("second run called the model: %d", h.client.callCount())
	}

	state := loadCursor(t, h.cursor)
	fp := state.Files[cursor.NormalizePath(path)]
	if fp.Status != cursor.StatusOK || len(fp.SegmentIDs) != 1 {
		t.Fatalf("fingerprint = %+v", fp)
	}
}

func TestRunAggregatesTokenUsageOnlyForModelCalls(t *testing.T) {
	h := newRunHarness(t)
	h.client.usage = extract.Usage{
		InputTokens: 100, OutputTokens: 20,
		CacheCreationInputTokens: 5, CacheReadInputTokens: 1000,
		CostUSD: 0.01, Known: true,
	}
	writeSessionFile(t, h.root, "one.jsonl", "Fix build usage", runNow.Add(-3*time.Hour), runNow.Add(-3*time.Hour+10*time.Second))

	first := h.run(t, nil)
	if first.Extracted != 1 || first.UsageUnknownReports != 0 {
		t.Fatalf("first result = %+v", first)
	}
	if first.Usage.TotalTokens() != 1125 || first.Usage.CostUSD != 0.01 {
		t.Fatalf("usage = %+v", first.Usage)
	}

	// A cache hit must not add the same tokens again.
	second := h.run(t, func(opt *RunOptions) { opt.Full = true })
	if second.CacheHits != 1 || second.Extracted != 0 {
		t.Fatalf("second result = %+v", second)
	}
	if second.Usage.TotalTokens() != 0 || second.Usage.Known {
		t.Fatalf("cache hit must not report tokens: %+v", second.Usage)
	}
}

func TestRunDetectsSameSizeRewriteWithRestoredMTime(t *testing.T) {
	h := newRunHarness(t)
	path := writeSessionFile(t, h.root, "one.jsonl", "Fix build A", runNow.Add(-2*time.Hour), runNow.Add(-2*time.Hour+10*time.Second))
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}

	h.run(t, nil)
	writeSessionFile(t, h.root, "one.jsonl", "Fix build B", runNow.Add(-2*time.Hour), runNow.Add(-2*time.Hour+10*time.Second))
	if err := os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
		t.Fatalf("chtimes: %v", err)
	}
	if after, err := os.Stat(path); err != nil || after.Size() != info.Size() {
		t.Fatalf("rewrite changed size: %v/%v", after, err)
	}

	second := h.run(t, nil)
	if second.Skipped != 0 || second.Parsed != 1 {
		t.Fatalf("same-size rewrite was skipped: %+v", second)
	}
	if h.client.callCount() != 2 {
		t.Fatalf("calls = %d, want 2", h.client.callCount())
	}
}

func TestRunTruncatedFileReparses(t *testing.T) {
	h := newRunHarness(t)
	writeSessionFile(t, h.root, "one.jsonl", "Fix build with a long description", runNow.Add(-2*time.Hour), runNow.Add(-2*time.Hour+10*time.Second))
	h.run(t, nil)

	writeSessionFile(t, h.root, "one.jsonl", "Fix build", runNow.Add(-2*time.Hour), runNow.Add(-2*time.Hour+10*time.Second))
	second := h.run(t, nil)
	if second.Skipped != 0 || second.Parsed != 1 || h.client.callCount() != 2 {
		t.Fatalf("result = %+v calls=%d", second, h.client.callCount())
	}
}

func TestRunFullAndForceSemantics(t *testing.T) {
	h := newRunHarness(t)
	writeSessionFile(t, h.root, "one.jsonl", "Fix build A", runNow.Add(-time.Hour), runNow.Add(-time.Hour+10*time.Second))

	h.run(t, nil)
	incremental := h.run(t, nil)
	if incremental.Skipped != 1 || h.client.callCount() != 1 {
		t.Fatalf("incremental = %+v calls=%d", incremental, h.client.callCount())
	}

	full := h.run(t, func(opt *RunOptions) { opt.Full = true })
	if full.Skipped != 0 || full.Parsed != 1 || full.CacheHits != 1 || full.Extracted != 0 {
		t.Fatalf("full = %+v", full)
	}
	if h.client.callCount() != 1 {
		t.Fatalf("--full must reuse cache, calls = %d", h.client.callCount())
	}

	forced := h.run(t, func(opt *RunOptions) { opt.Force = true })
	if forced.CacheHits != 0 || forced.Extracted != 1 {
		t.Fatalf("force = %+v", forced)
	}
	if h.client.callCount() != 2 {
		t.Fatalf("--force must call the model, calls = %d", h.client.callCount())
	}
}

func TestRunVersionMismatchForcesReparseButReusesCache(t *testing.T) {
	h := newRunHarness(t)
	writeSessionFile(t, h.root, "one.jsonl", "Fix build A", runNow.Add(-time.Hour), runNow.Add(-time.Hour+10*time.Second))
	h.run(t, nil)

	state := loadCursor(t, h.cursor)
	state.ParserVersion--
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(h.cursor, data, 0o644); err != nil {
		t.Fatalf("write cursor: %v", err)
	}

	second := h.run(t, nil)
	if second.Skipped != 0 || second.Parsed != 1 || second.CacheHits != 1 {
		t.Fatalf("result = %+v", second)
	}
	if h.client.callCount() != 1 {
		t.Fatalf("cache should still satisfy the segment, calls = %d", h.client.callCount())
	}
}

func TestRunCorruptCursorRecovers(t *testing.T) {
	h := newRunHarness(t)
	writeSessionFile(t, h.root, "one.jsonl", "Fix build A", runNow.Add(-time.Hour), runNow.Add(-time.Hour+10*time.Second))
	h.run(t, nil)

	if err := os.WriteFile(h.cursor, []byte("{corrupt"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	second := h.run(t, nil)
	if second.Parsed != 1 || second.CacheHits != 1 || second.Skipped != 0 {
		t.Fatalf("result = %+v", second)
	}
	if !containsWarning(second.Warnings, "cursor") {
		t.Fatalf("warnings = %v", second.Warnings)
	}
	if h.client.callCount() != 1 {
		t.Fatalf("calls = %d, want 1", h.client.callCount())
	}
}

func TestRunCorruptEntryIsAMiss(t *testing.T) {
	h := newRunHarness(t)
	writeSessionFile(t, h.root, "one.jsonl", "Fix build A", runNow.Add(-time.Hour), runNow.Add(-time.Hour+10*time.Second))
	h.run(t, nil)

	entries, err := os.ReadDir(filepath.Join(h.cache, "entries"))
	if err != nil || len(entries) != 1 {
		t.Fatalf("entries = %v, err = %v", entries, err)
	}
	if err := os.WriteFile(filepath.Join(h.cache, "entries", entries[0].Name()), []byte("{bad"), 0o644); err != nil {
		t.Fatalf("corrupt entry: %v", err)
	}

	second := h.run(t, func(opt *RunOptions) { opt.Full = true })
	if second.CacheHits != 0 || second.Extracted != 1 {
		t.Fatalf("result = %+v", second)
	}
	if h.client.callCount() != 2 {
		t.Fatalf("calls = %d, want 2", h.client.callCount())
	}
}

func TestRunDocChecksumCorruptionIsAMiss(t *testing.T) {
	h := newRunHarness(t)
	writeSessionFile(t, h.root, "one.jsonl", "Fix build A", runNow.Add(-time.Hour), runNow.Add(-time.Hour+10*time.Second))
	h.run(t, nil)

	dir := filepath.Join(h.cache, "entries")
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("entries = %v, err = %v", entries, err)
	}
	path := filepath.Join(dir, entries[0].Name())
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read entry: %v", err)
	}
	var entry map[string]any
	if err := json.Unmarshal(data, &entry); err != nil {
		t.Fatalf("decode entry: %v", err)
	}
	entry["doc_sha256"] = strings.Repeat("0", 64)
	data, err = json.MarshalIndent(entry, "", "  ")
	if err != nil {
		t.Fatalf("encode entry: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write entry: %v", err)
	}

	second := h.run(t, func(opt *RunOptions) { opt.Full = true })
	if second.CacheHits != 0 || second.Extracted != 1 || h.client.callCount() != 2 {
		t.Fatalf("result = %+v calls=%d", second, h.client.callCount())
	}
}

func TestRunFailureDoesNotMarkCursorOK(t *testing.T) {
	h := newRunHarness(t)
	path := writeSessionFile(t, h.root, "one.jsonl", "Fix build A", runNow.Add(-time.Hour), runNow.Add(-time.Hour+10*time.Second))
	h.client.failNext = 1

	firstResult, err := Run(context.Background(), h.options(nil))
	if err == nil {
		t.Fatal("expected an extraction error")
	}
	if firstResult.FailedSegments != 1 || firstResult.FailedFiles != 1 {
		t.Fatalf("result = %+v", firstResult)
	}
	state := loadCursor(t, h.cursor)
	if fp := state.Files[cursor.NormalizePath(path)]; fp.Status != cursor.StatusError {
		t.Fatalf("status = %q, want error", fp.Status)
	}

	second := h.run(t, nil)
	if second.Parsed != 1 || second.Extracted != 1 {
		t.Fatalf("retry result = %+v", second)
	}
	if h.client.callCount() != 2 {
		t.Fatalf("calls = %d, want 2", h.client.callCount())
	}
	state = loadCursor(t, h.cursor)
	if fp := state.Files[cursor.NormalizePath(path)]; fp.Status != cursor.StatusOK {
		t.Fatalf("status = %q, want ok", fp.Status)
	}
}

func TestRunSegmentLimitIsRetried(t *testing.T) {
	h := newRunHarness(t)
	writeSessionFile(t, h.root, "multi.jsonl", "Fix build A", runNow.Add(-3*time.Hour), runNow.Add(-3*time.Hour+10*time.Second))
	// Append a second problem separated by an idle gap.
	lines := []string{
		`{"type":"user","timestamp":"` + runNow.Add(-1*time.Hour).UTC().Format(time.RFC3339) + `","sessionId":"multi","cwd":"D:\\work\\demo","message":{"role":"user","content":"Another error in deploy"}}`,
		`{"type":"assistant","timestamp":"` + runNow.Add(-1*time.Hour+10*time.Second).UTC().Format(time.RFC3339) + `","sessionId":"multi","cwd":"D:\\work\\demo","message":{"role":"assistant","content":[{"type":"text","text":"Fixed it"}]}}`,
	}
	file, err := os.OpenFile(filepath.Join(h.root, "demo-project", "multi.jsonl"), os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := file.WriteString(strings.Join(lines, "\n") + "\n"); err != nil {
		t.Fatalf("append: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	first := h.run(t, func(opt *RunOptions) { opt.Segments = 1 })
	if first.Selected != 1 || first.Segments != 2 || first.FailedFiles != 1 {
		t.Fatalf("first = %+v", first)
	}
	if h.client.callCount() != 1 {
		t.Fatalf("calls = %d, want 1", h.client.callCount())
	}

	second := h.run(t, func(opt *RunOptions) { opt.Segments = 0 })
	if second.Selected != 2 || second.CacheHits != 1 || second.Extracted != 1 || second.FailedFiles != 0 {
		t.Fatalf("second = %+v", second)
	}
	if h.client.callCount() != 2 {
		t.Fatalf("calls = %d, want 2", h.client.callCount())
	}
}

func TestRunStaleSegmentAndRestore(t *testing.T) {
	h := newRunHarness(t)
	writeSessionFile(t, h.root, "one.jsonl", "Fix build A", runNow.Add(-time.Hour), runNow.Add(-time.Hour+10*time.Second))

	first := h.run(t, nil)
	if len(first.Reports) != 1 || first.Reports[0].Doc == nil {
		t.Fatalf("first = %+v", first)
	}
	idA := first.Reports[0].Doc.ID

	writeSessionFile(t, h.root, "one.jsonl", "Fix build B", runNow.Add(-time.Hour), runNow.Add(-time.Hour+10*time.Second))
	second := h.run(t, nil)
	if len(second.Reports) != 1 || second.Reports[0].Doc == nil {
		t.Fatalf("second = %+v", second)
	}
	idB := second.Reports[0].Doc.ID
	if idA == idB {
		t.Fatalf("segment id did not change: %q", idA)
	}
	if !containsString(second.StaleMarked, idA) {
		t.Fatalf("stale marked = %v, want %s", second.StaleMarked, idA)
	}
	store, err := cache.Open(h.cache)
	if err != nil {
		t.Fatalf("cache.Open: %v", err)
	}
	staleDoc, err := store.LoadDoc(idA)
	if err != nil {
		t.Fatalf("LoadDoc: %v", err)
	}
	if staleDoc.Lifecycle != extract.LifecycleStale || staleDoc.StaleReason != cache.StaleSegmentRemoved || staleDoc.StaledAt == nil {
		t.Fatalf("stale doc = %+v", staleDoc)
	}

	// The original content returns: its segment identity reappears and the
	// cached extraction is reused without another model call.
	writeSessionFile(t, h.root, "one.jsonl", "Fix build A", runNow.Add(-time.Hour), runNow.Add(-time.Hour+10*time.Second))
	third := h.run(t, nil)
	if !containsString(third.Restored, idA) {
		t.Fatalf("restored = %v, want %s", third.Restored, idA)
	}
	if third.CacheHits != 1 || third.Extracted != 0 {
		t.Fatalf("third = %+v", third)
	}
	if h.client.callCount() != 2 {
		t.Fatalf("calls = %d, want 2", h.client.callCount())
	}
	restored, err := store.LoadDoc(idA)
	if err != nil {
		t.Fatalf("LoadDoc: %v", err)
	}
	if restored.Lifecycle != extract.LifecycleActive || restored.StaleReason != "" || restored.StaledAt != nil {
		t.Fatalf("restored doc = %+v", restored)
	}
	nowStale, err := store.LoadDoc(idB)
	if err != nil {
		t.Fatalf("LoadDoc B: %v", err)
	}
	if nowStale.Lifecycle != extract.LifecycleStale {
		t.Fatalf("doc B lifecycle = %q", nowStale.Lifecycle)
	}
}

func TestRunMarksSourceMissingWithoutDeletingDoc(t *testing.T) {
	h := newRunHarness(t)
	path := writeSessionFile(t, h.root, "one.jsonl", "Fix build A", runNow.Add(-time.Hour), runNow.Add(-time.Hour+10*time.Second))
	first := h.run(t, nil)
	id := first.Reports[0].Doc.ID

	if err := os.Remove(path); err != nil {
		t.Fatalf("remove: %v", err)
	}
	second := h.run(t, nil)
	if !containsString(second.SourceMissing, id) {
		t.Fatalf("source missing = %v, want %s", second.SourceMissing, id)
	}
	store, err := cache.Open(h.cache)
	if err != nil {
		t.Fatalf("cache.Open: %v", err)
	}
	doc, err := store.LoadDoc(id)
	if err != nil {
		t.Fatalf("doc must remain on disk: %v", err)
	}
	if doc.Lifecycle != extract.LifecycleStale || doc.StaleReason != cache.StaleSourceMissing {
		t.Fatalf("doc = %+v", doc)
	}
	active, _ := store.ListActiveDocs()
	if len(active) != 0 {
		t.Fatalf("active docs = %+v", active)
	}
}

func TestRunConcurrentExtraction(t *testing.T) {
	h := newRunHarness(t)
	for i := range 4 {
		name := "session-" + string(rune('a'+i)) + ".jsonl"
		writeSessionFile(t, h.root, name, "Fix build A", runNow.Add(-time.Hour), runNow.Add(-time.Hour+10*time.Second))
	}
	h.client.delay = 60 * time.Millisecond

	result := h.run(t, func(opt *RunOptions) { opt.Concurrency = 2; opt.Segments = 0 })
	if result.Extracted != 4 || h.client.callCount() != 4 {
		t.Fatalf("result = %+v calls=%d", result, h.client.callCount())
	}
	if peak := h.client.peak(); peak != 2 {
		t.Fatalf("peak concurrency = %d, want 2", peak)
	}
	wantSessions := []string{"session-a", "session-b", "session-c", "session-d"}
	for i, report := range result.Reports {
		if report.Segment.SessionID != wantSessions[i] {
			t.Fatalf("report %d session = %q, want %q (results must keep discovery order)",
				i, report.Segment.SessionID, wantSessions[i])
		}
	}
}

func TestRunCancellationStopsWorkers(t *testing.T) {
	h := newRunHarness(t)
	for i := range 4 {
		name := "session-" + string(rune('a'+i)) + ".jsonl"
		writeSessionFile(t, h.root, name, "Fix build A", runNow.Add(-time.Hour), runNow.Add(-time.Hour+10*time.Second))
	}
	h.client.block = true

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	started := time.Now()
	result, err := Run(ctx, h.options(func(opt *RunOptions) { opt.Segments = 0 }))
	if err == nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want deadline exceeded", err)
	}
	if result == nil || result.FailedSegments == 0 {
		t.Fatalf("result = %+v", result)
	}
	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Fatalf("Run took %s after cancellation", elapsed)
	}
}

func TestRunRejectsBusyCache(t *testing.T) {
	cacheDir := t.TempDir()
	lock, err := cache.TryLock(cacheDir)
	if err != nil {
		t.Fatalf("TryLock: %v", err)
	}
	defer lock.Close()

	_, err = Run(context.Background(), RunOptions{CacheDir: cacheDir})
	if !errors.Is(err, cache.ErrLocked) {
		t.Fatalf("Run error = %v, want ErrLocked", err)
	}
}

func TestRunPreservesReviewWhenContentUnchanged(t *testing.T) {
	h := newRunHarness(t)
	writeSessionFile(t, h.root, "one.jsonl", "Fix build A", runNow.Add(-time.Hour), runNow.Add(-time.Hour+10*time.Second))
	first := h.run(t, nil)
	id := first.Reports[0].Doc.ID

	store, err := cache.Open(h.cache)
	if err != nil {
		t.Fatalf("cache.Open: %v", err)
	}
	doc, err := store.LoadDoc(id)
	if err != nil {
		t.Fatalf("LoadDoc: %v", err)
	}
	doc.ReviewStatus = extract.ReviewApproved
	doc.ReviewNote = "looks right"
	if err := store.SaveDoc(doc); err != nil {
		t.Fatalf("SaveDoc: %v", err)
	}

	// --full reparses but hits the cache; EnsureDoc must not overwrite review.
	full := h.run(t, func(opt *RunOptions) { opt.Full = true })
	if full.CacheHits != 1 || full.Extracted != 0 {
		t.Fatalf("full result = %+v", full)
	}
	got, err := store.LoadDoc(id)
	if err != nil {
		t.Fatalf("LoadDoc after full: %v", err)
	}
	if got.ReviewStatus != extract.ReviewApproved || got.ReviewNote != "looks right" {
		t.Fatalf("cache hit lost review state: %+v", got)
	}

	// --force re-extracts identical content; Normalize resets review, so the
	// pipeline must merge the previous decision back by SegmentHash.
	forced := h.run(t, func(opt *RunOptions) { opt.Force = true })
	if forced.Extracted != 1 || forced.CacheHits != 0 {
		t.Fatalf("forced result = %+v", forced)
	}
	got, err = store.LoadDoc(id)
	if err != nil {
		t.Fatalf("LoadDoc after force: %v", err)
	}
	if got.ReviewStatus != extract.ReviewApproved || got.ReviewNote != "looks right" {
		t.Fatalf("force re-extraction lost review state: %+v", got)
	}
}

func TestRunResetsReviewWhenContentChanges(t *testing.T) {
	h := newRunHarness(t)
	path := writeSessionFile(t, h.root, "one.jsonl", "Fix build A", runNow.Add(-time.Hour), runNow.Add(-time.Hour+10*time.Second))
	first := h.run(t, nil)
	id := first.Reports[0].Doc.ID

	store, err := cache.Open(h.cache)
	if err != nil {
		t.Fatalf("cache.Open: %v", err)
	}
	doc, err := store.LoadDoc(id)
	if err != nil {
		t.Fatalf("LoadDoc: %v", err)
	}
	doc.ReviewStatus = extract.ReviewRejected
	doc.ReviewNote = "not good enough"
	if err := store.SaveDoc(doc); err != nil {
		t.Fatalf("SaveDoc: %v", err)
	}

	appendSessionLine(t, path,
		`{"type":"assistant","timestamp":"`+runNow.Add(-time.Hour+20*time.Second).UTC().Format(time.RFC3339)+`","sessionId":"one","cwd":"D:\\work\\demo","message":{"role":"assistant","content":[{"type":"text","text":"Also updated the docs"}]}}`)

	second := h.run(t, nil)
	if second.Extracted != 1 || second.CacheHits != 0 {
		t.Fatalf("second result = %+v", second)
	}
	if second.Reports[0].Segment.ID != id {
		t.Fatalf("segment id changed (%s -> %s); test no longer exercises content-only change",
			id, second.Reports[0].Segment.ID)
	}
	got, err := store.LoadDoc(id)
	if err != nil {
		t.Fatalf("LoadDoc after change: %v", err)
	}
	if got.ReviewStatus != extract.ReviewPending || got.ReviewNote != "" {
		t.Fatalf("changed content must reset review: %+v", got)
	}
	if got.Provenance.SegmentHash == doc.Provenance.SegmentHash {
		t.Fatalf("segment hash did not change")
	}
}

func appendSessionLine(t *testing.T, path, line string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open session for append: %v", err)
	}
	defer f.Close()
	if _, err := f.WriteString(line + "\n"); err != nil {
		t.Fatalf("append session line: %v", err)
	}
}

func containsString(values []string, want string) bool {
	return slices.Contains(values, want)
}

func containsWarning(warnings []string, fragment string) bool {
	for _, warning := range warnings {
		if strings.Contains(warning, fragment) {
			return true
		}
	}
	return false
}
