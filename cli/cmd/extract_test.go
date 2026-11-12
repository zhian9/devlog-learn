package main

import (
	"bytes"
	"strings"
	"testing"
)

func runExtractCommand(args ...string) (string, string, error) {
	var stdout, stderr bytes.Buffer
	cmd := newRootCmd()
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs(append([]string{"extract"}, args...))
	err := cmd.Execute()
	return stdout.String(), stderr.String(), err
}

func TestExtractCommandRejectsCursor(t *testing.T) {
	_, _, err := runExtractCommand("--provider", "cursor", "--since", "all")
	if err == nil || !strings.Contains(err.Error(), "Cursor is not supported") {
		t.Fatalf("error = %v", err)
	}
}

func TestExtractCommandValidatesOptions(t *testing.T) {
	if _, _, err := runExtractCommand("--lang", "fr"); err == nil || !strings.Contains(err.Error(), "--lang") {
		t.Fatalf("lang error = %v", err)
	}
	if _, _, err := runExtractCommand("--segments", "-1"); err == nil || !strings.Contains(err.Error(), "--segments") {
		t.Fatalf("segments error = %v", err)
	}
	if _, _, err := runExtractCommand("--concurrency", "0"); err == nil || !strings.Contains(err.Error(), "--concurrency") {
		t.Fatalf("concurrency error = %v", err)
	}
}
