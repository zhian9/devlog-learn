package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"devlog-learn/internal/pipeline"
)

func TestDryRunCommandJSON(t *testing.T) {
	root := filepath.Join("..", "internal", "ingest", "testdata", "codex")
	var stdout, stderr bytes.Buffer

	cmd := newRootCmd()
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{
		"dry-run",
		"--provider", "codex",
		"--codex-dir", root,
		"--since", "all",
		"--json",
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v\nstderr: %s", err, stderr.String())
	}

	var report pipeline.DryRunReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("decode report: %v\noutput: %s", err, stdout.String())
	}
	if report.Files != 4 || report.Excluded != 1 {
		t.Fatalf("files/excluded = %d/%d", report.Files, report.Excluded)
	}
	if report.SegmentCount < 2 {
		t.Fatalf("segments = %d", report.SegmentCount)
	}
	if report.ParserVersion == 0 || report.SegmenterVersion == 0 || report.RedactionVersion == 0 {
		t.Fatalf("versions = %+v", report)
	}
	for _, item := range report.Segments {
		if len(item.Hash) != 64 || len(item.SegmentID) != 16 {
			t.Fatalf("item identifiers = %+v", item)
		}
	}
}

func TestDryRunCommandHumanOutput(t *testing.T) {
	root := filepath.Join("..", "internal", "ingest", "testdata", "codex")
	var stdout, stderr bytes.Buffer

	cmd := newRootCmd()
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{
		"dry-run",
		"--provider", "codex",
		"--codex-dir", root,
		"--since", "all",
		"--show-drops",
		"--show-events",
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v\nstderr: %s", err, stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{"summary:", "segment=", "drop reasons:", "versions:", "seq=", "role="} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q:\n%s", want, out)
		}
	}
}

func TestDryRunCommandRedactConfig(t *testing.T) {
	root := t.TempDir()
	sessionDir := filepath.Join(root, "demo")
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	body := strings.Join([]string{
		`{"type":"user","timestamp":"2026-04-01T10:00:00Z","sessionId":"s1","cwd":"D:\\work\\demo","message":{"role":"user","content":"contact user@example.com"}}`,
		`{"type":"assistant","timestamp":"2026-04-01T10:00:01Z","sessionId":"s1","cwd":"D:\\work\\demo","message":{"role":"assistant","content":[{"type":"text","text":"Done."}]}}`,
	}, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(sessionDir, "one.jsonl"), []byte(body), 0o644); err != nil {
		t.Fatalf("write session: %v", err)
	}

	configPath := filepath.Join(t.TempDir(), "redact.json")
	if err := os.WriteFile(configPath, []byte(`{"disable_emails": true}`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	run := func(extra ...string) pipeline.DryRunReport {
		var stdout, stderr bytes.Buffer
		cmd := newRootCmd()
		cmd.SetOut(&stdout)
		cmd.SetErr(&stderr)
		args := []string{"dry-run", "--provider", "claude-code", "--claude-dir", root, "--since", "all", "--json"}
		args = append(args, extra...)
		cmd.SetArgs(args)
		if err := cmd.Execute(); err != nil {
			t.Fatalf("Execute: %v\nstderr: %s", err, stderr.String())
		}
		var report pipeline.DryRunReport
		if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
			t.Fatalf("decode report: %v", err)
		}
		return report
	}

	withConfig := run("--redact-config", configPath)
	if withConfig.Redactions["email"] != 0 {
		t.Fatalf("email should be disabled, got %+v", withConfig.Redactions)
	}
	withoutConfig := run()
	if withoutConfig.Redactions["email"] < 1 {
		t.Fatalf("email should be redacted, got %+v", withoutConfig.Redactions)
	}
}

func TestDryRunCommandRejectsCursor(t *testing.T) {
	var stdout, stderr bytes.Buffer
	cmd := newRootCmd()
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"dry-run", "--provider", "cursor", "--since", "all"})

	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "Cursor is not supported") {
		t.Fatalf("error = %v", err)
	}
}
