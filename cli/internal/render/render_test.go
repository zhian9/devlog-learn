package render

import (
	"strings"
	"testing"
	"time"

	"devlog-learn/internal/extract"
)

var renderTime = time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)

func fullDoc() *extract.Doc {
	return &extract.Doc{
		SchemaVersion: 1,
		ID:            "abc123",
		Slug:          "2026-05-01-fix-build",
		Title:         "Fix build",
		Summary:       "The build failed.",
		Project:       "demo-project",
		Source:        "claude-code",
		KnowledgeType: "bug_fix",
		Status:        "resolved",
		Difficulty:    "medium",
		ReviewStatus:  extract.ReviewApproved,
		ReviewNote:    "verified by hand",
		Lifecycle:     extract.LifecycleActive,
		OccurredAt:    renderTime,
		EndedAt:       renderTime.Add(12 * time.Minute),
		DurationMin:   12,
		Stack:         []string{"go", "windows"},
		Tags:          []string{"build", "ci"},
		Problem: extract.Problem{
			Statement:       "Build broke.",
			Symptoms:        []string{"exit code 1"},
			ErrorSignatures: []string{`undefined: foo`},
			Evidence:        []int{1},
		},
		RootCause: extract.RootCause{
			Text:       "Missing import.",
			Known:      true,
			Confidence: "high",
			Evidence:   []int{1},
		},
		Diagnosis: []extract.DiagnosisStep{
			{
				Order:          1,
				Observation:    "Build failed with undefined: foo",
				Hypothesis:     "A missing import",
				Action:         "Inspect the compiler output",
				Command:        "go build ./...",
				CommandKind:    "build",
				ExpectedSignal: "undefined: foo",
				Result:         "Compiler pointed at main.go",
				Conclusion:     "Confirmed missing import",
				Evidence:       []int{1},
			},
		},
		Commands: []extract.Command{
			{
				Purpose:        "Reproduce the build failure",
				Command:        "go build ./...",
				Kind:           "build",
				ExpectedSignal: "exit code 1 with undefined: foo",
				Risk:           "read-only",
				Evidence:       []int{1},
			},
		},
		Solution: []extract.Step{
			{Order: 1, Action: "Add import", Detail: "Import foo.", Command: "go build ./...", Evidence: []int{1}},
		},
		Insights:     []extract.Insight{{Text: "Check imports first.", Confidence: "medium", Evidence: []int{1}}},
		Pitfalls:     []extract.Pitfall{{Trap: "Vendoring", Why: "Stale vendor dir", Avoid: "go mod tidy", Evidence: []int{2}}},
		Verification: []extract.Verification{{Check: "go build", Expected: "exit 0", Observed: "exit 0", Status: "passed", Evidence: []int{2}}},
		Lessons:      []string{"Run the compiler locally."},
		OpenQuestions: []string{
			"Why did CI not catch it?",
		},
		Artifacts: extract.Artifacts{Files: []string{"main.go"}, Commands: []string{"go build ./..."}},
		Evidence: []extract.Evidence{
			{EventSeq: 1, Kind: "error_output", Quote: "undefined: foo", Verified: true},
			{EventSeq: 2, Kind: "command_output", Quote: "exit 0", Verified: true},
		},
		Uncertain:  []extract.Uncertainty{{Field: "root_cause", Reason: "single sample", Known: false}},
		Confidence: 0.8,
		Validation: extract.Validation{EvidenceChecked: 2, EvidenceVerified: 2, Warnings: []string{"summary is short"}},
		Provenance: extract.Provenance{
			SessionID:   "session-1",
			SegmentID:   "abc123",
			SegmentHash: strings.Repeat("a", 64),
			EventRange:  [2]int{1, 2},
			Provider:    "claude-code",
			Model:       "fake-model",
			CLIVersion:  "fake-cli 1.0",
			CacheKey:    "key123",
			ExtractedAt: renderTime,
		},
	}
}

func TestMarkdownFrontMatterAndSections(t *testing.T) {
	got := Markdown(fullDoc())

	if !strings.HasPrefix(got, "---\n") {
		t.Fatalf("missing front matter start:\n%s", got)
	}
	end := strings.Index(got[4:], "\n---\n")
	if end < 0 {
		t.Fatalf("missing front matter end:\n%s", got)
	}
	front := got[:4+end+5]
	for _, want := range []string{
		`schema_version: 1`,
		`id: "abc123"`,
		`title: "Fix build"`,
		`project: "demo-project"`,
		`review_status: "approved"`,
		`review_note: "verified by hand"`,
		`lifecycle: "active"`,
		`occurred_at: "2026-05-01T12:00:00Z"`,
		`confidence: 0.8`,
		`tags:`,
		`  - "build"`,
		`stack:`,
		`evidence_verified: 2`,
	} {
		if !strings.Contains(front, want) {
			t.Errorf("front matter missing %q:\n%s", want, front)
		}
	}

	for _, want := range []string{
		"# Fix build",
		"> The build failed.",
		"## Problem",
		"### Symptoms",
		"### Error signatures",
		"## Root Cause",
		"## Diagnosis Path",
		"Hypothesis: A missing import",
		"## Effective Commands",
		"### C1. Reproduce the build failure",
		"Kind: build",
		"Risk: read-only",
		"## Solution",
		"## Insights",
		"## Pitfalls",
		"## Verification",
		"## Lessons",
		"## Open Questions",
		"## Artifacts",
		"## Evidence",
		"## Uncertainties",
		"## Validation",
		"## Provenance",
		"undefined: foo",
		"go build ./...",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("markdown missing %q:\n%s", want, got)
		}
	}
}

