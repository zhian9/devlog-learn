package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"devlog-learn/internal/cache"
	"devlog-learn/internal/extract"
	"devlog-learn/internal/render"
)

var outputTime = time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)

func runCommand(args ...string) (string, string, error) {
	var stdout, stderr bytes.Buffer
	cmd := newRootCmd()
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return stdout.String(), stderr.String(), err
}

func commandDoc(id, project, title string, at time.Time, tags ...string) *extract.Doc {
	return &extract.Doc{
		SchemaVersion: 1,
		ID:            id,
		Slug:          extract.MakeSlug(title, at),
		Title:         title,
		Summary:       "Summary for " + title,
		Project:       project,
		Source:        "claude-code",
		KnowledgeType: "bug_fix",
		Status:        "resolved",
		ReviewStatus:  extract.ReviewPending,
		Lifecycle:     extract.LifecycleActive,
		OccurredAt:    at,
		Tags:          tags,
		Stack:         []string{"go"},
		Confidence:    0.6,
		Problem:       extract.Problem{Statement: "Something broke."},
		Evidence:      []extract.Evidence{{EventSeq: 1, Kind: "error_output", Quote: "boom", Verified: true}},
	}
}

func seedCache(t *testing.T, docs ...*extract.Doc) string {
	t.Helper()
	dir := t.TempDir()
	store, err := cache.Open(dir)
	if err != nil {
		t.Fatalf("cache.Open: %v", err)
	}
	for _, doc := range docs {
		if err := store.SaveDoc(doc); err != nil {
			t.Fatalf("SaveDoc %s: %v", doc.ID, err)
		}
	}
	return dir
}

func TestRenderAndIndexCommandsExcludeStale(t *testing.T) {
	active := commandDoc("aaa111", "Demo Project", "Fix build", outputTime, "build", "go")
	other := commandDoc("bbb222", "Demo Project", "Tune cache", outputTime.Add(-time.Hour), "cache", "go")
	stale := commandDoc("ccc333", "Demo Project", "Removed topic", outputTime.Add(-2*time.Hour), "removed")
	stale.Lifecycle = extract.LifecycleStale

	cacheDir := seedCache(t, active, other, stale)
	out := t.TempDir()

	stdout, _, err := runCommand("render", "--cache-dir", cacheDir, "--out", out)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	activePath := filepath.Join(out, filepath.FromSlash(render.DocPath(active)))
	if !strings.Contains(stdout, activePath) {
		t.Fatalf("render output missing %s:\n%s", activePath, stdout)
	}
	data, err := os.ReadFile(activePath)
	if err != nil {
		t.Fatalf("read rendered doc: %v", err)
	}
	md := string(data)
	for _, want := range []string{"---\n", `title: "Fix build"`, "# Fix build", "## Problem", "## Evidence"} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown missing %q:\n%s", want, md)
		}
	}
	stalePath := filepath.Join(out, filepath.FromSlash(render.DocPath(stale)))
	if _, err := os.Stat(stalePath); !os.IsNotExist(err) {
		t.Fatalf("stale markdown should not be rendered by default: %v", err)
	}

	if _, _, err := runCommand("render", "--cache-dir", cacheDir, "--out", out, "--include-stale"); err != nil {
		t.Fatalf("render --include-stale: %v", err)
	}
	if _, err := os.Stat(stalePath); err != nil {
		t.Fatalf("stale markdown missing with --include-stale: %v", err)
	}

	if _, _, err := runCommand("index", "--cache-dir", cacheDir, "--out", out); err != nil {
		t.Fatalf("index: %v", err)
	}
	indexData, err := os.ReadFile(filepath.Join(out, "index.json"))
	if err != nil {
		t.Fatalf("read index.json: %v", err)
	}
	if strings.Contains(string(indexData), stale.ID) || strings.Contains(string(indexData), stale.Title) {
		t.Fatalf("stale doc leaked into index.json:\n%s", indexData)
	}
	if !strings.Contains(string(indexData), active.ID) || !strings.Contains(string(indexData), other.ID) {
		t.Fatalf("active docs missing from index.json:\n%s", indexData)
	}

	var parsed struct {
		Total int `json:"total"`
		Docs  []struct {
			ID   string `json:"id"`
			Path string `json:"path"`
		} `json:"docs"`
	}
	if err := json.Unmarshal(indexData, &parsed); err != nil {
		t.Fatalf("parse index.json: %v", err)
	}
	if parsed.Total != 2 || len(parsed.Docs) != 2 {
		t.Fatalf("index total = %+v", parsed)
	}
	for _, entry := range parsed.Docs {
		if _, err := os.Stat(filepath.Join(out, filepath.FromSlash(entry.Path))); err != nil {
			t.Fatalf("index path %q does not resolve to rendered markdown: %v", entry.Path, err)
		}
	}

	readme, err := os.ReadFile(filepath.Join(out, "README.md"))
	if err != nil {
		t.Fatalf("read README: %v", err)
	}
	if strings.Contains(string(readme), stale.Title) {
		t.Fatalf("stale title leaked into README:\n%s", readme)
	}
	projectReadme := filepath.Join(out, render.SafeProject(active.Project), "README.md")
	if _, err := os.Stat(projectReadme); err != nil {
		t.Fatalf("project README missing: %v", err)
	}

	// Indexing must never delete the stale canonical Doc.
	store, err := cache.Open(cacheDir)
	if err != nil {
		t.Fatalf("cache.Open: %v", err)
	}
	if _, err := store.LoadDoc(stale.ID); err != nil {
		t.Fatalf("stale doc was removed: %v", err)
	}
}

