package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"devlog-learn/internal/questions"
)

func TestQuestionsCommandLifecycle(t *testing.T) {
	doc := commandDoc("qdoc1", "cli", "Question doc", outputTime)
	doc.OpenQuestions = []string{"First open question?", "Second open question?"}
	cacheDir := seedCache(t, doc)
	first := questions.NewID(doc.ID, "First open question?")

	stdout, _, err := runCommand("questions", "--cache-dir", cacheDir)
	if err != nil {
		t.Fatalf("questions: %v", err)
	}
	if !strings.Contains(stdout, first) || !strings.Contains(stdout, "First open question?") {
		t.Fatalf("questions output = %q", stdout)
	}

	stdout, _, err = runCommand("questions", "--cache-dir", cacheDir, "--json")
	if err != nil {
		t.Fatalf("questions json: %v", err)
	}
	var payload struct {
		Count     int               `json:"count"`
		Questions []questions.Entry `json:"questions"`
	}
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("parse questions JSON %q: %v", stdout, err)
	}
	if payload.Count != 2 || len(payload.Questions) != 2 {
		t.Fatalf("payload = %+v", payload)
	}

	stdout, _, err = runCommand("questions", "resolve", first, "--cache-dir", cacheDir, "--note", "fixed in v0.9")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if !strings.Contains(stdout, "resolved "+first) {
		t.Fatalf("resolve output = %q", stdout)
	}

	state, err := questions.Load(questions.Path(cacheDir))
	if err != nil {
		t.Fatalf("load state: %v", err)
	}
	item, ok := state.Items[first]
	if !ok || item.Status != questions.StatusResolved || item.Note != "fixed in v0.9" {
		t.Fatalf("resolved item = %+v, ok=%v", item, ok)
	}

	// Resolving again without --note must preserve the existing note.
	if _, _, err := runCommand("questions", "resolve", first, "--cache-dir", cacheDir); err != nil {
		t.Fatalf("second resolve: %v", err)
	}
	state, err = questions.Load(questions.Path(cacheDir))
	if err != nil {
		t.Fatalf("load state after second resolve: %v", err)
	}
	if state.Items[first].Note != "fixed in v0.9" {
		t.Fatalf("note lost on idempotent resolve: %+v", state.Items[first])
	}

	stdout, _, err = runCommand("questions", "--cache-dir", cacheDir)
	if err != nil {
		t.Fatalf("questions after resolve: %v", err)
	}
	if strings.Contains(stdout, first) {
		t.Fatalf("resolved question still listed as open:\n%s", stdout)
	}

	stdout, _, err = runCommand("questions", "--cache-dir", cacheDir, "--status", "resolved", "--json")
	if err != nil {
		t.Fatalf("resolved list: %v", err)
	}
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("parse resolved JSON: %v", err)
	}
	if payload.Count != 1 || payload.Questions[0].ID != first || payload.Questions[0].Note != "fixed in v0.9" {
		t.Fatalf("resolved payload = %+v", payload)
	}

	if _, _, err := runCommand("questions", "reopen", first, "--cache-dir", cacheDir); err != nil {
		t.Fatalf("reopen: %v", err)
	}
	stdout, _, err = runCommand("questions", "--cache-dir", cacheDir, "--json")
	if err != nil {
		t.Fatalf("questions after reopen: %v", err)
	}
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("parse reopened JSON: %v", err)
	}
	if payload.Count != 2 {
		t.Fatalf("reopened payload = %+v", payload)
	}

	if _, _, err := runCommand("questions", "resolve", "q_missing", "--cache-dir", cacheDir); err == nil {
		t.Fatal("unknown question id accepted")
	}
	if _, _, err := runCommand("questions", "reopen", first, "--cache-dir", cacheDir); err == nil {
		t.Fatal("reopening an open question accepted")
	}
	if _, _, err := runCommand("questions", "--cache-dir", cacheDir, "--status", "maybe"); err == nil {
		t.Fatal("invalid status accepted")
	}
}

func TestQuestionsCommandFilters(t *testing.T) {
	alpha := commandDoc("qa1", "Alpha", "Alpha doc", outputTime)
	alpha.OpenQuestions = []string{"Alpha question?"}
	beta := commandDoc("qb1", "Beta", "Beta doc", outputTime)
	beta.OpenQuestions = []string{"Beta question?"}
	cacheDir := seedCache(t, alpha, beta)

	stdout, _, err := runCommand("questions", "--cache-dir", cacheDir, "--project", "beta", "--json")
	if err != nil {
		t.Fatalf("questions project: %v", err)
	}
	var payload struct {
		Count     int               `json:"count"`
		Questions []questions.Entry `json:"questions"`
	}
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("parse JSON: %v", err)
	}
	if payload.Count != 1 || payload.Questions[0].DocID != beta.ID {
		t.Fatalf("project filter = %+v", payload)
	}

	stdout, _, err = runCommand("questions", "--cache-dir", cacheDir, "--doc", alpha.ID, "--json")
	if err != nil {
		t.Fatalf("questions doc: %v", err)
	}
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("parse JSON: %v", err)
	}
	if payload.Count != 1 || payload.Questions[0].DocID != alpha.ID {
		t.Fatalf("doc filter = %+v", payload)
	}
}

func TestDigestOmitsResolvedQuestions(t *testing.T) {
	doc := commandDoc("dg1", "cli", "Digest doc", outputTime)
	doc.OpenQuestions = []string{"Resolved question?", "Still open question?"}
	cacheDir := seedCache(t, doc)
	resolvedID := questions.NewID(doc.ID, "Resolved question?")

	if _, _, err := runCommand("questions", "resolve", resolvedID, "--cache-dir", cacheDir); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	out := t.TempDir()
	if _, _, err := runCommand("digest", "--cache-dir", cacheDir, "--out", out); err != nil {
		t.Fatalf("digest: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(out, "AGENTS.md"))
	if err != nil {
		t.Fatalf("read digest: %v", err)
	}
	content := string(data)
	if strings.Contains(content, "Resolved question?") {
		t.Fatalf("resolved question leaked into digest:\n%s", content)
	}
	if !strings.Contains(content, "Still open question?") {
		t.Fatalf("open question missing from digest:\n%s", content)
	}
}
