package ingest

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestParseEmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.jsonl")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatalf("write empty file: %v", err)
	}

	for _, src := range []Source{NewClaudeCode(), NewCodex()} {
		t.Run(src.Name(), func(t *testing.T) {
			sess, err := src.Parse(FileRef{
				Source:    src.Name(),
				Path:      path,
				SessionID: "empty-session",
				Project:   "empty-project",
			}, ParseOptions{})
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if len(sess.Events) != 0 || sess.Stats.Events != 0 {
				t.Fatalf("events = %d, stats = %d", len(sess.Events), sess.Stats.Events)
			}
			if sess.File != path {
				t.Fatalf("file = %q, want %q", sess.File, path)
			}
			if sess.Project != "empty-project" || sess.ID != "empty-session" {
				t.Fatalf("session = %+v", sess)
			}
			if len(sess.Warnings) != 0 {
				t.Fatalf("warnings = %+v", sess.Warnings)
			}
		})
	}
}

func TestParseMissingFile(t *testing.T) {
	for _, src := range []Source{NewClaudeCode(), NewCodex()} {
		t.Run(src.Name(), func(t *testing.T) {
			_, err := src.Parse(FileRef{
				Source: src.Name(),
				Path:   filepath.Join(t.TempDir(), "missing.jsonl"),
			}, ParseOptions{})
			if err == nil {
				t.Fatal("expected an error for a missing file")
			}
		})
	}
}

func TestProjectFromCwd(t *testing.T) {
	tests := map[string]string{
		`D:\work\demo`:      "demo",
		`D:\work\demo\`:     "demo",
		`/home/user/space`:  "space",
		`/home/user/space/`: "space",
		`D:\`:               "",
		"":                  "",
	}
	for input, want := range tests {
		if got := projectFromCwd(input); got != want {
			t.Fatalf("projectFromCwd(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestExecFromText(t *testing.T) {
	t.Run("exit code zero", func(t *testing.T) {
		ex := execFromText("Process exited with code 0\nOutput:\nok")
		if ex == nil || !ex.Known || ex.ExitCode == nil || *ex.ExitCode != 0 {
			t.Fatalf("exec = %+v", ex)
		}
	})

	t.Run("exit code nonzero", func(t *testing.T) {
		ex := execFromText("Exit code: 2")
		if ex == nil || !ex.Known || ex.ExitCode == nil || *ex.ExitCode != 2 {
			t.Fatalf("exec = %+v", ex)
		}
	})

	t.Run("timeout", func(t *testing.T) {
		ex := execFromText("Command timed out after 30 seconds")
		if ex == nil || !ex.Known || !ex.TimedOut {
			t.Fatalf("exec = %+v", ex)
		}
	})

	t.Run("no signal", func(t *testing.T) {
		if ex := execFromText("tests failed\nFAIL"); ex != nil {
			t.Fatalf("exec = %+v, want nil", ex)
		}
	})
}

func TestParseTimestamp(t *testing.T) {
	got, err := parseTimestamp("2026-01-02T03:04:05.123Z")
	if err != nil {
		t.Fatalf("parseTimestamp: %v", err)
	}
	want := time.Date(2026, 1, 2, 3, 4, 5, 123000000, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("timestamp = %v, want %v", got, want)
	}

	zero, err := parseTimestamp("")
	if err != nil || !zero.IsZero() {
		t.Fatalf("empty timestamp = %v, %v", zero, err)
	}

	if _, err := parseTimestamp("not-a-time"); err == nil {
		t.Fatal("invalid timestamp must return an error")
	}
}