func TestRenderLearningProfileCommand(t *testing.T) {
	doc := commandDoc("learn01", "demo", "Kafka 消费积压", outputTime, "kafka", "go")
	doc.LearningValue = "high"
	doc.Diagnosis = []extract.DiagnosisStep{{
		Order:          1,
		Observation:    "订单一直 pending",
		Hypothesis:     "消费者没有提交 offset",
		Action:         "查看消费者组状态",
		Command:        "kafka-consumer-groups --describe --group payment",
		CommandKind:    "read",
		ExpectedSignal: "LAG 持续增大",
		Result:         "LAG 持续增大",
		Conclusion:     "消费停滞",
		Evidence:       []int{1},
	}}
	doc.Commands = []extract.Command{{
		Purpose:        "确认消费者组 lag",
		Command:        "kafka-consumer-groups --describe --group payment",
		Kind:           "read",
		ExpectedSignal: "LAG 与 CURRENT-OFFSET",
		Evidence:       []int{1},
	}}
	doc.Concepts = []extract.Concept{{
		Name:        "kafka-consumer-groups",
		Stack:       "Kafka",
		WhyRelevant: "用来确认消费者组是否停滞",
		Syntax:      "kafka-consumer-groups --describe --group <group>",
		Example:     "kafka-consumer-groups --bootstrap-server localhost:9092 --describe --group payment",
		Evidence:    []int{1},
	}}
	doc.RejectedHypotheses = []extract.RejectedHypothesis{{
		Hypothesis:  "数据库连接池耗尽",
		WhyRejected: "连接数指标正常",
		Evidence:    []int{1},
	}}
	doc.BlindSpots = []extract.BlindSpot{{
		Point:        "consumer group offset 的含义",
		Kind:         "framework_knowledge",
		WhyItMatters: "否则容易误判为服务故障",
		Evidence:     []int{1},
	}}
	doc.RootCause = extract.RootCause{
		Text: "消费者线程卡死，offset 一直没有提交。", Known: true, Confidence: "medium", Evidence: []int{1},
	}
	doc.Solution = []extract.Step{{
		Order: 1, Action: "重启消费者并补交 offset", Detail: "确认无重复消费后恢复。", Evidence: []int{1},
	}}
	doc.Verification = []extract.Verification{{
		Check: "查看 lag", Expected: "LAG 归零", Observed: "LAG 归零", Status: "passed", Evidence: []int{1},
	}}
	doc.Insights = []extract.Insight{{Text: "先看消费者组指标再改代码。", Confidence: "medium", Evidence: []int{1}}}
	doc.Lessons = []string{"遇到积压先确认消费者组 lag。"}
	doc.Pitfalls = []extract.Pitfall{{Trap: "直接重启服务", Why: "没有确认 offset", Avoid: "先查看 consumer group 状态", Evidence: []int{1}}}

	cacheDir := seedCache(t, doc)
	out := t.TempDir()
	stdout, stderr, err := runCommand("render", "--cache-dir", cacheDir, "--out", out, "--profile", "learning")
	if err != nil {
		t.Fatalf("render learning: %v", err)
	}
	learnPath := filepath.Join(out, filepath.FromSlash(render.LearnDocPath(doc)))
	if !strings.Contains(stdout, learnPath) {
		t.Fatalf("stdout missing learning path %s:\n%s", learnPath, stdout)
	}
	data, err := os.ReadFile(learnPath)
	if err != nil {
		t.Fatalf("read learning markdown: %v", err)
	}
	md := string(data)
	for _, want := range []string{
		"## 症状", "## 先自己想", "## 定位路径", "#### C1.", "## 被排除的假设",
		"## 根因", "## 修复", "## 验证", "## 可迁移方法", "## 知识点卡片",
		"## 盲区", "## 下次检查清单", "## 案例内自测题", "## 证据来源",
		"kafka-consumer-groups --describe --group payment",
		"<details><summary>答案</summary>",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("learning markdown missing %q:\n%s", want, md)
		}
	}
	if !strings.Contains(stderr, "profile=learning") {
		t.Fatalf("stderr = %q", stderr)
	}
	agentPath := filepath.Join(out, filepath.FromSlash(render.DocPath(doc)))
	if _, err := os.Stat(agentPath); !os.IsNotExist(err) {
		t.Fatalf("learning profile should not write the agent path: %v", err)
	}
	if _, _, err := runCommand("render", "--cache-dir", cacheDir, "--out", out, "--profile", "bogus"); err == nil {
		t.Fatal("unknown render profile should fail")
	}
}

