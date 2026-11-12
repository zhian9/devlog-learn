package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"devlog-learn/internal/cursor"
	"devlog-learn/internal/extract"
	"devlog-learn/internal/questions"
)

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestStatusCommandJSON(t *testing.T) {
	pending := commandDoc("st1", "demo", "Pending doc", outputTime, "go")
	pending.OpenQuestions = []string{"Open question A?", "Question to resolve?"}
	approved := commandDoc("st2", "demo", "Approved doc", outputTime, "go")
	approved.ReviewStatus = extract.ReviewApproved
	stale := commandDoc("st3", "demo", "Stale doc", outputTime, "go")
	stale.Lifecycle = extract.LifecycleStale
	cacheDir := seedCache(t, pending, approved, stale)

	state := questions.New()
	resolvedID := questions.NewID(pending.ID, "Question to resolve?")
	state.Items[resolvedID] = questions.Item{
		DocID: pending.ID, Question: "Question to resolve?",
		Status: questions.StatusResolved, Note: "done", ResolvedAt: outputTime,
	}
	if err := state.Save(questions.Path(cacheDir), outputTime); err != nil {
		t.Fatalf("save questions: %v", err)
	}

	cursorPath := filepath.Join(t.TempDir(), "cursor.json")
	cursorState := cursor.NewState()
	cursorState.MarkOK("file-a", cursor.Fingerprint{}, []string{pending.ID}, outputTime)
	cursorState.MarkError("file-b", cursor.Fingerprint{}, errors.New("boom"), outputTime)
	if err := cursorState.Save(cursorPath, outputTime); err != nil {
		t.Fatalf("save cursor: %v", err)
	}

	out := t.TempDir()
	writeTestFile(t, filepath.Join(out, "index.json"), `{"generated_at":"2020-01-01T00:00:00Z"}`)
	digestPath := filepath.Join(out, "AGENTS.md")
	writeTestFile(t, digestPath, "# Project memory (devlog-learn)\n")
	oldTime := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := os.Chtimes(digestPath, oldTime, oldTime); err != nil {
		t.Fatalf("chtimes digest: %v", err)
	}

	claudeDir := t.TempDir()
	writeTestFile(t, filepath.Join(claudeDir, "proj", "a.jsonl"), "{}\n")
	writeTestFile(t, filepath.Join(claudeDir, "proj", "notes.txt"), "ignored")
	codexDir := t.TempDir()
	writeTestFile(t, filepath.Join(codexDir, "rollout-1.jsonl"), "{}\n")
	writeTestFile(t, filepath.Join(codexDir, "other.jsonl"), "{}\n")

	stdout, _, err := runCommand("status",
		"--cache-dir", cacheDir,
		"--cursor", cursorPath,
		"--out", out,
		"--claude-dir", claudeDir,
		"--codex-dir", codexDir,
		"--claude-bin", "devlog-learn-missing-claude",
		"--codex-bin", "devlog-learn-missing-codex",
		"--json")
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	var report statusReport
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("parse status JSON %q: %v", stdout, err)
	}
	if report.Cache.Active != 2 || report.Cache.Stale != 1 {
		t.Fatalf("cache counts = %+v", report.Cache)
	}
	if report.Cache.Pending != 1 || report.Cache.Approved != 1 || report.Cache.Rejected != 0 {
		t.Fatalf("review counts = %+v", report.Cache)
	}
	if !report.Cache.Exists || report.Cache.SizeBytes == 0 {
		t.Fatalf("cache existence = %+v", report.Cache)
	}
	if report.Questions.Open != 1 || report.Questions.Resolved != 1 {
		t.Fatalf("question counts = %+v", report.Questions)
	}
	if !report.Cursor.Exists || report.Cursor.Files != 2 || report.Cursor.Errors != 1 || !report.Cursor.VersionsMatch {
		t.Fatalf("cursor = %+v", report.Cursor)
	}
	if !report.Outputs.IndexExists || !report.Outputs.IndexStale || !report.Outputs.DigestExists || !report.Outputs.DigestStale {
		t.Fatalf("outputs = %+v", report.Outputs)
	}
	if len(report.Providers) != 2 {
		t.Fatalf("providers = %+v", report.Providers)
	}
	claude, codex := report.Providers[0], report.Providers[1]
	if claude.Name != "claude-code" || !claude.DirExists || claude.SessionFiles != 1 || claude.BinFound {
		t.Fatalf("claude provider = %+v", claude)
	}
	if codex.Name != "codex" || !codex.DirExists || codex.SessionFiles != 1 || codex.BinFound {
		t.Fatalf("codex provider = %+v", codex)
	}
}

func TestStatusCommandTextAndMissingEntries(t *testing.T) {
	cacheDir := seedCache(t, commandDoc("empty1", "demo", "Doc", outputTime))
	missing := filepath.Join(t.TempDir(), "missing")

	stdout, _, err := runCommand("status",
		"--cache-dir", cacheDir,
		"--cursor", filepath.Join(missing, "cursor.json"),
		"--out", filepath.Join(missing, "out"),
		"--claude-dir", filepath.Join(missing, "claude"),
		"--codex-dir", filepath.Join(missing, "codex"),
		"--claude-bin", "devlog-learn-missing-claude",
		"--codex-bin", "devlog-learn-missing-codex")
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	for _, want := range []string{"devlog-learn", "docs      active=1 stale=0", "index=no", "digest=no", "missing"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("status output missing %q:\n%s", want, stdout)
		}
	}
}

func TestStatusCommandReportsCorruptCursor(t *testing.T) {
	cacheDir := seedCache(t)
	cursorPath := filepath.Join(t.TempDir(), "cursor.json")
	writeTestFile(t, cursorPath, "{not json")
	stdout, stderr, err := runCommand("status", "--cache-dir", cacheDir, "--cursor", cursorPath, "--json")
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	var report statusReport
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("parse JSON: %v", err)
	}
	if len(report.Warnings) == 0 {
		t.Fatalf("corrupt cursor produced no warning: %+v", report)
	}
	if !strings.Contains(stderr, "warning") {
		t.Fatalf("stderr = %q", stderr)
	}
	var zero time.Time
	if report.Cursor.UpdatedAt != nil && *report.Cursor.UpdatedAt != zero {
		t.Fatalf("corrupt cursor updated_at = %v", report.Cursor.UpdatedAt)
	}
}
