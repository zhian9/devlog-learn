package ingest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func parseFixture(t *testing.T, src Source, path string, opt ParseOptions) *Session {
	t.Helper()
	ref := FileRef{
		Source:    src.Name(),
		Path:      path,
		SessionID: strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)),
		Project:   "fixture-project",
	}
	sess, err := src.Parse(ref, opt)
	if err != nil {
		t.Fatalf("Parse(%s): %v", path, err)
	}
	return sess
}

func TestClaudeParseBasic(t *testing.T) {
	path := filepath.Join("testdata", "claude", "demo-project", "basic.jsonl")
	sess := parseFixture(t, NewClaudeCode(), path, ParseOptions{})

	if sess.ID != "sess-1" {
		t.Fatalf("session id = %q, want sess-1", sess.ID)
	}
	if sess.Project != "demo" {
		t.Fatalf("project = %q, want demo", sess.Project)
	}
	if sess.Cwd != `D:\work\demo` {
		t.Fatalf("cwd = %q", sess.Cwd)
	}
	if sess.Title != "Fix flaky test" {
		t.Fatalf("title = %q", sess.Title)
	}
	if got := sess.Meta["cli_version"]; got != "2.1.268" {
		t.Fatalf("cli_version = %q", got)
	}
	if got := sess.Meta["git_branch"]; got != "main" {
		t.Fatalf("git_branch = %q", got)
	}
	if got := sess.Meta["model"]; got != "claude-test" {
		t.Fatalf("model = %q", got)
	}
	if sess.Excluded {
		t.Fatal("session must not be excluded")
	}
	if sess.File != path {
		t.Fatalf("session file = %q, want %q", sess.File, path)
	}

	wantKinds := []Kind{
		KindUserText,
		KindAssistantThink,
		KindAssistantText,
		KindToolUse,
		KindToolResult,
		KindAssistantText,
		KindUnknown,
	}
	if len(sess.Events) != len(wantKinds) {
		t.Fatalf("events = %d, want %d: %+v", len(sess.Events), len(wantKinds), eventKinds(sess.Events))
	}
	for i, want := range wantKinds {
		if sess.Events[i].Kind != want {
			t.Fatalf("event[%d].Kind = %q, want %q", i, sess.Events[i].Kind, want)
		}
		if sess.Events[i].Seq != i+1 {
			t.Fatalf("event[%d].Seq = %d, want %d", i, sess.Events[i].Seq, i+1)
		}
		if sess.Events[i].File != path {
			t.Fatalf("event[%d].File = %q, want %q", i, sess.Events[i].File, path)
		}
	}

	use := sess.Events[3]
	if use.ToolName != "Bash" {
		t.Fatalf("tool name = %q", use.ToolName)
	}
	if !strings.Contains(string(use.ToolInput), `"command":"go test ./..."`) {
		t.Fatalf("tool input = %s", use.ToolInput)
	}
	if use.ToolUseID != "tool-1" {
		t.Fatalf("tool use id = %q", use.ToolUseID)
	}

	result := sess.Events[4]
	if result.ToolName != "Bash" {
		t.Fatalf("result tool name = %q", result.ToolName)
	}
	if result.ToolUseID != "tool-1" {
		t.Fatalf("result tool use id = %q", result.ToolUseID)
	}
	if result.ToolResult != "FAIL" {
		t.Fatalf("tool result = %q", result.ToolResult)
	}
	if result.IsError {
		t.Fatal("explicit is_error=false must stay false")
	}
	if result.Exec == nil {
		t.Fatal("expected captured execution detail")
	}
	if result.Exec.Known {
		t.Fatal("is_error=false must not be treated as a known successful execution")
	}
	if result.Exec.Stdout != "FAIL" {
		t.Fatalf("exec stdout = %q", result.Exec.Stdout)
	}

	unknown := sess.Events[6]
	if len(unknown.Raw) == 0 {
		t.Fatal("unknown record must preserve raw JSON")
	}

	if sess.Stats.Lines != 10 || sess.Stats.Records != 9 {
		t.Fatalf("lines/records = %d/%d, want 10/9", sess.Stats.Lines, sess.Stats.Records)
	}
	if sess.Stats.BadJSON != 1 {
		t.Fatalf("bad json = %d, want 1", sess.Stats.BadJSON)
	}
	if sess.Stats.SkippedSidechain != 1 {
		t.Fatalf("skipped sidechain = %d, want 1", sess.Stats.SkippedSidechain)
	}
	if sess.Stats.UnknownTypes["type.mystery-event"] != 1 {
		t.Fatalf("unknown types = %+v", sess.Stats.UnknownTypes)
	}
	if sess.Stats.IgnoredTypes["attachment.hook_success"] != 1 {
		t.Fatalf("ignored types = %+v", sess.Stats.IgnoredTypes)
	}
	if sess.Stats.IgnoredTypes["type.file-history-delta"] != 1 {
		t.Fatalf("ignored types = %+v", sess.Stats.IgnoredTypes)
	}
	if len(sess.Warnings) != 2 {
		t.Fatalf("warnings = %d, want 2: %+v", len(sess.Warnings), sess.Warnings)
	}
	for _, warning := range sess.Warnings {
		if warning.File != path {
			t.Fatalf("warning file = %q, want %q", warning.File, path)
		}
	}

	wantStart := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	wantEnd := time.Date(2026, 1, 2, 3, 4, 12, 0, time.UTC)
	if !sess.Started.Equal(wantStart) || !sess.Ended.Equal(wantEnd) {
		t.Fatalf("session time = %v..%v, want %v..%v", sess.Started, sess.Ended, wantStart, wantEnd)
	}
}

