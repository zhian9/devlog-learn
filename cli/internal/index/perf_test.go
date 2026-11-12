package index

import (
	"fmt"
	"testing"
	"time"

	"devlog-learn/internal/cache"
	"devlog-learn/internal/extract"
)

func benchmarkDocs(count int) []*extract.Doc {
	docs := make([]*extract.Doc, count)
	base := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	for i := range docs {
		docs[i] = &extract.Doc{
			SchemaVersion: 1,
			ID:            fmt.Sprintf("doc-%04d", i),
			Slug:          fmt.Sprintf("2026-01-01-doc-%d", i),
			Title:         fmt.Sprintf("Document %d", i),
			Project:       fmt.Sprintf("project-%d", i%8),
			Source:        "claude-code",
			KnowledgeType: "bug_fix",
			Status:        "resolved",
			ReviewStatus:  "pending",
			Lifecycle:     "active",
			OccurredAt:    base.Add(time.Duration(i) * time.Minute),
			Confidence:    0.7,
			Tags:          []string{"go", fmt.Sprintf("tag-%d", i%12)},
			Stack:         []string{"go", "windows"},
		}
	}
	return docs
}

func BenchmarkRelatedDocs(b *testing.B) {
	docs := benchmarkDocs(300)
	target := docs[0]
	b.ResetTimer()
	for b.Loop() {
		if RelatedDocs(target, docs) == nil {
			b.Fatal("no related docs")
		}
	}
}

func BenchmarkWriteIndex(b *testing.B) {
	docs := benchmarkDocs(100)
	infos := make([]cache.DocInfo, len(docs))
	for i, doc := range docs {
		infos[i] = cache.DocInfo{ID: doc.ID, Path: doc.ID + ".json", Doc: doc}
	}
	out := b.TempDir()
	b.ResetTimer()
	for b.Loop() {
		if _, err := Write(out, infos, time.Now()); err != nil {
			b.Fatal(err)
		}
	}
}
