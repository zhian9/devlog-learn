package render

import (
	"fmt"
	"testing"
	"time"

	"devlog-learn/internal/extract"
)

func BenchmarkMarkdownLargeDoc(b *testing.B) {
	doc := &extract.Doc{
		SchemaVersion: 1,
		ID:            "perf-doc",
		Slug:          "2026-01-01-perf",
		Title:         "Performance document",
		Summary:       "A large document used to benchmark rendering.",
		Project:       "perf",
		Source:        "claude-code",
		KnowledgeType: "bug_fix",
		Status:        "resolved",
		ReviewStatus:  "pending",
		Lifecycle:     "active",
		OccurredAt:    time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC),
		EndedAt:       time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC),
		Tags:          []string{"go", "performance", "build"},
		Stack:         []string{"go", "windows"},
		Confidence:    0.7,
	}
	for i := range 200 {
		doc.Insights = append(doc.Insights, extract.Insight{Text: fmt.Sprintf("insight %d", i), Confidence: "medium"})
		doc.Pitfalls = append(doc.Pitfalls, extract.Pitfall{Trap: fmt.Sprintf("pitfall %d", i), Why: "because", Avoid: "do better"})
		doc.Evidence = append(doc.Evidence, extract.Evidence{EventSeq: i + 1, Kind: "tool_output", Quote: fmt.Sprintf("output %d", i), Verified: true})
		doc.Lessons = append(doc.Lessons, fmt.Sprintf("lesson %d", i))
	}
	b.ResetTimer()
	for b.Loop() {
		if Markdown(doc) == "" {
			b.Fatal("empty markdown")
		}
	}
}
