package main

import (
	"encoding/json"
	"strings"
	"testing"

	"devlog-learn/internal/cache"
	"devlog-learn/internal/extract"
	"devlog-learn/internal/pipeline"
)

func TestSessionsCommandListsSelectorsAsJSON(t *testing.T) {
	p := newE2EPaths(t)
	stdout, stderr, err := runCommand(
		"sessions",
		"--provider", "all",
		"--claude-dir", p.claudeDir,
		"--codex-dir", p.codexDir,
		"--since", "all",
		"--cache-dir", p.cacheDir,
		"--json",
	)
	if err != nil {
		t.Fatalf("sessions: %v\nstderr:\n%s", err, stderr)
	}
	var list pipeline.SessionList
	if err := json.Unmarshal([]byte(stdout), &list); err != nil {
		t.Fatalf("decode sessions JSON: %v\n%s", err, stdout)
	}
	if len(list.Sessions) != 2 || list.SegmentCount != 2 {
		t.Fatalf("list = %+v", list)
	}
	bySelector := map[string]pipeline.SelectableSession{}
	for _, session := range list.Sessions {
		bySelector[session.Selector] = session
		if session.SegmentCount != 1 || len(session.Segments) != 1 {
			t.Fatalf("session %s = %+v", session.Selector, session)
		}
		if session.Segments[0].ID == "" || session.Segments[0].Extracted {
			t.Fatalf("segment = %+v", session.Segments[0])
		}
	}
	claude, ok := bySelector["claude-code:claude-e2e"]
	if !ok {
		t.Fatalf("missing claude selector: %+v", bySelector)
	}
	if _, ok := bySelector["codex:codex-e2e"]; !ok {
		t.Fatalf("missing codex selector: %+v", bySelector)
	}

	// A canonical Doc makes the next listing report doc=yes.
	store, err := cache.Open(p.cacheDir)
	if err != nil {
		t.Fatalf("cache.Open: %v", err)
	}
	seed := &extract.Doc{SchemaVersion: extract.SchemaVersion, ID: claude.Segments[0].ID, Title: "seed"}
	if err := store.SaveDoc(seed); err != nil {
		t.Fatalf("SaveDoc: %v", err)
	}
	stdout, stderr, err = runCommand(
		"sessions", "--provider", "claude-code", "--claude-dir", p.claudeDir,
		"--since", "all", "--cache-dir", p.cacheDir, "--json",
	)
	if err != nil {
		t.Fatalf("sessions after seed: %v\nstderr:\n%s", err, stderr)
	}
	if err := json.Unmarshal([]byte(stdout), &list); err != nil {
		t.Fatalf("decode sessions JSON: %v", err)
	}
	if len(list.Sessions) != 1 || list.Sessions[0].Extracted != 1 || !list.Sessions[0].Segments[0].Extracted {
		t.Fatalf("extracted listing = %+v", list.Sessions)
	}
}

func TestSessionsCommandTextOutput(t *testing.T) {
	p := newE2EPaths(t)
	stdout, stderr, err := runCommand(
		"sessions",
		"--provider", "all",
		"--claude-dir", p.claudeDir,
		"--codex-dir", p.codexDir,
		"--since", "all",
		"--cache-dir", p.cacheDir,
	)
	if err != nil {
		t.Fatalf("sessions: %v\nstderr:\n%s", err, stderr)
	}
	for _, want := range []string{
		"claude-code:claude-e2e", "codex:codex-e2e", "doc=no",
		"use: devlog-learn extract --session",
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("stdout missing %q:\n%s", want, stdout)
		}
	}
}
