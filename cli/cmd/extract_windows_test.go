//go:build windows

package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExtractCommandEndToEndWithFakeCLI(t *testing.T) {
	root := t.TempDir()
	projectDir := filepath.Join(root, "demo-project")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	session := strings.Join([]string{
		`{"type":"user","timestamp":"2026-05-01T09:00:00Z","sessionId":"s1","cwd":"D:\\work\\demo","message":{"role":"user","content":"Build fails with error X"}}`,
		`{"type":"assistant","timestamp":"2026-05-01T09:00:10Z","sessionId":"s1","cwd":"D:\\work\\demo","message":{"role":"assistant","content":[{"type":"text","text":"Fixed"}]}}`,
	}, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(projectDir, "session.jsonl"), []byte(session), 0o644); err != nil {
		t.Fatalf("write session: %v", err)
	}

	docJSON := `{"title":"Fix build","summary":"s","knowledge_type":"bug_fix","status":"unknown","evidence":[{"event_seq":1,"kind":"user_report","quote":"Build fails"}],"confidence":0.8}`
	quoted, err := json.Marshal(docJSON)
	if err != nil {
		t.Fatalf("marshal doc: %v", err)
	}
	envelope := `{"type":"result","result":` + string(quoted) + `,"modelUsage":{"fake-model":{}}}`

	fake := filepath.Join(t.TempDir(), "fake-claude.cmd")
	body := strings.Join([]string{
		`@echo off`,
		`if "%~1"=="--version" (echo fake-cli 1.0& exit /b 0)`,
		`if "%~1"=="--help" (echo --output-format& echo --no-session-persistence& echo --model& exit /b 0)`,
		`more > nul`,
		`echo ` + envelope,
		`exit /b 0`,
	}, "\n")
	body = strings.ReplaceAll(body, "\n", "\r\n")
	if err := os.WriteFile(fake, []byte(body), 0o644); err != nil {
		t.Fatalf("write fake cli: %v", err)
	}

	outDir := filepath.Join(t.TempDir(), "docs")
	cacheDir := filepath.Join(t.TempDir(), "cache")
	cursorPath := filepath.Join(t.TempDir(), "cursor.json")
	var stdout, stderr bytes.Buffer
	cmd := newRootCmd()
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{
		"extract",
		"--provider", "claude-code",
		"--claude-dir", root,
		"--since", "all",
		"--segments", "1",
		"--claude-bin", fake,
		"--out", outDir,
		"--cache-dir", cacheDir,
		"--cursor", cursorPath,
		"--timeout", "10s",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v\nstderr: %s", err, stderr.String())
	}

	entries, err := os.ReadDir(outDir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("output dir = %v, err = %v", entries, err)
	}
	data, err := os.ReadFile(filepath.Join(outDir, entries[0].Name()))
	if err != nil {
		t.Fatalf("read doc: %v", err)
	}
	var doc struct {
		ID           string `json:"id"`
		Title        string `json:"title"`
		ReviewStatus string `json:"review_status"`
		Provenance   struct {
			Provider   string `json:"provider"`
			Model      string `json:"model"`
			CLIVersion string `json:"cli_version"`
		} `json:"provenance"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("decode doc: %v\n%s", err, data)
	}
	if doc.Title != "Fix build" || doc.ID == "" {
		t.Fatalf("doc = %+v", doc)
	}
	if doc.ReviewStatus != "pending" {
		t.Fatalf("review status = %q", doc.ReviewStatus)
	}
	if doc.Provenance.Provider != "claude-code" || doc.Provenance.CLIVersion != "fake-cli 1.0" {
		t.Fatalf("provenance = %+v", doc.Provenance)
	}
	if doc.Provenance.Model != "fake-model" {
		t.Fatalf("model = %q", doc.Provenance.Model)
	}
	if !strings.Contains(stdout.String(), entries[0].Name()) {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

// TestExtractCommandEndToEndUnicodePaths runs the real extract command through
// a .cmd shim whose directory contains CJK characters, spaces, and an
// ampersand. This is the regression that matters for the PowerShell wrapper.
func TestExtractCommandEndToEndUnicodePaths(t *testing.T) {
	root := filepath.Join(t.TempDir(), "测试 目录 & space")
	claudeDir := filepath.Join(root, "claude logs")
	writeClaudeE2EFixture(t, claudeDir)
	fake := writeFakeCLI(t, filepath.Join(root, "fake bin & tools"), "fake claude", "fake-claude 1.0", e2eDocResponse)

	outDir := filepath.Join(root, "输出 docs")
	cacheDir := filepath.Join(root, "缓存 cache")
	cursorPath := filepath.Join(root, "cursor 游标.json")
	stdout, stderr, err := runCommand(
		"extract",
		"--provider", "claude-code",
		"--claude-dir", claudeDir,
		"--since", "all",
		"--segments", "0",
		"--claude-bin", fake,
		"--out", outDir,
		"--cache-dir", cacheDir,
		"--cursor", cursorPath,
		"--timeout", "30s",
	)
	if err != nil {
		t.Fatalf("extract: %v\nstderr:\n%s", err, stderr)
	}
	entries, err := os.ReadDir(outDir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("output dir = %v, err = %v", entries, err)
	}
	if !strings.Contains(stdout, entries[0].Name()) {
		t.Fatalf("stdout = %q", stdout)
	}
	data, err := os.ReadFile(filepath.Join(outDir, entries[0].Name()))
	if err != nil {
		t.Fatalf("read doc: %v", err)
	}
	var doc struct {
		ID         string `json:"id"`
		Project    string `json:"project"`
		Provenance struct {
			Provider   string `json:"provider"`
			CLIVersion string `json:"cli_version"`
		} `json:"provenance"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("decode doc: %v\n%s", err, data)
	}
	if doc.ID == "" || doc.Project != "demo" || doc.Provenance.Provider != "claude-code" || doc.Provenance.CLIVersion != "fake-claude 1.0" {
		t.Fatalf("doc = %+v", doc)
	}
}
