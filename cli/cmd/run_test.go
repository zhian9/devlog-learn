package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"devlog-learn/internal/cache"
	"devlog-learn/internal/index"
	"devlog-learn/internal/pipeline"
)

func TestRunCommandExtractsRendersAndIndexes(t *testing.T) {
	p := newE2EPaths(t)
	args := []string{
		"run",
		"--provider", "all",
		"--claude-dir", p.claudeDir,
		"--codex-dir", p.codexDir,
		"--since", "all",
		"--segments", "0",
		"--claude-bin", p.claudeBin,
		"--codex-bin", p.codexBin,
		"--out", p.renderOut,
		"--cache-dir", p.cacheDir,
		"--cursor", p.cursor,
		"--timeout", "30s",
	}
	stdout, stderr, err := runCommand(args...)
	if err != nil {
		t.Fatalf("run: %v\nstderr:\n%s", err, stderr)
	}
	if strings.TrimSpace(stdout) != p.renderOut {
		t.Fatalf("stdout = %q, want output dir %q", stdout, p.renderOut)
	}
	for _, want := range []string{"extracted=2", "markdown=2", "docs=2"} {
		if !strings.Contains(stderr, want) {
			t.Fatalf("stderr missing %q:\n%s", want, stderr)
		}
	}

	raw, err := os.ReadFile(filepath.Join(p.renderOut, "index.json"))
	if err != nil {
		t.Fatalf("read index.json: %v", err)
	}
	var global index.Document
	if err := json.Unmarshal(raw, &global); err != nil {
		t.Fatalf("decode index.json: %v", err)
	}
	if global.Total != 2 || len(global.Docs) != 2 {
		t.Fatalf("index = %+v", global)
	}
	for _, entry := range global.Docs {
		path := filepath.Join(p.renderOut, filepath.FromSlash(entry.Path))
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("index path %q does not resolve: %v", entry.Path, err)
		}
	}

	// A second run is incremental: files are skipped but render/index still work.
	stdout, stderr, err = runCommand(args...)
	if err != nil {
		t.Fatalf("second run: %v\nstderr:\n%s", err, stderr)
	}
	if strings.TrimSpace(stdout) != p.renderOut {
		t.Fatalf("second stdout = %q", stdout)
	}
	if !strings.Contains(stderr, "skipped=2") || !strings.Contains(stderr, "selected=0") {
		t.Fatalf("second summary =\n%s", stderr)
	}
	if !strings.Contains(stderr, "markdown=2") {
		t.Fatalf("second run should re-render cached docs:\n%s", stderr)
	}
}

func TestRunCommandWithSessionSelection(t *testing.T) {
	p := newE2EPaths(t)
	stdout, stderr, err := runCommand(
		"sessions", "--provider", "claude-code", "--claude-dir", p.claudeDir,
		"--since", "all", "--cache-dir", p.cacheDir, "--json",
	)
	if err != nil {
		t.Fatalf("sessions: %v\nstderr:\n%s", err, stderr)
	}
	var list pipeline.SessionList
	if err := json.Unmarshal([]byte(stdout), &list); err != nil {
		t.Fatalf("decode sessions: %v", err)
	}
	if len(list.Sessions) != 1 {
		t.Fatalf("sessions = %+v", list.Sessions)
	}
	selector := list.Sessions[0].Selector
	outDir := filepath.Join(p.root, "selected-markdown")

	_, stderr, err = runCommand(
		"run",
		"--provider", "claude-code",
		"--claude-dir", p.claudeDir,
		"--since", "all",
		"--session", selector,
		"--claude-bin", p.claudeBin,
		"--out", outDir,
		"--cache-dir", p.cacheDir,
		"--cursor", p.cursor,
		"--timeout", "30s",
	)
	if err != nil {
		t.Fatalf("selected run: %v\nstderr:\n%s", err, stderr)
	}
	if !strings.Contains(stderr, "extracted=1") || !strings.Contains(stderr, "markdown=1") {
		t.Fatalf("selected summary =\n%s", stderr)
	}

	store, err := cache.Open(p.cacheDir)
	if err != nil {
		t.Fatalf("cache.Open: %v", err)
	}
	docs, _ := store.ListActiveDocs()
	if len(docs) != 1 || docs[0].Doc.Source != "claude-code" {
		t.Fatalf("active docs = %+v", docs)
	}
	if _, err := os.Stat(filepath.Join(outDir, "index.json")); err != nil {
		t.Fatalf("selected run did not build an index: %v", err)
	}
}

func TestRunCommandRejectsSegmentsWithSelection(t *testing.T) {
	_, _, err := runCommand("run", "--session", "claude-code:x", "--segments", "2")
	if err == nil || !strings.Contains(err.Error(), "cannot be combined") {
		t.Fatalf("error = %v", err)
	}
}