func TestMarkdownQuotesYAMLSpecialCharacters(t *testing.T) {
	doc := fullDoc()
	doc.Title = "He said \"hi\"\nand left: now"
	doc.Project = `D:\work\demo`
	doc.ReviewNote = "line one\nline two"

	got := Markdown(doc)
	front := got[:strings.Index(got[4:], "\n---\n")+9]
	if !strings.Contains(front, `title: "He said \"hi\"\nand left: now"`) {
		t.Errorf("title not safely quoted:\n%s", front)
	}
	if strings.Contains(front, "line one\nline two") {
		t.Errorf("review note leaked a raw newline:\n%s", front)
	}
	if !strings.Contains(front, `review_note: "line one\nline two"`) {
		t.Errorf("review note not escaped:\n%s", front)
	}
	if !strings.Contains(front, `project: "D:\\work\\demo"`) {
		t.Errorf("project not escaped:\n%s", front)
	}
}

func TestDocPathSanitizesComponents(t *testing.T) {
	doc := fullDoc()
	doc.Project = `..\..\Windows\CON`
	doc.Slug = "../../escape/attempt"
	got := DocPath(doc)
	if strings.Contains(got, "..") || strings.Contains(got, `\`) {
		t.Fatalf("unsafe path %q", got)
	}
	if strings.Count(got, "/") != 1 {
		t.Fatalf("path should have exactly one separator: %q", got)
	}
	if strings.Contains(got, ":/") {
		t.Fatalf("path must stay relative: %q", got)
	}

	reserved := fullDoc()
	reserved.Project = "CON"
	if got := DocPath(reserved); !strings.HasPrefix(got, "_CON/") {
		t.Fatalf("expected Windows reserved project to be escaped, got %q", got)
	}
}

func TestPlanDocsDisambiguatesCollisions(t *testing.T) {
	first := fullDoc()
	first.ID = "aaaaaaaa1111"
	second := fullDoc()
	second.ID = "bbbbbbbb2222"
	// Same project, slug and title produce the same nominal path.
	third := fullDoc()
	third.ID = "cccccccc3333"
	third.Title = "Other"
	third.Slug = "2026-05-01-other"

	plans := PlanDocs([]*extract.Doc{first, second, third})
	if len(plans) != 3 {
		t.Fatalf("plans = %d", len(plans))
	}
	seen := map[string]bool{}
	for _, plan := range plans {
		if seen[plan.Path] {
			t.Fatalf("duplicate path %q", plan.Path)
		}
		seen[plan.Path] = true
	}
	if plans[0].Path == plans[1].Path {
		t.Fatalf("colliding docs kept the same path")
	}
	if !strings.HasSuffix(plans[1].Path, "-bbbbbbbb.md") {
		t.Fatalf("second colliding path = %q", plans[1].Path)
	}
}

func TestMarkdownTablesEscapePipesAndNewlines(t *testing.T) {
	doc := fullDoc()
	doc.Evidence = []extract.Evidence{{EventSeq: 7, Kind: "tool_output", Quote: "a|b\nc", Verified: false}}
	doc.Verification = []extract.Verification{{Check: "a|b", Expected: "x", Observed: "y", Status: "failed"}}
	got := Markdown(doc)
	if !strings.Contains(got, `a\|b<br>c`) {
		t.Errorf("evidence table not escaped:\n%s", got)
	}
	if !strings.Contains(got, `| failed | a\|b |`) {
		t.Errorf("verification table not escaped:\n%s", got)
	}
}

func TestMarkdownEmptyDoc(t *testing.T) {
	if got := Markdown(nil); got != "" {
		t.Fatalf("nil doc rendered %q", got)
	}
	got := Markdown(&extract.Doc{ID: "x", Title: "Only a title"})
	if !strings.Contains(got, "# Only a title") {
		t.Fatalf("missing title heading:\n%s", got)
	}
	if strings.Contains(got, "## Problem") {
		t.Fatalf("empty sections should be omitted:\n%s", got)
	}
}

func learnDoc() *extract.Doc {
	doc := fullDoc()
	doc.LearningValue = "high"
	doc.Concepts = []extract.Concept{
		{
			Name:        "kafka-consumer-groups",
			Stack:       "Kafka",
			WhyRelevant: "Shows the lag that explained the stuck order.",
			Syntax:      "kafka-consumer-groups --describe --group <group>",
			Example:     "kafka-consumer-groups --bootstrap-server localhost:9092 --describe --group payment",
			DocsURL:     "https://kafka.apache.org/documentation/#basic_ops_consumer_group",
			Evidence:    []int{1},
		},
		{
			Name:        "Gin 路由组",
			Stack:       "Gin",
			WhyRelevant: "Middleware attached to the wrong group skipped auth.",
			Syntax:      "r.Group(\"/api\", authMiddleware)",
			Example:     "api := r.Group(\"/api\", authMiddleware)\napi.GET(\"/orders\", listOrders)",
			Evidence:    []int{2},
		},
	}
	doc.RejectedHypotheses = []extract.RejectedHypothesis{{
		Hypothesis:  "数据库连接池耗尽",
		WhyRejected: "连接数指标正常，且重新消费后订单恢复。",
		Evidence:    []int{2},
	}}
	doc.BlindSpots = []extract.BlindSpot{
		{Point: "消费者组 offset 的含义", Kind: "framework_knowledge", WhyItMatters: "否则会把 lag 当成服务故障。", Evidence: []int{1}},
		{Point: "先确认指标再改代码", Kind: "general_method", WhyItMatters: "避免盲目改动。", Evidence: []int{2}},
	}
	return doc
}

func TestLearnMarkdownChineseStructure(t *testing.T) {
	got := LearnMarkdown(learnDoc())

	sections := []string{
		"## 症状",
		"## 先自己想",
		"## 定位路径",
		"### 有效命令（C1..Cn）",
		"#### C1. Reproduce the build failure",
		"## 被排除的假设",
		"## 根因",
		"## 修复",
		"## 验证",
		"## 可迁移方法",
		"## 知识点卡片",
		"## 盲区",
		"## 下次检查清单",
		"## 案例内自测题",
		"## 证据来源",
	}
	last := -1
	for _, want := range sections {
		idx := strings.Index(got, want)
		if idx < 0 {
			t.Fatalf("learn markdown missing %q:\n%s", want, got)
		}
		if idx < last {
			t.Fatalf("section %q is out of order:\n%s", want, got)
		}
		last = idx
	}
	for _, want := range []string{
		"**学习价值**：高",
		"kafka-consumer-groups --describe --group <group>",
		"```bash",
		"```go",
		"Gin 路由组",
		"数据库连接池耗尽",
		"消费者组 offset 的含义",
		"### 框架知识",
		"### 通用方法",
		"- [ ] go mod tidy",
		"<details><summary>答案</summary>",
		"_证据：1。_",
		"_证据：2。_",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("learn markdown missing %q:\n%s", want, got)
		}
	}
	// The learning renderer must not emit the agent-facing section names.
	for _, unwanted := range []string{"## Diagnosis Path", "## Effective Commands", "## Problem"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("learn markdown leaked agent section %q:\n%s", unwanted, got)
		}
	}
}

