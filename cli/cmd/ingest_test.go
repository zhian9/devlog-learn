package main

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestSelectProvidersRejectsCursor(t *testing.T) {
	_, err := selectProviders(&ingestOptions{provider: "cursor"})
	if err == nil {
		t.Fatal("expected an error for provider cursor")
	}
	if !strings.Contains(err.Error(), "Cursor is not supported") {
		t.Fatalf("error = %v", err)
	}
}

func TestSelectProvidersAll(t *testing.T) {
	specs, err := selectProviders(&ingestOptions{provider: "all"})
	if err != nil {
		t.Fatalf("selectProviders: %v", err)
	}
	if len(specs) != 2 {
		t.Fatalf("specs = %d, want 2", len(specs))
	}
	if specs[0].source.Name() != "claude-code" || specs[1].source.Name() != "codex" {
		t.Fatalf("unexpected providers: %q, %q", specs[0].source.Name(), specs[1].source.Name())
	}
}

func TestIngestCommandJSONReport(t *testing.T) {
	root := filepath.Join("..", "internal", "ingest", "testdata", "codex")
	var stdout, stderr bytes.Buffer

	cmd := newRootCmd()
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{
		"ingest",
		"--provider", "codex",
		"--codex-dir", root,
		"--since", "all",
		"--json",
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v\nstderr: %s", err, stderr.String())
	}

	var report ingestReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("decode report: %v\noutput: %s", err, stdout.String())
	}
	if report.ParserVersion == 0 {
		t.Fatal("parser_version must be reported")
	}
	if report.Totals.Files != 4 {
		t.Fatalf("files = %d, want 4", report.Totals.Files)
	}
	if report.Totals.Excluded != 1 {
		t.Fatalf("excluded = %d, want 1", report.Totals.Excluded)
	}
	if report.Totals.Events == 0 {
		t.Fatal("expected parsed events")
	}

	var excluded *ingestFileReport
	for i := range report.Files {
		if report.Files[i].Excluded {
			excluded = &report.Files[i]
			break
		}
	}
	if excluded == nil {
		t.Fatal("expected one excluded subagent session")
	}
	if excluded.Path == "" {
		t.Fatal("excluded file must still report its source path")
	}
}

func TestIngestCommandRejectsJSONAndEventsTogether(t *testing.T) {
	var stdout, stderr bytes.Buffer
	cmd := newRootCmd()
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{
		"ingest",
		"--provider", "codex",
		"--codex-dir", filepath.Join("..", "internal", "ingest", "testdata", "codex"),
		"--since", "all",
		"--json",
		"--show-events",
	})

	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("error = %v, want mutually exclusive", err)
	}
}
