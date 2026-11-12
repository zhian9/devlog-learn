package extract

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"devlog-learn/internal/ingest"
	"devlog-learn/internal/prompts"
	"devlog-learn/internal/segment"
)

func buildSegment() segment.Segment {
	return segment.Segment{
		ID: "seg-build", Source: "claude-code", SessionID: "s1", Project: "demo",
		Title: "Fix build", Start: docTime, End: docTime.Add(time.Minute),
		Events: []ingest.Event{
			{Kind: ingest.KindUserText, Role: "user", Text: "Build fails", Seq: 1, Time: docTime},
			{Kind: ingest.KindAssistantThink, Role: "assistant", Text: "secret reasoning", Seq: 2, Time: docTime},
			{Kind: ingest.KindToolUse, Role: "assistant", ToolName: "Bash", ToolInput: []byte(`{"command":"go build ./..."}`), Seq: 3, Time: docTime},
			{Kind: ingest.KindToolResult, Role: "tool", ToolName: "Bash", ToolResult: "error X", Seq: 4, Time: docTime,
				Exec: &ingest.ExecResult{Known: true, ExitCode: intPtr(1)}},
			{Kind: ingest.KindAssistantText, Role: "assistant", Text: "Fixed by updating the import", Seq: 5, Time: docTime},
		},
	}
}

func TestBuildPromptIncludesSeqAndOutcome(t *testing.T) {
	prompt, err := Build(buildSegment(), DefaultBuildOptions())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	for _, want := range []string{
		`<event seq="1"`, `kind="user_text"`, `outcome="failed"`, `exit_code="1"`,
		`tool="Bash"`, "EVIDENCE RULES", "Simplified Chinese", "<transcript>",
	} {
		if !strings.Contains(prompt.Text, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt.Text)
		}
	}
	if strings.Contains(prompt.Text, "secret reasoning") {
		t.Fatal("thinking must be excluded by default")
	}
	if prompt.IncludedSeqs[2] {
		t.Fatal("excluded thinking seq must not be marked as included")
	}
	if !prompt.IncludedSeqs[1] || !prompt.IncludedSeqs[3] || !prompt.IncludedSeqs[4] || !prompt.IncludedSeqs[5] {
		t.Fatalf("included seqs = %v", prompt.IncludedSeqs)
	}
	if prompt.Hash != prompts.Hash() || prompt.Version != prompts.PromptVersion {
		t.Fatalf("prompt version/hash = %q/%q", prompt.Version, prompt.Hash)
	}
	if prompt.Bytes != len(prompt.Text) {
		t.Fatalf("bytes = %d, text = %d", prompt.Bytes, len(prompt.Text))
	}
}

func TestBuildIncludesThinkingWhenRequested(t *testing.T) {
	opt := DefaultBuildOptions()
	opt.IncludeThinking = true
	prompt, err := Build(buildSegment(), opt)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if !strings.Contains(prompt.Text, "secret reasoning") {
		t.Fatal("thinking should be included")
	}
	if !prompt.IncludedSeqs[2] {
		t.Fatal("thinking seq should be included")
	}
}

func TestBuildTruncatesHugeToolResult(t *testing.T) {
	seg := buildSegment()
	seg.Events[3].ToolResult = strings.Repeat("x", 100_000)
	opt := DefaultBuildOptions()
	opt.MaxInputBytes = 20_000

	prompt, err := Build(seg, opt)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if !prompt.Truncated {
		t.Fatal("expected truncation")
	}
	if len(prompt.Text) > opt.MaxInputBytes {
		t.Fatalf("prompt bytes = %d > %d", len(prompt.Text), opt.MaxInputBytes)
	}
	if !strings.Contains(prompt.Text, "[truncated") {
		t.Fatal("expected truncation marker")
	}
}

func TestBuildOmitsLowPriorityEvents(t *testing.T) {
	events := []ingest.Event{
		{Kind: ingest.KindUserText, Role: "user", Text: "first request", Seq: 1, Time: docTime},
	}
	for seq := 2; seq <= 200; seq++ {
		events = append(events, ingest.Event{
			Kind: ingest.KindAssistantText, Role: "assistant",
			Text: fmt.Sprintf("assistant message %d with some repeated content", seq),
			Seq:  seq, Time: docTime,
		})
	}
	seg := segment.Segment{ID: "seg-big", Source: "codex", SessionID: "s", Project: "p", Events: events}
	opt := DefaultBuildOptions()
	// The learning-section prompt rules raised the fixed overhead, so keep this
	// test budget above it while still forcing low-priority events to be omitted.
	opt.MaxInputBytes = 16_000

	prompt, err := Build(seg, opt)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(prompt.Omitted) == 0 {
		t.Fatal("expected omitted events")
	}
	if !prompt.Truncated {
		t.Fatal("expected truncation")
	}
	if len(prompt.Text) > opt.MaxInputBytes {
		t.Fatalf("prompt bytes = %d > %d", len(prompt.Text), opt.MaxInputBytes)
	}
	if !prompt.IncludedSeqs[1] || !prompt.IncludedSeqs[200] {
		t.Fatalf("last user/assistant must be kept: %v", prompt.IncludedSeqs)
	}
	for _, seq := range prompt.Omitted {
		if prompt.IncludedSeqs[seq] {
			t.Fatalf("omitted seq %d still marked included", seq)
		}
	}
	if !strings.Contains(prompt.Text, "omitted_event_seqs") {
		t.Fatal("prompt must mention omitted seqs")
	}
}

func TestBuildRejectsTinyBudget(t *testing.T) {
	opt := DefaultBuildOptions()
	opt.MaxInputBytes = 1000
	if _, err := Build(buildSegment(), opt); err == nil {
		t.Fatal("expected error for tiny budget")
	}
}

func TestBuildRepair(t *testing.T) {
	prompt, err := BuildRepair(`{"broken":`, "unexpected EOF", "en")
	if err != nil {
		t.Fatalf("BuildRepair: %v", err)
	}
	for _, want := range []string{"unexpected EOF", `{"broken":`, "English", "ONE valid JSON object"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("repair prompt missing %q:\n%s", want, prompt)
		}
	}
}