func TestLearnMarkdownOldDocOmitsEmptySections(t *testing.T) {
	got := LearnMarkdown(&extract.Doc{ID: "x", Title: "Only a title"})
	if !strings.Contains(got, "# Only a title") {
		t.Fatalf("missing title:\n%s", got)
	}
	for _, unwanted := range []string{
		"## 症状", "## 先自己想", "## 定位路径", "## 被排除的假设", "## 根因",
		"## 修复", "## 验证", "## 可迁移方法", "## 知识点卡片", "## 盲区",
		"## 下次检查清单", "## 案例内自测题", "## 证据来源",
	} {
		if strings.Contains(got, unwanted) {
			t.Fatalf("empty section %q should be omitted:\n%s", unwanted, got)
		}
	}
	if LearnMarkdown(nil) != "" {
		t.Fatal("nil doc should render empty")
	}
}

func TestLearnPathsDoNotOverwriteAgentPaths(t *testing.T) {
	doc := fullDoc()
	agent := DocPath(doc)
	learn := LearnDocPath(doc)
	if agent == learn {
		t.Fatalf("learning path must differ from agent path: %q", learn)
	}
	if !strings.HasSuffix(learn, ".learn.md") {
		t.Fatalf("learning path = %q", learn)
	}
	plans := LearnPlans([]*extract.Doc{doc})
	if len(plans) != 1 || plans[0].Path != learn {
		t.Fatalf("learn plans = %+v", plans)
	}
}

func TestMarkdownCodeFenceGrowsForBackticks(t *testing.T) {
	doc := fullDoc()
	doc.Solution = []extract.Step{{Order: 1, Action: "Print", Command: "echo ```"}}
	got := Markdown(doc)
	if !strings.Contains(got, "````bash") {
		t.Fatalf("fence should grow to avoid closing early:\n%s", got)
	}
}
