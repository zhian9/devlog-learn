package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"devlog-learn/internal/extract"
)

func TestExperienceListShowAndReadOnly(t *testing.T) {
	docs := []*extract.Doc{
		commandDoc("aaa111", "Demo Project", "Fix build", outputTime, "go"),
		commandDoc("bbb222", "Other Project", "Tune cache", outputTime.Add(-time.Hour), "cache"),
	}
	cacheDir := seedCache(t, docs...)

	stdout, _, err := runCommand("experience", "list", "--cache-dir", cacheDir, "--json")
	if err != nil {
		t.Fatalf("list: %v\n%s", err, stdout)
	}
	var list struct {
		Count       int `json:"count"`
		Experiences []struct {
			Project    string `json:"project"`
			Kind       string `json:"kind"`
			KindSource string `json:"kind_source"`
			Docs       int    `json:"docs"`
		} `json:"experiences"`
	}
	if err := json.Unmarshal([]byte(stdout), &list); err != nil {
		t.Fatalf("decode list: %v\n%s", err, stdout)
	}
	if list.Count != 2 || len(list.Experiences) != 2 {
		t.Fatalf("list = %+v", list)
	}

	stdout, _, err = runCommand("experience", "show", "--project", "Demo Project", "--cache-dir", cacheDir)
	if err != nil {
		t.Fatalf("show: %v\n%s", err, stdout)
	}
	if !strings.Contains(stdout, "# Demo Project 项目经历") {
		t.Fatalf("show output = %s", stdout)
	}
	if !strings.Contains(stdout, "## 复习清单") {
		t.Fatalf("checklist missing = %s", stdout)
	}
	if _, err := os.Stat(filepath.Join(cacheDir, "experiences.json")); !os.IsNotExist(err) {
		t.Fatalf("read-only command created experiences.json: %v", err)
	}
}

func TestExperienceSetAndUnset(t *testing.T) {
	cacheDir := seedCache(t, commandDoc("aaa111", "Demo Project", "Fix build", outputTime, "go"))

	if _, _, err := runCommand("experience", "set", "--cache-dir", cacheDir,
		"--project", "Demo Project", "--kind", "internship", "--org", "ACME", "--role", "Backend Intern"); err != nil {
		t.Fatalf("set: %v", err)
	}
	if _, err := os.Stat(filepath.Join(cacheDir, "experiences.json")); err != nil {
		t.Fatalf("sidecar missing: %v", err)
	}

	stdout, _, err := runCommand("experience", "show", "--project", "Demo Project", "--cache-dir", cacheDir)
	if err != nil {
		t.Fatalf("show: %v", err)
	}
	if !strings.Contains(stdout, "# ACME 实习经验") {
		t.Fatalf("manual kind not applied: %s", stdout)
	}

	stdout, _, err = runCommand("experience", "list", "--cache-dir", cacheDir, "--json")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if !strings.Contains(stdout, `"kind_source": "manual"`) {
		t.Fatalf("manual source missing: %s", stdout)
	}

	if _, _, err := runCommand("experience", "unset", "--cache-dir", cacheDir, "--project", "Demo Project"); err != nil {
		t.Fatalf("unset: %v", err)
	}
	stdout, _, err = runCommand("experience", "show", "--project", "Demo Project", "--cache-dir", cacheDir)
	if err != nil {
		t.Fatalf("show after unset: %v", err)
	}
	if !strings.Contains(stdout, "# Demo Project 项目经历") {
		t.Fatalf("manual kind survived unset: %s", stdout)
	}
}

func TestExperienceChecklistAndRender(t *testing.T) {
	cacheDir := seedCache(t,
		commandDoc("aaa111", "Demo Project", "Fix build", outputTime, "go"),
		commandDoc("bbb222", "Demo Project", "Tune cache", outputTime.Add(-time.Hour), "cache"),
	)

	stdout, _, err := runCommand("experience", "checklist", "--project", "Demo Project", "--cache-dir", cacheDir)
	if err != nil {
		t.Fatalf("checklist: %v", err)
	}
	if !strings.Contains(stdout, "- [ ]") {
		t.Fatalf("checklist output = %s", stdout)
	}

	out := t.TempDir()
	if _, _, err := runCommand("experience", "render", "--cache-dir", cacheDir, "--out", out); err != nil {
		t.Fatalf("render: %v", err)
	}
	path := filepath.Join(out, "Demo-Project", "EXPERIENCE.md")
	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read experience: %v", err)
	}
	if _, _, err := runCommand("experience", "render", "--cache-dir", cacheDir, "--out", out); err != nil {
		t.Fatalf("render again: %v", err)
	}
	second, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read experience again: %v", err)
	}
	if string(first) != string(second) {
		t.Fatal("render is not deterministic")
	}
	if _, err := os.Stat(filepath.Join(out, "EXPERIENCE.md")); err != nil {
		t.Fatalf("overview missing: %v", err)
	}
}

func TestExperienceEmptyJSONAndUnknownProject(t *testing.T) {
	cacheDir := seedCache(t)
	stdout, _, err := runCommand("experience", "list", "--cache-dir", cacheDir, "--json")
	if err != nil {
		t.Fatalf("empty list: %v", err)
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("decode: %v\n%s", err, stdout)
	}
	if string(payload["experiences"]) != "[]" {
		t.Fatalf("empty experiences = %s", payload["experiences"])
	}

	if _, _, err := runCommand("experience", "show", "--project", "missing", "--cache-dir", cacheDir); err == nil {
		t.Fatal("unknown project did not error")
	}
	if _, _, err := runCommand("experience", "set", "--project", "missing", "--kind", "internship", "--cache-dir", cacheDir); err == nil {
		t.Fatal("set on unknown project did not error")
	}
}
