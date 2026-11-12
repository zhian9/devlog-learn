package extract

import (
	"strings"
	"testing"
)

func TestNewCLIClientRejectsUnknownProvider(t *testing.T) {
	if _, err := NewCLIClient(CLIOptions{Provider: "cursor"}); err == nil {
		t.Fatal("expected an error for provider cursor")
	}
}

func TestDetectCapabilities(t *testing.T) {
	help := "  -p, --print\n  --output-format <format>\n  --no-session-persistence\n  --model <model>\n"
	caps := detectCapabilities(help)
	if !caps["--output-format"] || !caps["--no-session-persistence"] || !caps["--model"] {
		t.Fatalf("caps = %+v", caps)
	}
	if caps["--ephemeral"] {
		t.Fatalf("unexpected codex flag: %+v", caps)
	}
}

func TestParseClaudeOutput(t *testing.T) {
	t.Run("envelope", func(t *testing.T) {
		text, model, _, err := parseClaudeOutput(`{"type":"result","result":"{\"title\":\"x\"}","modelUsage":{"b-model":{},"a-model":{}}}`)
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		if text != `{"title":"x"}` {
			t.Fatalf("text = %q", text)
		}
		if model != "a-model" {
			t.Fatalf("model = %q", model)
		}
	})

	t.Run("raw json", func(t *testing.T) {
		text, _, _, err := parseClaudeOutput(`{"title":"x"}`)
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		if text != `{"title":"x"}` {
			t.Fatalf("text = %q", text)
		}
	})

	t.Run("plain text", func(t *testing.T) {
		text, _, _, err := parseClaudeOutput("not json")
		if err != nil || text != "not json" {
			t.Fatalf("text = %q, err = %v", text, err)
		}
	})

	t.Run("error envelope", func(t *testing.T) {
		if _, _, _, err := parseClaudeOutput(`{"type":"result","subtype":"error_during_execution","is_error":true}`); err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestParseClaudeOutputUsage(t *testing.T) {
	_, _, usage, err := parseClaudeOutput(`{
		"type":"result",
		"result":"ok",
		"usage":{
			"input_tokens":100,
			"output_tokens":20,
			"cache_creation_input_tokens":5,
			"cache_read_input_tokens":1000
		},
		"total_cost_usd":0.0123
	}`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !usage.Known {
		t.Fatal("expected known usage")
	}
	if usage.InputTokens != 100 || usage.OutputTokens != 20 ||
		usage.CacheCreationInputTokens != 5 || usage.CacheReadInputTokens != 1000 {
		t.Fatalf("usage = %+v", usage)
	}
	if usage.TotalTokens() != 1125 {
		t.Fatalf("total = %d", usage.TotalTokens())
	}
	if usage.CostUSD != 0.0123 {
		t.Fatalf("cost = %v", usage.CostUSD)
	}
}

func TestParseClaudeOutputUsageFromModelUsage(t *testing.T) {
	_, _, usage, err := parseClaudeOutput(`{
		"type":"result",
		"result":"ok",
		"modelUsage":{
			"model-a":{"inputTokens":10,"outputTokens":2,"cacheReadInputTokens":30,"costUSD":0.001},
			"model-b":{"inputTokens":5,"outputTokens":1,"cacheCreationInputTokens":7,"costUSD":0.002}
		}
	}`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if usage.InputTokens != 15 || usage.OutputTokens != 3 || usage.CacheReadInputTokens != 30 || usage.CacheCreationInputTokens != 7 {
		t.Fatalf("usage = %+v", usage)
	}
	if usage.TotalTokens() != 55 {
		t.Fatalf("total = %d", usage.TotalTokens())
	}
	if usage.CostUSD != 0.003 {
		t.Fatalf("cost = %v", usage.CostUSD)
	}
}

func TestParseCodexUsage(t *testing.T) {
	t.Run("turn event", func(t *testing.T) {
		stdout := `{"type":"thread.started"}
{"type":"turn.completed","usage":{"input_tokens":120,"cached_input_tokens":100,"output_tokens":30}}`
		usage := parseCodexUsage(stdout, "")
		if !usage.Known || usage.InputTokens != 120 || usage.OutputTokens != 30 || usage.CacheReadInputTokens != 100 {
			t.Fatalf("usage = %+v", usage)
		}
		if usage.TotalTokens() != 150 {
			t.Fatalf("cached input must not be double counted: total = %d", usage.TotalTokens())
		}
	})

	t.Run("tokens used line", func(t *testing.T) {
		usage := parseCodexUsage("done\n", "tokens used: 1,234\n")
		if !usage.Known || usage.TotalTokens() != 1234 {
			t.Fatalf("usage = %+v", usage)
		}
	})

	t.Run("unknown", func(t *testing.T) {
		if usage := parseCodexUsage(`{"type":"item.completed","usage":{"input_tokens":1}}`, ""); usage.Known {
			t.Fatalf("model JSON must not be mistaken for an event: %+v", usage)
		}
	})
}

func TestBuildArgs(t *testing.T) {
	claude := &CLIClient{provider: "claude-code", model: "sonnet", extraArgs: []string{"--extra"}}
	args := claude.buildArgs(map[string]bool{
		"--output-format":          true,
		"--no-session-persistence": true,
		"--model":                  true,
	}, "ignored")
	want := []string{"-p", "--output-format", "json", "--no-session-persistence", "--model", "sonnet", "--extra"}
	if strings.Join(args, " ") != strings.Join(want, " ") {
		t.Fatalf("claude args = %v, want %v", args, want)
	}

	codex := &CLIClient{provider: "codex", model: "gpt-test"}
	args = codex.buildArgs(map[string]bool{
		"--skip-git-repo-check": true,
		"--ephemeral":           true,
		"--sandbox":             true,
		"--color":               true,
		"--model":               true,
		"--output-last-message": true,
	}, `C:\tmp\last message.txt`)
	want = []string{
		"exec", "-", "--skip-git-repo-check", "--ephemeral",
		"--sandbox", "read-only", "--color", "never",
		"--model", "gpt-test", "--output-last-message", `C:\tmp\last message.txt`,
	}
	if strings.Join(args, " ") != strings.Join(want, " ") {
		t.Fatalf("codex args = %v, want %v", args, want)
	}

	args = codex.buildArgs(map[string]bool{"--ephemeral": false}, "x")
	if strings.Contains(strings.Join(args, " "), "--ephemeral") {
		t.Fatalf("disabled flag survived: %v", args)
	}

	empty := codex.buildArgs(map[string]bool{}, "x")
	if !strings.Contains(strings.Join(empty, " "), "--ephemeral") {
		t.Fatalf("empty capability map should use known-good flags: %v", empty)
	}
}

func TestCappedBuffer(t *testing.T) {
	buf := &cappedBuffer{limit: 5}
	n, err := buf.Write([]byte("abcdefgh"))
	if err != nil || n != 8 {
		t.Fatalf("write = %d, %v", n, err)
	}
	if buf.String() != "abcde" || !buf.Truncated() {
		t.Fatalf("buffer = %q, truncated=%v", buf.String(), buf.Truncated())
	}
}

func TestTailRunes(t *testing.T) {
	if got := tailRunes("你好世界", 2); got != "...世界" {
		t.Fatalf("tail = %q", got)
	}
}
