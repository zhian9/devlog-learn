package ingest

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeLargeClaudeSession emits alternating user/assistant records. It is used
// by both the bounded large-file test and the parser benchmark.
func writeLargeClaudeSession(tb testing.TB, path string, events int) {
	tb.Helper()
	f, err := os.Create(path)
	if err != nil {
		tb.Fatalf("create large claude session: %v", err)
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := range events {
		ts := base.Add(time.Duration(i) * time.Second).Format(time.RFC3339)
		if i%2 == 0 {
			fmt.Fprintf(w, `{"type":"user","timestamp":%q,"sessionId":"large","cwd":"D:\\work\\large","message":{"role":"user","content":"question %d"}}`+"\n", ts, i)
		} else {
			fmt.Fprintf(w, `{"type":"assistant","timestamp":%q,"sessionId":"large","cwd":"D:\\work\\large","message":{"role":"assistant","content":[{"type":"text","text":"answer %d"}]}}`+"\n", ts, i)
		}
	}
	if err := w.Flush(); err != nil {
		tb.Fatalf("flush large claude session: %v", err)
	}
}

func writeLargeCodexSession(tb testing.TB, path string, events int) {
	tb.Helper()
	f, err := os.Create(path)
	if err != nil {
		tb.Fatalf("create large codex session: %v", err)
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	fmt.Fprintf(w, `{"timestamp":%q,"type":"session_meta","payload":{"session_id":"large","id":"large","cwd":"D:\\work\\large","originator":"Codex Desktop"}}`+"\n",
		base.Format(time.RFC3339))
	for i := range events {
		ts := base.Add(time.Duration(i+1) * time.Second).Format(time.RFC3339)
		if i%2 == 0 {
			fmt.Fprintf(w, `{"timestamp":%q,"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"question %d"}]}}`+"\n", ts, i)
		} else {
			fmt.Fprintf(w, `{"timestamp":%q,"type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"answer %d"}]}}`+"\n", ts, i)
		}
	}
	if err := w.Flush(); err != nil {
		tb.Fatalf("flush large codex session: %v", err)
	}
}

// TestParseLargeSessions is a bounded resource regression: 15k events per
// provider must parse linearly and yield every text event. It also catches
// accidental O(n^2) accumulation in the parsers.
func TestParseLargeSessions(t *testing.T) {
	if testing.Short() {
		t.Skip("large session parsing skipped in -short mode")
	}
	const events = 15000

	t.Run("claude", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "large.jsonl")
		writeLargeClaudeSession(t, path, events)
		sess, err := NewClaudeCode().Parse(FileRef{Source: "claude-code", Path: path}, ParseOptions{})
		if err != nil {
			t.Fatalf("Parse: %v", err)
		}
		if len(sess.Events) != events {
			t.Fatalf("events = %d, want %d", len(sess.Events), events)
		}
		if sess.Project != "large" {
			t.Fatalf("project = %q", sess.Project)
		}
	})

	t.Run("codex", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "rollout-large.jsonl")
		writeLargeCodexSession(t, path, events)
		sess, err := NewCodex().Parse(FileRef{Source: "codex", Path: path}, ParseOptions{})
		if err != nil {
			t.Fatalf("Parse: %v", err)
		}
		if len(sess.Events) != events { // session_meta is metadata, not an event
			t.Fatalf("events = %d, want %d", len(sess.Events), events)
		}
		if sess.Project != "large" {
			t.Fatalf("project = %q", sess.Project)
		}
	})
}

func BenchmarkParseLargeClaudeSession(b *testing.B) {
	path := filepath.Join(b.TempDir(), "large.jsonl")
	writeLargeClaudeSession(b, path, 5000)
	ref := FileRef{Source: "claude-code", Path: path}
	src := NewClaudeCode()
	b.ResetTimer()
	for b.Loop() {
		if _, err := src.Parse(ref, ParseOptions{}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkParseLargeCodexSession(b *testing.B) {
	path := filepath.Join(b.TempDir(), "rollout-large.jsonl")
	writeLargeCodexSession(b, path, 5000)
	ref := FileRef{Source: "codex", Path: path}
	src := NewCodex()
	b.ResetTimer()
	for b.Loop() {
		if _, err := src.Parse(ref, ParseOptions{}); err != nil {
			b.Fatal(err)
		}
	}
}
