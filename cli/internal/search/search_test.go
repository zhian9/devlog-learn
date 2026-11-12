package search

import (
	"path/filepath"
	"testing"
	"time"

	"devlog-learn/internal/cache"
	"devlog-learn/internal/extract"
)

var searchTime = time.Date(2026, 11, 11, 10, 0, 0, 0, time.UTC)

func searchDoc(id, project, title, summary string, tags []string, at time.Time) *extract.Doc {
	return &extract.Doc{
		ID:            id,
		Project:       project,
		Title:         title,
		Summary:       summary,
		Tags:          tags,
		Stack:         []string{"go"},
		KnowledgeType: "bug_fix",
		Status:        "resolved",
		ReviewStatus:  extract.ReviewPending,
		Lifecycle:     extract.LifecycleActive,
		OccurredAt:    at,
		Confidence:    0.6,
		Validation: extract.Validation{
			EvidenceChecked:  2,
			EvidenceVerified: 2,
		},
		Problem: extract.Problem{Statement: summary},
		Solution: []extract.Step{
			{Action: "apply the fix"},
		},
	}
}

func searchInfos(docs ...*extract.Doc) []cache.DocInfo {
	infos := make([]cache.DocInfo, 0, len(docs))
	for _, doc := range docs {
		infos = append(infos, cache.DocInfo{
			ID:   doc.ID,
			Path: filepath.Join("cache", "docs", doc.ID+".json"),
			Doc:  doc,
		})
	}
	return infos
}

func TestSearchRanksTitleAndTagMatches(t *testing.T) {
	cacheDoc := searchDoc("aaa", "demo", "Fix cache eviction", "cache entries were evicted too early", []string{"cache", "go"}, searchTime)
	webDoc := searchDoc("bbb", "demo", "React tutorial", "learn hooks", []string{"web", "react"}, searchTime.Add(-time.Hour))
	docs := searchInfos(cacheDoc, webDoc)

	results := Search(docs, Options{Query: "cache eviction", OutDir: "out"})
	if len(results) != 1 {
		t.Fatalf("results = %+v", results)
	}
	if results[0].ID != "aaa" {
		t.Fatalf("top result = %+v", results[0])
	}
	if results[0].MarkdownPath != "demo/"+extract.MakeSlug(cacheDoc.Title, cacheDoc.OccurredAt)+".md" {
		t.Fatalf("markdown path = %q", results[0].MarkdownPath)
	}
	if results[0].MarkdownFile == "" || results[0].DocPath == "" {
		t.Fatalf("expected absolute paths: %+v", results[0])
	}
	if len(results[0].Matched) == 0 {
		t.Fatalf("expected matched reasons: %+v", results[0])
	}
}

func TestSearchFindsDiagnosisAndCommands(t *testing.T) {
	doc := searchDoc("diag1", "demo", "Payment stuck", "Order stayed pending.", nil, searchTime)
	doc.Diagnosis = []extract.DiagnosisStep{
		{Observation: "order status mismatch", Command: "docker exec mysql mysql -e 'SELECT * FROM orders'", Result: "status is still 1"},
	}
	doc.Commands = []extract.Command{
		{Purpose: "inspect consumer offset", Command: "docker exec kafka kafka-consumer-groups --describe --group payment", ExpectedSignal: "lag and current offset"},
	}

	results := Search(searchInfos(doc), Options{Query: "kafka-consumer-groups offset", Limit: 5})
	if len(results) != 1 || results[0].ID != doc.ID {
		t.Fatalf("command search results = %+v", results)
	}
	for _, want := range []string{"commands"} {
		found := false
		for _, matched := range results[0].Matched {
			if matched == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("matched fields = %v, want %q", results[0].Matched, want)
		}
	}
}

func TestSearchFindsConceptsAndRejectedHypotheses(t *testing.T) {
	doc := searchDoc("learn1", "demo", "Kafka 消费积压", "订单一直 pending。", nil, searchTime)
	doc.Concepts = []extract.Concept{
		{
			Name:        "kafka-consumer-groups",
			Stack:       "Kafka",
			WhyRelevant: "查看消费者组 lag",
			Syntax:      "kafka-consumer-groups --describe --group payment",
		},
		{
			Name:        "Gin 路由组",
			Stack:       "Gin",
			WhyRelevant: "中间件挂错路由组",
			Syntax:      "r.Group(\"/api\", authMiddleware)",
		},
		{
			Name:    "go env",
			Stack:   "Go",
			Example: "go env GOROOT",
		},
	}
	doc.RejectedHypotheses = []extract.RejectedHypothesis{{
		Hypothesis:  "数据库连接池耗尽",
		WhyRejected: "连接数指标正常",
	}}

	tests := []struct {
		query string
		field string
	}{
		{"kafka-consumer-groups", "concepts"},
		{"Gin 路由组", "concepts"},
		{"go env GOROOT", "concepts"},
		{"数据库连接池耗尽", "rejected_hypotheses"},
	}
	for _, tt := range tests {
		results := Search(searchInfos(doc), Options{Query: tt.query, Limit: 5})
		if len(results) != 1 || results[0].ID != doc.ID {
			t.Fatalf("query %q results = %+v", tt.query, results)
		}
		found := false
		for _, matched := range results[0].Matched {
			if matched == tt.field {
				found = true
			}
		}
		if !found {
			t.Fatalf("query %q matched fields = %v, want %q", tt.query, results[0].Matched, tt.field)
		}
	}
}

