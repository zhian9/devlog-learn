package pipeline

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"devlog-learn/internal/cache"
	"devlog-learn/internal/cursor"
	"devlog-learn/internal/extract"
	"devlog-learn/internal/redact"
	"devlog-learn/internal/segment"
)

// multiProviderOptions runs the real Claude Code and Codex fixture trees with
// one fake client per source. It exercises discovery, parsing, segmentation,
// provider routing, cache keys, and provenance without calling a real model.
func multiProviderOptions(t *testing.T, clients map[string]extract.Client) RunOptions {
	t.Helper()
	return RunOptions{
		Load: LoadOptions{
			Provider:  "all",
			ClaudeDir: filepath.Join("..", "ingest", "testdata", "claude"),
			CodexDir:  filepath.Join("..", "ingest", "testdata", "codex"),
		},
		Segment:     segment.DefaultOptions(),
		Redact:      redact.DefaultConfig(),
		CacheDir:    filepath.Join(t.TempDir(), "cache"),
		CursorPath:  filepath.Join(t.TempDir(), "cursor.json"),
		Concurrency: 2,
		Segments:    0,
		ClientFor: func(_ context.Context, source string) (extract.Client, error) {
			client, ok := clients[source]
			if !ok {
				return nil, fmt.Errorf("no fake client for provider %q", source)
			}
			return client, nil
		},
		Now: func() time.Time { return runNow },
	}
}

func TestRunMultiProviderFixturesEndToEnd(t *testing.T) {
	claude := &fakeRunClient{name: "claude-code", version: "claude-fake 1.0", model: "claude-model"}
	codex := &fakeRunClient{name: "codex", version: "codex-fake 1.0", model: "codex-model"}
	opt := multiProviderOptions(t, map[string]extract.Client{
		"claude-code": claude,
		"codex":       codex,
	})

	first, err := Run(context.Background(), opt)
	if err != nil {
		t.Fatalf("first Run: %v", err)
	}
	if first.Files != 6 || first.Selected != 4 || first.Extracted != 4 || first.FailedSegments != 0 {
		t.Fatalf("first result = %+v", first)
	}

	wantVersion := map[string]string{
		"claude-code": "claude-fake 1.0",
		"codex":       "codex-fake 1.0",
	}
	wantModel := map[string]string{
		"claude-code": "claude-model",
		"codex":       "codex-model",
	}
	perSource := map[string]int{}
	for _, report := range first.Reports {
		if report.Err != nil {
			t.Fatalf("segment %s: %v", report.Segment.ID, report.Err)
		}
		if report.Doc == nil {
			t.Fatalf("segment %s has no doc", report.Segment.ID)
		}
		source := report.Segment.Source
		perSource[source]++
		if report.Doc.Source != source {
			t.Fatalf("doc source = %q, want %q", report.Doc.Source, source)
		}
		prov := report.Doc.Provenance
		if prov.Provider != source || prov.Model != wantModel[source] || prov.CLIVersion != wantVersion[source] {
			t.Fatalf("segment %s provenance = %+v", report.Segment.ID, prov)
		}
		if report.Doc.ID != report.Segment.ID || prov.SegmentID != report.Segment.ID || prov.SegmentHash != report.Segment.Hash {
			t.Fatalf("segment %s identity mismatch: doc=%s prov=%s/%s", report.Segment.ID, report.Doc.ID, prov.SegmentID, prov.SegmentHash)
		}
		if report.CacheKey == "" {
			t.Fatalf("segment %s has no cache key", report.Segment.ID)
		}
	}
	if perSource["claude-code"] != 2 || perSource["codex"] != 2 {
		t.Fatalf("segments per provider = %v", perSource)
	}
	if claude.callCount() != 2 || codex.callCount() != 2 {
		t.Fatalf("model calls = claude:%d codex:%d, want 2/2", claude.callCount(), codex.callCount())
	}

	store, err := cache.Open(opt.CacheDir)
	if err != nil {
		t.Fatalf("cache.Open: %v", err)
	}
	docs, warnings := store.ListActiveDocs()
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v", warnings)
	}
	if len(docs) != 4 {
		t.Fatalf("active docs = %d, want 4", len(docs))
	}
	diskPerSource := map[string]int{}
	for _, info := range docs {
		diskPerSource[info.Doc.Source]++
	}
	if diskPerSource["claude-code"] != 2 || diskPerSource["codex"] != 2 {
		t.Fatalf("docs per provider = %v", diskPerSource)
	}

	// A second incremental run must skip every unchanged file and call no model.
	second, err := Run(context.Background(), opt)
	if err != nil {
		t.Fatalf("second Run: %v", err)
	}
	if second.Skipped != 6 || second.Parsed != 0 || second.Selected != 0 {
		t.Fatalf("incremental result = %+v", second)
	}
	if claude.callCount() != 2 || codex.callCount() != 2 {
		t.Fatalf("incremental run called the model: claude:%d codex:%d", claude.callCount(), codex.callCount())
	}

	// --full reparses everything but still reuses both providers' cache entries.
	opt.Full = true
	third, err := Run(context.Background(), opt)
	if err != nil {
		t.Fatalf("full Run: %v", err)
	}
	if third.Parsed != 6 || third.CacheHits != 4 || third.Extracted != 0 {
		t.Fatalf("full result = %+v", third)
	}
	if claude.callCount() != 2 || codex.callCount() != 2 {
		t.Fatalf("--full must reuse cache: claude:%d codex:%d", claude.callCount(), codex.callCount())
	}
}