func TestReviewCommandUpdatesAndLists(t *testing.T) {
	doc := commandDoc("rev123", "demo", "Review me", outputTime, "go")
	cacheDir := seedCache(t, doc)

	stdout, _, err := runCommand("review", doc.ID, "--cache-dir", cacheDir, "--status", "approved", "--note", "verified locally")
	if err != nil {
		t.Fatalf("review update: %v", err)
	}
	if !strings.Contains(stdout, "review_status=approved") {
		t.Fatalf("stdout = %q", stdout)
	}

	store, err := cache.Open(cacheDir)
	if err != nil {
		t.Fatalf("cache.Open: %v", err)
	}
	got, err := store.LoadDoc(doc.ID)
	if err != nil {
		t.Fatalf("LoadDoc: %v", err)
	}
	if got.ReviewStatus != extract.ReviewApproved || got.ReviewNote != "verified locally" {
		t.Fatalf("doc review = %+v", got)
	}
	// Updating review must not reset lifecycle or content.
	if got.Lifecycle != extract.LifecycleActive || got.Title != doc.Title {
		t.Fatalf("review update changed unrelated fields: %+v", got)
	}

	stdout, _, err = runCommand("review", "--cache-dir", cacheDir, "--list", "--status", "approved")
	if err != nil {
		t.Fatalf("review list: %v", err)
	}
	if !strings.Contains(stdout, doc.ID) {
		t.Fatalf("approved list missing doc: %q", stdout)
	}
	stdout, _, err = runCommand("review", "--cache-dir", cacheDir, "--list", "--status", "pending")
	if err != nil {
		t.Fatalf("review list pending: %v", err)
	}
	if strings.Contains(stdout, doc.ID) {
		t.Fatalf("pending list contains approved doc: %q", stdout)
	}

	stdout, _, err = runCommand("review", doc.ID, "--cache-dir", cacheDir, "--json")
	if err != nil {
		t.Fatalf("review show: %v", err)
	}
	var entry reviewEntry
	if err := json.Unmarshal([]byte(stdout), &entry); err != nil {
		t.Fatalf("parse review JSON %q: %v", stdout, err)
	}
	if entry.ReviewStatus != extract.ReviewApproved || entry.ReviewNote != "verified locally" {
		t.Fatalf("entry = %+v", entry)
	}

	if _, _, err := runCommand("review", doc.ID, "--cache-dir", cacheDir, "--status", "maybe"); err == nil {
		t.Fatalf("invalid status accepted")
	}
	if _, _, err := runCommand("review", "missing", "--cache-dir", cacheDir, "--status", "approved"); err == nil {
		t.Fatalf("unknown doc accepted")
	}
}

func TestRelatedCommand(t *testing.T) {
	target := commandDoc("rel1", "demo", "Target", outputTime, "go", "cache")
	peer := commandDoc("rel2", "demo", "Peer", outputTime, "go", "cache")
	unrelated := commandDoc("rel3", "other", "Unrelated", outputTime, "design")
	unrelated.Stack = []string{"figma"}
	unrelated.KnowledgeType = "learning"
	cacheDir := seedCache(t, target, peer, unrelated)

	stdout, _, err := runCommand("related", target.ID, "--cache-dir", cacheDir)
	if err != nil {
		t.Fatalf("related: %v", err)
	}
	if !strings.Contains(stdout, peer.ID) || strings.Contains(stdout, unrelated.ID) {
		t.Fatalf("related output = %q", stdout)
	}

	stdout, _, err = runCommand("related", target.ID, "--cache-dir", cacheDir, "--json")
	if err != nil {
		t.Fatalf("related json: %v", err)
	}
	var related []struct {
		ID      string   `json:"id"`
		Reasons []string `json:"reasons"`
	}
	if err := json.Unmarshal([]byte(stdout), &related); err != nil {
		t.Fatalf("parse related JSON %q: %v", stdout, err)
	}
	if len(related) != 1 || related[0].ID != peer.ID {
		t.Fatalf("related = %+v", related)
	}
}
