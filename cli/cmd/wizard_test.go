package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"devlog-learn/internal/cache"
)

func runCommandWithInput(input string, args ...string) (string, string, error) {
	var stdout, stderr bytes.Buffer
	cmd := newRootCmd()
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetIn(strings.NewReader(input))
	cmd.SetArgs(args)
	err := cmd.Execute()
	return stdout.String(), stderr.String(), err
}

func TestParseSelection(t *testing.T) {
	cases := []struct {
		input string
		count int
		want  []int
	}{
		{"1", 3, []int{1}},
		{"1,3", 3, []int{1, 3}},
		{"1-3", 3, []int{1, 2, 3}},
		{"3,1,3", 3, []int{1, 3}},
		{"all", 3, []int{1, 2, 3}},
		{"*", 2, []int{1, 2}},
	}
	for _, tc := range cases {
		got, err := parseSelection(tc.input, tc.count)
		if err != nil {
			t.Fatalf("parseSelection(%q): %v", tc.input, err)
		}
		if len(got) != len(tc.want) {
			t.Fatalf("parseSelection(%q) = %v, want %v", tc.input, got, tc.want)
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Fatalf("parseSelection(%q) = %v, want %v", tc.input, got, tc.want)
			}
		}
	}

	for _, input := range []string{"", "q", "quit"} {
		if _, err := parseSelection(input, 3); !errors.Is(err, errWizardQuit) {
			t.Fatalf("parseSelection(%q) error = %v, want quit", input, err)
		}
	}
	for _, input := range []string{"0", "4", "3-1", "1-x", "abc", "1,,x"} {
		if _, err := parseSelection(input, 3); err == nil {
			t.Fatalf("parseSelection(%q) succeeded", input)
		}
	}
}

func TestWizardSelectsSessionAndRunsPipeline(t *testing.T) {
	p := newE2EPaths(t)
	stdout, stderr, err := runCommandWithInput("1\n",
		"wizard",
		"--provider", "claude-code",
		"--claude-dir", p.claudeDir,
		"--since", "all",
		"--claude-bin", p.claudeBin,
		"--out", p.renderOut,
		"--cache-dir", p.cacheDir,
		"--cursor", p.cursor,
		"--timeout", "30s",
		"--yes",
	)
	if err != nil {
		t.Fatalf("wizard: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	if !strings.Contains(stdout, "claude-code:claude-e2e") {
		t.Fatalf("stdout does not list the session:\n%s", stdout)
	}
	if !strings.Contains(stderr, "selected 1 session(s)") || !strings.Contains(stderr, "extracted=1") {
		t.Fatalf("stderr =\n%s", stderr)
	}
	if _, err := os.Stat(filepath.Join(p.renderOut, "index.json")); err != nil {
		t.Fatalf("wizard did not build an index: %v", err)
	}
	store, err := cache.Open(p.cacheDir)
	if err != nil {
		t.Fatalf("cache.Open: %v", err)
	}
	docs, _ := store.ListActiveDocs()
	if len(docs) != 1 || docs[0].Doc.Source != "claude-code" {
		t.Fatalf("active docs = %+v", docs)
	}
}

func TestWizardCancelsWithoutExtraction(t *testing.T) {
	p := newE2EPaths(t)
	_, stderr, err := runCommandWithInput("q\n",
		"wizard",
		"--provider", "claude-code",
		"--claude-dir", p.claudeDir,
		"--since", "all",
		"--claude-bin", p.claudeBin,
		"--out", p.renderOut,
		"--cache-dir", p.cacheDir,
		"--cursor", p.cursor,
	)
	if err != nil {
		t.Fatalf("wizard cancel: %v\nstderr:\n%s", err, stderr)
	}
	if !strings.Contains(stderr, "canceled") {
		t.Fatalf("stderr = %q", stderr)
	}
	if _, err := os.Stat(p.cacheDir); !os.IsNotExist(err) {
		t.Fatalf("canceled wizard touched the cache: %v", err)
	}
}

func TestWizardRejectsInvalidSelection(t *testing.T) {
	p := newE2EPaths(t)
	_, _, err := runCommandWithInput("99\n",
		"wizard",
		"--provider", "claude-code",
		"--claude-dir", p.claudeDir,
		"--since", "all",
		"--claude-bin", p.claudeBin,
		"--cache-dir", p.cacheDir,
		"--cursor", p.cursor,
	)
	if err == nil || !strings.Contains(err.Error(), "invalid session number") {
		t.Fatalf("error = %v", err)
	}
}

func TestWizardHandlesMissingInput(t *testing.T) {
	p := newE2EPaths(t)
	_, _, err := runCommandWithInput("",
		"wizard",
		"--provider", "claude-code",
		"--claude-dir", p.claudeDir,
		"--since", "all",
		"--claude-bin", p.claudeBin,
	)
	if err == nil || !strings.Contains(err.Error(), "no interactive input") {
		t.Fatalf("error = %v", err)
	}
}

func TestWizardRejectsSegmentsFlag(t *testing.T) {
	_, _, err := runCommandWithInput("1\n", "wizard", "--segments", "2")
	if err == nil || !strings.Contains(err.Error(), "wizard selects whole sessions") {
		t.Fatalf("error = %v", err)
	}
}