func TestClaudeIncludeSidechain(t *testing.T) {
	path := filepath.Join("testdata", "claude", "demo-project", "basic.jsonl")
	sess := parseFixture(t, NewClaudeCode(), path, ParseOptions{IncludeSubagent: true})

	if len(sess.Events) != 8 {
		t.Fatalf("events = %d, want 8", len(sess.Events))
	}
	if sess.Stats.SkippedSidechain != 0 {
		t.Fatalf("skipped sidechain = %d, want 0", sess.Stats.SkippedSidechain)
	}

	found := false
	for _, ev := range sess.Events {
		if ev.Sidechain {
			found = true
			if ev.Text != "subagent note" {
				t.Fatalf("sidechain text = %q", ev.Text)
			}
		}
	}
	if !found {
		t.Fatal("expected an included sidechain event")
	}
}

func TestClaudeContentVariants(t *testing.T) {
	path := filepath.Join("testdata", "claude", "demo-project", "variants.jsonl")
	sess := parseFixture(t, NewClaudeCode(), path, ParseOptions{})

	wantKinds := []Kind{
		KindUserText,
		KindUserText,
		KindAssistantText,
		KindToolResult,
		KindUnknown,
	}
	if len(sess.Events) != len(wantKinds) {
		t.Fatalf("events = %d, want %d: %+v", len(sess.Events), len(wantKinds), eventKinds(sess.Events))
	}
	for i, want := range wantKinds {
		if sess.Events[i].Kind != want {
			t.Fatalf("event[%d].Kind = %q, want %q", i, sess.Events[i].Kind, want)
		}
	}

	if got := sess.Events[0].Text; got != "first" {
		t.Fatalf("first text = %q", got)
	}
	if got := sess.Events[1].Text; got != "second" {
		t.Fatalf("second text = %q", got)
	}
	if got := sess.Events[2].Text; got != "plain assistant" {
		t.Fatalf("assistant string content = %q", got)
	}

	result := sess.Events[3]
	if result.ToolResult != "ok\n[image]" {
		t.Fatalf("tool result = %q", result.ToolResult)
	}
	if !result.IsError {
		t.Fatal("explicit is_error=true must be preserved")
	}
	if result.ToolName != "" {
		t.Fatalf("unpaired tool result name = %q, want empty", result.ToolName)
	}
	if result.Exec != nil {
		t.Fatalf("tool result without execution evidence must not invent Exec: %+v", result.Exec)
	}

	unknown := sess.Events[4]
	if !strings.Contains(string(unknown.Raw), "mystery_block") {
		t.Fatalf("unknown block raw = %s", unknown.Raw)
	}
	if sess.Stats.UnsupportedBlocks != 1 {
		t.Fatalf("unsupported blocks = %d, want 1", sess.Stats.UnsupportedBlocks)
	}
	if sess.Stats.MalformedTimestamps != 1 {
		t.Fatalf("malformed timestamps = %d, want 1", sess.Stats.MalformedTimestamps)
	}
	if sess.Events[4].Time.IsZero() != true {
		t.Fatalf("event with invalid timestamp must keep zero time, got %v", sess.Events[4].Time)
	}
	if len(sess.Warnings) != 2 {
		t.Fatalf("warnings = %d, want 2: %+v", len(sess.Warnings), sess.Warnings)
	}
}

func TestClaudeDiscoveryHandlesWindowsSpecialPaths(t *testing.T) {
	root := t.TempDir()
	project := "测试 项目 & 数据"
	dir := filepath.Join(root, project)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "session 1.jsonl"), []byte("{}\n"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ignore.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write non-jsonl: %v", err)
	}

	refs, err := NewClaudeCode().Discover(root, DiscoverOptions{})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(refs) != 1 {
		t.Fatalf("refs = %d, want 1: %+v", len(refs), refs)
	}
	if refs[0].Project != project {
		t.Fatalf("project = %q, want %q", refs[0].Project, project)
	}
	if refs[0].SessionID != "session 1" {
		t.Fatalf("session id = %q", refs[0].SessionID)
	}
}

func TestClaudeDiscovery(t *testing.T) {
	root := filepath.Join("testdata", "claude")
	src := NewClaudeCode()

	refs, err := src.Discover(root, DiscoverOptions{})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(refs) != 2 {
		t.Fatalf("refs = %d, want 2: %+v", len(refs), refs)
	}
	if filepath.Base(refs[0].Path) != "basic.jsonl" || filepath.Base(refs[1].Path) != "variants.jsonl" {
		t.Fatalf("unexpected order: %+v", refs)
	}
	for _, ref := range refs {
		if ref.Source != "claude-code" {
			t.Fatalf("source = %q", ref.Source)
		}
		if ref.Project != "demo-project" {
			t.Fatalf("project = %q, want demo-project", ref.Project)
		}
		if ref.SessionID == "" {
			t.Fatal("session id must be derived from the file name")
		}
	}

	future, err := src.Discover(root, DiscoverOptions{Since: time.Now().Add(24 * time.Hour)})
	if err != nil {
		t.Fatalf("Discover future: %v", err)
	}
	if len(future) != 0 {
		t.Fatalf("future cutoff refs = %d, want 0", len(future))
	}
}

func eventKinds(events []Event) []Kind {
	out := make([]Kind, len(events))
	for i, ev := range events {
		out[i] = ev.Kind
	}
	return out
}
