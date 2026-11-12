package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"devlog-learn/internal/extract"
	"devlog-learn/internal/search"
)

func TestSearchCommandTextAndJSON(t *testing.T) {
	timer := commandDoc("srch1", "test-1", "Fix timer bug", outputTime, "timer", "go")
	timer.Summary = "The timer used setInterval."
	other := commandDoc("srch2", "test-1", "Unrelated work", outputTime.Add(-time.Hour), "design")
	stale := commandDoc("srch3", "test-1", "Fix timer stale", outputTime, "timer")
	stale.Lifecycle = extract.LifecycleStale
	cacheDir := seedCache(t, timer, other, stale)

	stdout, stderr, err := runCommand("search", "timer", "--cache-dir", cacheDir)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if !strings.Contains(stdout, timer.ID) || !strings.Contains(stdout, "Fix timer bug") {
		t.Fatalf("search output missing result:\n%s", stdout)
	}
	if strings.Contains(stdout, stale.ID) || strings.Contains(stdout, other.ID) {
		t.Fatalf("search output leaked non-matching/stale docs:\n%s", stdout)
	}
	if !strings.Contains(stderr, "results=1") {
		t.Fatalf("stderr = %q", stderr)
	}

	stdout, _, err = runCommand("search", "timer", "--cache-dir", cacheDir, "--json", "--limit", "5")
	if err != nil {
		t.Fatalf("search json: %v", err)
	}
	var payload struct {
		Query   string          `json:"query"`
		Count   int             `json:"count"`
		Results []search.Result `json:"results"`
	}
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("parse search JSON %q: %v", stdout, err)
	}
	if payload.Query != "timer" || payload.Count != 1 || payload.Results[0].ID != timer.ID {
		t.Fatalf("payload = %+v", payload)
	}
	if payload.Results[0].MarkdownPath == "" {
		t.Fatalf("markdown path missing: %+v", payload.Results[0])
	}
}

func TestSearchCommandFilters(t *testing.T) {
	alpha := commandDoc("flt1", "Alpha Project", "Build fix", outputTime, "build", "go")
	alpha.ReviewStatus = extract.ReviewApproved
	beta := commandDoc("flt2", "Beta", "Build fix", outputTime, "build", "rust")
	cacheDir := seedCache(t, alpha, beta)

	stdout, _, err := runCommand("search", "build", "--cache-dir", cacheDir, "--project", "alpha-project", "--json")
	if err != nil {
		t.Fatalf("search project: %v", err)
	}
	var payload struct {
		Count   int             `json:"count"`
		Results []search.Result `json:"results"`
	}
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("parse JSON: %v", err)
	}
	if payload.Count != 1 || payload.Results[0].ID != alpha.ID {
		t.Fatalf("project filter = %+v", payload)
	}

	stdout, _, err = runCommand("search", "build", "--cache-dir", cacheDir, "--review", "approved", "--json")
	if err != nil {
		t.Fatalf("search review: %v", err)
	}
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("parse JSON: %v", err)
	}
	if payload.Count != 1 || payload.Results[0].ID != alpha.ID {
		t.Fatalf("review filter = %+v", payload)
	}

	stdout, _, err = runCommand("search", "build", "--cache-dir", cacheDir, "--tag", "rust", "--json")
	if err != nil {
		t.Fatalf("search tag: %v", err)
	}
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("parse JSON: %v", err)
	}
	if payload.Count != 1 || payload.Results[0].ID != beta.ID {
		t.Fatalf("tag filter = %+v", payload)
	}

	if _, _, err := runCommand("search", "build", "--cache-dir", cacheDir, "--review", "maybe"); err == nil {
		t.Fatal("invalid review filter accepted")
	}
	if _, _, err := runCommand("search", "build", "--cache-dir", cacheDir, "--limit", "0"); err == nil {
		t.Fatal("invalid limit accepted")
	}
}

func TestSearchEmptyQueryReturnsRecent(t *testing.T) {
	older := commandDoc("old1", "demo", "Older", outputTime.Add(-2*time.Hour))
	newer := commandDoc("new1", "demo", "Newer", outputTime)
	cacheDir := seedCache(t, older, newer)

	stdout, _, err := runCommand("search", "--cache-dir", cacheDir, "--json", "--limit", "1")
	if err != nil {
		t.Fatalf("search empty: %v", err)
	}
	var payload struct {
		Results []search.Result `json:"results"`
	}
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("parse JSON: %v", err)
	}
	if len(payload.Results) != 1 || payload.Results[0].ID != newer.ID {
		t.Fatalf("recent order = %+v", payload)
	}
}

func TestProjectsCommand(t *testing.T) {
	alpha := commandDoc("prj1", "Alpha", "One", outputTime)
	alpha.ReviewStatus = extract.ReviewApproved
	beta := commandDoc("prj2", "Beta", "Two", outputTime)
	stale := commandDoc("prj3", "Alpha", "Stale", outputTime)
	stale.Lifecycle = extract.LifecycleStale
	cacheDir := seedCache(t, alpha, beta, stale)

	stdout, _, err := runCommand("projects", "--cache-dir", cacheDir, "--json")
	if err != nil {
		t.Fatalf("projects: %v", err)
	}
	var payload struct {
		Count    int              `json:"count"`
		Projects []search.Project `json:"projects"`
	}
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("parse projects JSON %q: %v", stdout, err)
	}
	if payload.Count != 2 || len(payload.Projects) != 2 {
		t.Fatalf("payload = %+v", payload)
	}
	byName := map[string]search.Project{}
	for _, project := range payload.Projects {
		byName[project.Name] = project
	}
	if byName["Alpha"].Docs != 1 || byName["Alpha"].Approved != 1 {
		t.Fatalf("alpha counts = %+v", byName["Alpha"])
	}
	if byName["Beta"].Docs != 1 || byName["Beta"].Pending != 1 {
		t.Fatalf("beta counts = %+v", byName["Beta"])
	}

	stdout, _, err = runCommand("projects", "--cache-dir", cacheDir)
	if err != nil {
		t.Fatalf("projects text: %v", err)
	}
	if !strings.Contains(stdout, "Alpha") || !strings.Contains(stdout, "Beta") {
		t.Fatalf("text output = %q", stdout)
	}
}
