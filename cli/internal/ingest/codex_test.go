package ingest

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	codexBasicPath    = "testdata/codex/2026/02/03/rollout-2026-02-03T04-05-00-11111111-1111-1111-1111-111111111111.jsonl"
	codexCustomPath   = "testdata/codex/2026/02/04/rollout-2026-02-04T04-05-00-22222222-2222-2222-2222-222222222222.jsonl"
	codexFallbackPath = "testdata/codex/2026/02/05/rollout-2026-02-05T04-05-00-33333333-3333-3333-3333-333333333333.jsonl"
	codexSubagentPath = "testdata/codex/2026/02/06/rollout-2026-02-06T04-05-00-44444444-4444-4444-4444-444444444444.jsonl"
)

func TestCodexParseBasic(t *testing.T) {
	path := filepath.FromSlash(codexBasicPath)
	sess := parseFixture(t, NewCodex(), path, ParseOptions{})

	if sess.ID != "codex-1" {
		t.Fatalf("session id = %q", sess.ID)
	}
	if sess.Project != "codex-demo" {
		t.Fatalf("project = %q", sess.Project)
	}
	if sess.Cwd != `D:\work\codex-demo` {
		t.Fatalf("cwd = %q", sess.Cwd)
	}
	if sess.Meta["originator"] != "Codex Desktop" {
		t.Fatalf("originator = %q", sess.Meta["originator"])
	}
	if sess.Meta["cli_version"] != "0.155.0-alpha.9" {
		t.Fatalf("cli_version = %q", sess.Meta["cli_version"])
	}
	if sess.Excluded {
		t.Fatal("user session must not be excluded")
	}

	wantKinds := []Kind{
		KindMeta,
		KindUserText,
		KindAssistantThink,
		KindToolUse,
		KindToolResult,
		KindAssistantText,
	}
	if len(sess.Events) != len(wantKinds) {
		t.Fatalf("events = %d, want %d: %+v", len(sess.Events), len(wantKinds), eventKinds(sess.Events))
	}
	for i, want := range wantKinds {
		if sess.Events[i].Kind != want {
			t.Fatalf("event[%d].Kind = %q, want %q", i, sess.Events[i].Kind, want)
		}
	}

	meta := sess.Events[0]
	if meta.Role != "developer" || !meta.Synthetic {
		t.Fatalf("developer message = %+v", meta)
	}
	if sess.Events[1].Text != "Please fix the failing build" {
		t.Fatalf("user text = %q", sess.Events[1].Text)
	}
	if sess.Events[2].Text != "Need to run the build" {
		t.Fatalf("reasoning = %q", sess.Events[2].Text)
	}

	use := sess.Events[3]
	if use.ToolName != "exec_command" || use.ToolUseID != "call-1" {
		t.Fatalf("tool use = %+v", use)
	}
	if !strings.Contains(string(use.ToolInput), "go build ./...") {
		t.Fatalf("tool input = %s", use.ToolInput)
	}

	result := sess.Events[4]
	if result.ToolName != "exec_command" || result.ToolUseID != "call-1" {
		t.Fatalf("tool result = %+v", result)
	}
	if !strings.Contains(result.ToolResult, "build failed") {
		t.Fatalf("tool result text = %q", result.ToolResult)
	}
	if result.Exec == nil || !result.Exec.Known || result.Exec.ExitCode == nil {
		t.Fatalf("expected known exit code, got %+v", result.Exec)
	}
	if *result.Exec.ExitCode != 1 {
		t.Fatalf("exit code = %d, want 1", *result.Exec.ExitCode)
	}
	if result.IsError {
		t.Fatal("Codex output has no explicit error flag; IsError must stay false")
	}

	if sess.Events[5].Text != "The build fails because of X." {
		t.Fatalf("assistant text = %q", sess.Events[5].Text)
	}

	if sess.Stats.BadJSON != 1 {
		t.Fatalf("bad json = %d, want 1", sess.Stats.BadJSON)
	}
	if sess.Stats.Duplicates != 1 {
		t.Fatalf("duplicates = %d, want 1", sess.Stats.Duplicates)
	}
	if sess.Stats.UnknownTypes["type.mystery-top-level"] != 1 {
		t.Fatalf("unknown types = %+v", sess.Stats.UnknownTypes)
	}
	if sess.Stats.IgnoredTypes["event_msg.token_count"] != 1 {
		t.Fatalf("ignored types = %+v", sess.Stats.IgnoredTypes)
	}
	if len(sess.Warnings) != 2 {
		t.Fatalf("warnings = %d, want 2: %+v", len(sess.Warnings), sess.Warnings)
	}

	wantStart := time.Date(2026, 2, 3, 4, 5, 0, 0, time.UTC)
	wantEnd := time.Date(2026, 2, 3, 4, 5, 7, 0, time.UTC)
	if !sess.Started.Equal(wantStart) || !sess.Ended.Equal(wantEnd) {
		t.Fatalf("session time = %v..%v, want %v..%v", sess.Started, sess.Ended, wantStart, wantEnd)
	}
}

