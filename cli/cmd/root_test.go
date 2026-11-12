package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"devlog-learn/internal/version"
)

func TestVersionFlag(t *testing.T) {
	stdout, _, err := runCommand("--version")
	if err != nil {
		t.Fatalf("--version: %v", err)
	}
	want := "devlog-learn " + version.ToolVersion
	if !strings.Contains(stdout, want) {
		t.Fatalf("stdout = %q, want %q", stdout, want)
	}
}

// TestExtractRejectsUnsupportedProviderBeforeCreatingState guards against the
// regression where an invalid --provider created cache/cursor artifacts before
// failing.
func TestExtractRejectsUnsupportedProviderBeforeCreatingState(t *testing.T) {
	dir := t.TempDir()
	cacheDir := filepath.Join(dir, "cache")
	cursorPath := filepath.Join(dir, "cursor.json")

	_, _, err := runCommand(
		"extract",
		"--provider", "cursor",
		"--cache-dir", cacheDir,
		"--cursor", cursorPath,
	)
	if err == nil || !strings.Contains(err.Error(), "Cursor is not supported") {
		t.Fatalf("error = %v", err)
	}
	if _, statErr := os.Stat(cacheDir); !os.IsNotExist(statErr) {
		t.Fatalf("cache directory was created before validation: %v", statErr)
	}
	if _, statErr := os.Stat(cursorPath); !os.IsNotExist(statErr) {
		t.Fatalf("cursor file was created before validation: %v", statErr)
	}
}
