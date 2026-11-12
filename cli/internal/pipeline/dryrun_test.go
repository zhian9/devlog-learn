package pipeline

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"devlog-learn/internal/redact"
	"devlog-learn/internal/segment"
)

var testNow = time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)

func writeJSONL(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestDryRunWithTemporaryClaudeSession(t *testing.T) {
	root := t.TempDir()
	projectDir := filepath.Join(root, "demo-project")
	writeJSONL(t, filepath.Join(projectDir, "one.jsonl"), strings.Join([]string{
		`{"type":"user","timestamp":"2026-04-01T10:00:00Z","sessionId":"s1","cwd":"D:\\work\\demo","message":{"role":"user","content":"Fix the failing test with sk-abcdefghijklmnopqrstuvwx"}}`,
		`{"type":"assistant","timestamp":"2026-04-01T10:00:01Z","sessionId":"s1","cwd":"D:\\work\\demo","message":{"role":"assistant","content":[{"type":"text","text":"Done."}]}}`,
	}, "\n")+"\n")
	writeJSONL(t, filepath.Join(projectDir, "two.jsonl"), strings.Join([]string{
		`{"type":"user","timestamp":"2026-04-01T10:05:00Z","sessionId":"s2","cwd":"D:\\work\\demo","message":{"role":"user","content":"hello"}}`,
	}, "\n")+"\n")

	report, err := DryRun(context.Background(), Options{
		Load: LoadOptions{
			Provider:  "claude-code",
			ClaudeDir: root,
		},
		SinceLabel:    "all",
		Segment:       segment.DefaultOptions(),
		Redact:        redact.DefaultConfig(),
		IncludeEvents: true,
		Now:           func() time.Time { return testNow },
	})
	if err != nil {
		t.Fatalf("DryRun: %v", err)
	}

	if report.Files != 2 || report.Sessions != 2 {
		t.Fatalf("files/sessions = %d/%d", report.Files, report.Sessions)
	}
	if report.SegmentCount != 1 {
		t.Fatalf("segments = %d, want 1: %+v", report.SegmentCount, report.Segments)
	}
	if report.DropCount != 1 {
		t.Fatalf("drops = %d, want 1: %+v", report.DropCount, report.Drops)
	}
	if report.EventCount != 3 {
		t.Fatalf("events = %d, want 3", report.EventCount)
	}
	if report.Bytes <= 0 {
		t.Fatal("expected a positive payload estimate")
	}
	if report.Redactions["brand-token"] < 1 {
		t.Fatalf("redactions = %+v", report.Redactions)
	}

	item := report.Segments[0]
	if len(item.SegmentID) != 16 {
		t.Fatalf("segment id = %q", item.SegmentID)
	}
	if len(item.Hash) != 64 {
		t.Fatalf("segment hash = %q", item.Hash)
	}
	if item.Redactions["brand-token"] < 1 {
		t.Fatalf("item redactions = %+v", item.Redactions)
	}
	if strings.Contains(item.Title, "sk-") {
		t.Fatalf("title leaked a secret: %q", item.Title)
	}
	if len(item.Events) == 0 {
		t.Fatal("expected event previews")
	}
	for _, ev := range item.Events {
		if strings.Contains(ev.Text, "sk-") {
			t.Fatalf("event preview leaked a secret: %q", ev.Text)
		}
	}

	if !containsReason(report.Drops[0].Reasons, segment.DropNoAssistantReply) {
		t.Fatalf("drop reasons = %v", report.Drops[0].Reasons)
	}
	if !report.GeneratedAt.Equal(testNow) {
		t.Fatalf("generated at = %v", report.GeneratedAt)
	}
}

func TestDryRunIsDeterministic(t *testing.T) {
	root := filepath.Join("..", "ingest", "testdata", "claude")
	opt := Options{
		Load:       LoadOptions{Provider: "claude-code", ClaudeDir: root},
		SinceLabel: "all",
		Segment:    segment.DefaultOptions(),
		Redact:     redact.DefaultConfig(),
		Now:        func() time.Time { return testNow },
	}

	a, err := DryRun(context.Background(), opt)
	if err != nil {
		t.Fatalf("first DryRun: %v", err)
	}
	b, err := DryRun(context.Background(), opt)
	if err != nil {
		t.Fatalf("second DryRun: %v", err)
	}
	if len(a.Segments) != len(b.Segments) {
		t.Fatalf("segment counts differ: %d vs %d", len(a.Segments), len(b.Segments))
	}
	for i := range a.Segments {
		if a.Segments[i].SegmentID != b.Segments[i].SegmentID || a.Segments[i].Hash != b.Segments[i].Hash {
			t.Fatalf("segment %d is not deterministic", i)
		}
	}
}

func TestDryRunExcludesCodexSubagentSessions(t *testing.T) {
	root := filepath.Join("..", "ingest", "testdata", "codex")
	report, err := DryRun(context.Background(), Options{
		Load:       LoadOptions{Provider: "codex", CodexDir: root},
		SinceLabel: "all",
		Segment:    segment.DefaultOptions(),
		Redact:     redact.DefaultConfig(),
		Now:        func() time.Time { return testNow },
	})
	if err != nil {
		t.Fatalf("DryRun: %v", err)
	}
	if report.Files != 4 || report.Excluded != 1 {
		t.Fatalf("files/excluded = %d/%d", report.Files, report.Excluded)
	}
	if report.SegmentCount < 2 {
		t.Fatalf("segments = %d, want at least 2", report.SegmentCount)
	}
	if report.DropCount == 0 {
		t.Fatal("expected at least one filtered segment (custom tool session has no user text)")
	}
}

func TestDryRunCanceledContext(t *testing.T) {
	root := filepath.Join("..", "ingest", "testdata", "claude")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := DryRun(ctx, Options{
		Load:       LoadOptions{Provider: "claude-code", ClaudeDir: root},
		SinceLabel: "all",
		Segment:    segment.DefaultOptions(),
		Redact:     redact.DefaultConfig(),
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}

func TestLoadSessionsRejectsUnknownProviderAndMissingExplicitDir(t *testing.T) {
	_, _, err := LoadSessions(LoadOptions{Provider: "cursor"})
	if err == nil || !strings.Contains(err.Error(), "Cursor is not supported") {
		t.Fatalf("provider error = %v", err)
	}

	missing := filepath.Join(t.TempDir(), "missing")
	_, _, err = LoadSessions(LoadOptions{Provider: "claude-code", ClaudeDir: missing})
	if err == nil || !strings.Contains(err.Error(), "directory") {
		t.Fatalf("missing dir error = %v", err)
	}
}

func containsReason(reasons []segment.DropReason, want segment.DropReason) bool {
	for _, reason := range reasons {
		if reason == want {
			return true
		}
	}
	return false
}