func TestCodexCustomToolCall(t *testing.T) {
	path := filepath.FromSlash(codexCustomPath)
	sess := parseFixture(t, NewCodex(), path, ParseOptions{})

	if len(sess.Events) != 3 {
		t.Fatalf("events = %d, want 3: %+v", len(sess.Events), eventKinds(sess.Events))
	}
	if sess.Events[0].Kind != KindToolUse || sess.Events[0].ToolName != "apply_patch" {
		t.Fatalf("custom tool use = %+v", sess.Events[0])
	}
	if len(sess.Events[0].ToolInput) != 0 {
		t.Fatalf("patch input must be text, got raw JSON %s", sess.Events[0].ToolInput)
	}
	if !strings.Contains(sess.Events[0].ToolInputText, "*** Begin Patch") {
		t.Fatalf("patch text = %q", sess.Events[0].ToolInputText)
	}
	if sess.Events[1].Kind != KindToolResult || sess.Events[1].ToolName != "apply_patch" {
		t.Fatalf("custom tool result = %+v", sess.Events[1])
	}
	if !strings.Contains(sess.Events[1].ToolResult, "Success") {
		t.Fatalf("custom tool result text = %q", sess.Events[1].ToolResult)
	}
	if sess.Events[2].Kind != KindUnknown || len(sess.Events[2].Raw) == 0 {
		t.Fatalf("unknown response item must be preserved: %+v", sess.Events[2])
	}
	if sess.Stats.UnknownTypes["response_item.mystery_item"] != 1 {
		t.Fatalf("unknown types = %+v", sess.Stats.UnknownTypes)
	}
}

func TestCodexEventMsgFallback(t *testing.T) {
	path := filepath.FromSlash(codexFallbackPath)
	sess := parseFixture(t, NewCodex(), path, ParseOptions{})

	wantKinds := []Kind{KindUserText, KindAssistantThink, KindAssistantText, KindMeta}
	if len(sess.Events) != len(wantKinds) {
		t.Fatalf("events = %d, want %d: %+v", len(sess.Events), len(wantKinds), eventKinds(sess.Events))
	}
	for i, want := range wantKinds {
		if sess.Events[i].Kind != want {
			t.Fatalf("event[%d].Kind = %q, want %q", i, sess.Events[i].Kind, want)
		}
	}
	if sess.Events[0].Text != "hello" || sess.Events[1].Text != "thinking" || sess.Events[2].Text != "world" {
		t.Fatalf("fallback texts = %q / %q / %q", sess.Events[0].Text, sess.Events[1].Text, sess.Events[2].Text)
	}
	aborted := sess.Events[3]
	if aborted.Text != "turn_aborted: interrupted" || !aborted.Synthetic || len(aborted.Raw) == 0 {
		t.Fatalf("turn_aborted event = %+v", aborted)
	}
	if sess.Stats.Duplicates != 0 {
		t.Fatalf("duplicates = %d, want 0", sess.Stats.Duplicates)
	}
	if sess.Stats.IgnoredTypes["event_msg.task_started"] != 1 {
		t.Fatalf("ignored types = %+v", sess.Stats.IgnoredTypes)
	}
}

func TestCodexSubagentSession(t *testing.T) {
	path := filepath.FromSlash(codexSubagentPath)

	sess := parseFixture(t, NewCodex(), path, ParseOptions{})
	if !sess.Excluded {
		t.Fatal("guardian/subagent session must be excluded by default")
	}
	if !strings.Contains(sess.ExcludeReason, "subagent") {
		t.Fatalf("exclude reason = %q", sess.ExcludeReason)
	}
	if len(sess.Events) != 0 {
		t.Fatalf("excluded session events = %d, want 0", len(sess.Events))
	}
	if sess.Stats.Events != 0 {
		t.Fatalf("excluded session stats events = %d", sess.Stats.Events)
	}

	included := parseFixture(t, NewCodex(), path, ParseOptions{IncludeSubagent: true})
	if included.Excluded {
		t.Fatal("session must be included when IncludeSubagent is set")
	}
	if len(included.Events) != 1 || included.Events[0].Kind != KindUserText {
		t.Fatalf("included events = %+v", eventKinds(included.Events))
	}
}

func TestCodexDiscovery(t *testing.T) {
	root := filepath.Join("testdata", "codex")
	src := NewCodex()

	refs, err := src.Discover(root, DiscoverOptions{})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(refs) != 4 {
		t.Fatalf("refs = %d, want 4: %+v", len(refs), refs)
	}
	for i := 1; i < len(refs); i++ {
		if refs[i-1].Path >= refs[i].Path {
			t.Fatalf("refs are not sorted: %q >= %q", refs[i-1].Path, refs[i].Path)
		}
	}
	for _, ref := range refs {
		if ref.Source != "codex" {
			t.Fatalf("source = %q", ref.Source)
		}
		if !strings.HasPrefix(filepath.Base(ref.Path), "rollout-") {
			t.Fatalf("unexpected file: %s", ref.Path)
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
