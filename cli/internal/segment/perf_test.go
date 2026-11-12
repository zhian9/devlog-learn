package segment

import (
	"fmt"
	"testing"
	"time"

	"devlog-learn/internal/ingest"
)

func benchmarkSession(events int) *ingest.Session {
	base := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	sess := &ingest.Session{
		ID:      "perf",
		Source:  "claude-code",
		Project: "perf",
		Title:   "Performance session",
		File:    "perf.jsonl",
	}
	for i := range events {
		at := base.Add(time.Duration(i) * time.Second)
		kind, role := ingest.KindUserText, "user"
		text := fmt.Sprintf("question %d", i)
		if i%2 == 1 {
			kind, role = ingest.KindAssistantText, "assistant"
			text = fmt.Sprintf("answer %d", i)
		}
		sess.Events = append(sess.Events, ingest.Event{
			Kind: kind, Role: role, Text: text, Seq: i + 1, Time: at, File: sess.File,
		})
	}
	return sess
}

func BenchmarkSplitLargeSession(b *testing.B) {
	sess := benchmarkSession(10000)
	opt := DefaultOptions()
	b.ResetTimer()
	for b.Loop() {
		segments, _ := Split(sess, opt)
		if len(segments) == 0 {
			b.Fatal("no segments")
		}
	}
}

func BenchmarkSetHashLargeSegment(b *testing.B) {
	segments, _ := Split(benchmarkSession(10000), DefaultOptions())
	seg := segments[0]
	b.ResetTimer()
	for b.Loop() {
		SetHash(&seg)
	}
}