func TestRunMultiProviderOneProviderFailure(t *testing.T) {
	claude := &fakeRunClient{name: "claude-code", version: "claude-fake 1.0", model: "claude-model"}
	// No codex client is registered, so provider preparation fails for codex
	// while Claude Code keeps working.
	opt := multiProviderOptions(t, map[string]extract.Client{"claude-code": claude})

	result, err := Run(context.Background(), opt)
	if err == nil {
		t.Fatal("expected codex provider failure")
	}
	if !strings.Contains(err.Error(), "prepare codex client") {
		t.Fatalf("error = %v", err)
	}
	if result.Extracted != 2 || result.FailedSegments != 2 {
		t.Fatalf("result = %+v", result)
	}
	if claude.callCount() != 2 {
		t.Fatalf("claude calls = %d, want 2", claude.callCount())
	}
	for _, report := range result.Reports {
		if report.Segment.Source == "codex" {
			if report.Err == nil {
				t.Fatalf("codex segment %s unexpectedly succeeded", report.Segment.ID)
			}
			continue
		}
		if report.Err != nil || report.Doc == nil {
			t.Fatalf("claude segment %s failed: %v", report.Segment.ID, report.Err)
		}
	}

	store, err := cache.Open(opt.CacheDir)
	if err != nil {
		t.Fatalf("cache.Open: %v", err)
	}
	docs, _ := store.ListActiveDocs()
	if len(docs) != 2 {
		t.Fatalf("active docs = %d, want the 2 claude docs", len(docs))
	}
	for _, info := range docs {
		if info.Doc.Source != "claude-code" {
			t.Fatalf("unexpected doc source %q", info.Doc.Source)
		}
	}
}

func TestRunRepairsInvalidJSONOnce(t *testing.T) {
	h := newRunHarness(t)
	writeSessionFile(t, h.root, "one.jsonl", "Fix build A", runNow.Add(-time.Hour), runNow.Add(-time.Hour+10*time.Second))
	h.client.responses = []string{"not json", runDocJSON}

	result := h.run(t, nil)
	if result.Extracted != 1 || result.FailedSegments != 0 {
		t.Fatalf("result = %+v", result)
	}
	if len(result.Reports) != 1 || result.Reports[0].Repairs != 1 {
		t.Fatalf("repairs = %+v", result.Reports)
	}
	if h.client.callCount() != 2 {
		t.Fatalf("calls = %d, want initial + one repair", h.client.callCount())
	}
	key := cursor.NormalizePath(filepath.Join(h.root, "demo-project", "one.jsonl"))
	if fp := loadCursor(t, h.cursor).Files[key]; fp.Status != cursor.StatusOK {
		t.Fatalf("cursor status = %q", fp.Status)
	}
}