func TestSearchSupportsCJKQueries(t *testing.T) {
	doc := searchDoc("cjk", "demo", "为会话增加精确选择", "支持按会话和 Segment 选择", []string{"cli"}, searchTime)
	results := Search(searchInfos(doc), Options{Query: "会话选择"})
	if len(results) != 1 || results[0].ID != "cjk" {
		t.Fatalf("results = %+v", results)
	}
}

func TestSearchFiltersProjectTagsReviewAndStale(t *testing.T) {
	approved := searchDoc("aaa", "Demo Project", "Fix cache", "cache bug", []string{"cache", "go"}, searchTime)
	approved.ReviewStatus = extract.ReviewApproved
	otherProject := searchDoc("bbb", "other", "Fix cache", "cache bug", []string{"cache", "go"}, searchTime)
	pending := searchDoc("ccc", "Demo Project", "Fix cache", "cache bug", []string{"cache"}, searchTime)
	stale := searchDoc("ddd", "Demo Project", "Fix cache", "cache bug", []string{"cache", "go"}, searchTime)
	stale.Lifecycle = extract.LifecycleStale
	docs := searchInfos(approved, otherProject, pending, stale)

	results := Search(docs, Options{Query: "cache", Project: "demo-project", Tags: []string{"go"}})
	if len(results) != 1 || results[0].ID != "aaa" {
		t.Fatalf("project/tag/stale filter = %+v", results)
	}
	results = Search(docs, Options{Query: "cache", Review: "approved"})
	if len(results) != 1 || results[0].ID != "aaa" {
		t.Fatalf("review filter = %+v", results)
	}
	results = Search(docs, Options{Query: "cache", Project: "Demo-Project", Tags: []string{"cache", "go"}})
	if len(results) != 1 || results[0].ID != "aaa" {
		t.Fatalf("all-tags filter = %+v", results)
	}
}

func TestSearchEmptyQueryReturnsRecentFirst(t *testing.T) {
	oldDoc := searchDoc("old", "demo", "Old", "old", nil, searchTime.Add(-2*time.Hour))
	newDoc := searchDoc("new", "demo", "New", "new", nil, searchTime)
	results := Search(searchInfos(oldDoc, newDoc), Options{Limit: 1})
	if len(results) != 1 || results[0].ID != "new" {
		t.Fatalf("recent results = %+v", results)
	}
}

func TestSearchDisambiguatesDuplicateMarkdownPaths(t *testing.T) {
	first := searchDoc("aaa", "demo", "Same title", "first", nil, searchTime)
	second := searchDoc("bbb", "demo", "Same title", "second", nil, searchTime)
	results := Search(searchInfos(first, second), Options{})
	if len(results) != 2 {
		t.Fatalf("results = %+v", results)
	}
	if results[0].MarkdownPath == results[1].MarkdownPath {
		t.Fatalf("paths collide: %+v", results)
	}
}

func TestListProjectsSummarizesActiveDocs(t *testing.T) {
	approved := searchDoc("aaa", "demo", "A", "a", nil, searchTime)
	approved.ReviewStatus = extract.ReviewApproved
	pending := searchDoc("bbb", "demo", "B", "b", nil, searchTime)
	other := searchDoc("ccc", "other", "C", "c", nil, searchTime)
	stale := searchDoc("ddd", "demo", "D", "d", nil, searchTime)
	stale.Lifecycle = extract.LifecycleStale

	projects := ListProjects(searchInfos(approved, pending, other, stale))
	if len(projects) != 2 {
		t.Fatalf("projects = %+v", projects)
	}
	if projects[0].Name != "demo" || projects[0].Docs != 2 || projects[0].Approved != 1 || projects[0].Pending != 1 {
		t.Fatalf("demo project = %+v", projects[0])
	}
	if projects[1].Name != "other" || projects[1].Docs != 1 {
		t.Fatalf("other project = %+v", projects[1])
	}
}

func TestSearchIncludeStaleAndAll(t *testing.T) {
	active := searchDoc("active", "demo", "Active doc", "visible", []string{"go"}, searchTime)
	stale := searchDoc("stale", "demo", "Stale doc", "hidden", []string{"go"}, searchTime.Add(-time.Hour))
	stale.Lifecycle = extract.LifecycleStale

	docs := searchInfos(active, stale)
	if results := Search(docs, Options{}); len(results) != 1 {
		t.Fatalf("default results = %d, want 1 active doc", len(results))
	}

	results := Search(docs, Options{IncludeStale: true, All: true})
	if len(results) != 2 {
		t.Fatalf("all results = %d, want 2", len(results))
	}
	seenStale := false
	for _, result := range results {
		if result.ID == "stale" {
			seenStale = true
			if result.Lifecycle != extract.LifecycleStale {
				t.Fatalf("stale lifecycle = %q", result.Lifecycle)
			}
		}
	}
	if !seenStale {
		t.Fatal("stale doc missing from IncludeStale results")
	}

	if results := Search(docs, Options{IncludeStale: true, All: true, Limit: 1}); len(results) != 2 {
		t.Fatalf("All should ignore Limit, got %d", len(results))
	}
}
