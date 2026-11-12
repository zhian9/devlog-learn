package cursor

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"devlog-learn/internal/version"
)

func TestHashFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	content := "hello\nworld\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	sha, size, _, err := HashFile(path)
	if err != nil {
		t.Fatalf("HashFile: %v", err)
	}
	if size != int64(len(content)) {
		t.Fatalf("size = %d, want %d", size, len(content))
	}
	again, _, _, err := HashFile(path)
	if err != nil || again != sha {
		t.Fatalf("hash not deterministic: %q vs %q (%v)", sha, again, err)
	}
	if len(sha) != 64 {
		t.Fatalf("sha length = %d", len(sha))
	}
}

func TestSaveLoadAndCanSkip(t *testing.T) {
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	path := filepath.Join(t.TempDir(), "cursor.json")
	state := NewState()
	fp := Fingerprint{Source: "claude-code", Size: 10, ModTimeNS: 123, SHA256: "abc"}
	state.MarkOK("/tmp/a.jsonl", fp, []string{"seg1"}, now)
	if err := state.Save(path, now); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !loaded.VersionsMatch() {
		t.Fatal("versions should match")
	}
	if !loaded.CanSkip("/tmp/a.jsonl", "abc") {
		t.Fatal("expected a skip")
	}
	if loaded.CanSkip("/tmp/a.jsonl", "different") {
		t.Fatal("different hash must not be skipped")
	}
	if loaded.CanSkip("/tmp/missing.jsonl", "abc") {
		t.Fatal("unknown file must not be skipped")
	}
	if got := loaded.Files["/tmp/a.jsonl"].SegmentIDs; len(got) != 1 || got[0] != "seg1" {
		t.Fatalf("segment ids = %v", got)
	}
}

func TestCanSkipRejectsVersionAndError(t *testing.T) {
	now := time.Now()
	state := NewState()
	fp := Fingerprint{SHA256: "abc"}
	state.MarkOK("k", fp, nil, now)

	entry := state.Files["k"]
	entry.ParserVersion = version.ParserVersion - 1
	state.Files["k"] = entry
	if state.CanSkip("k", "abc") {
		t.Fatal("per-file parser version mismatch must force a reparse")
	}

	state = NewState()
	state.Files["k"] = Fingerprint{
		SHA256:           "abc",
		ParserVersion:    version.ParserVersion,
		SegmenterVersion: version.SegmenterVersion,
		Status:           StatusOK,
	}
	state.ParserVersion = version.ParserVersion - 1
	if state.CanSkip("k", "abc") {
		t.Fatal("top-level parser version mismatch must force a full reparse")
	}

	state = NewState()
	state.Files["k"] = Fingerprint{
		SHA256:           "abc",
		ParserVersion:    version.ParserVersion,
		SegmenterVersion: version.SegmenterVersion,
		Status:           StatusOK,
	}
	state.SegmenterVersion = version.SegmenterVersion - 1
	if state.CanSkip("k", "abc") {
		t.Fatal("top-level segmenter version mismatch must force a full reparse")
	}

	state = NewState()
	state.MarkError("k", Fingerprint{SHA256: "abc"}, errors.New("boom"), now)
	if state.CanSkip("k", "abc") {
		t.Fatal("error entries must be retried")
	}
	if state.Files["k"].LastError != "boom" {
		t.Fatalf("last error = %q", state.Files["k"].LastError)
	}
}

func TestLoadCorruptCursorRecovers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cursor.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	state, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "parse cursor") {
		t.Fatalf("error = %v", err)
	}
	if state == nil || !state.VersionsMatch() || len(state.Files) != 0 {
		t.Fatalf("state = %+v", state)
	}
	// The recovered state must be usable for a new run.
	state.MarkOK("k", Fingerprint{SHA256: "abc"}, nil, time.Now())
	if err := state.Save(path, time.Now()); err != nil {
		t.Fatalf("Save over corrupt cursor: %v", err)
	}
	reloaded, err := Load(path)
	if err != nil || !reloaded.CanSkip("k", "abc") {
		t.Fatalf("reload = %+v, err = %v", reloaded, err)
	}
}

func TestLoadMissingCursorIsEmpty(t *testing.T) {
	state, err := Load(filepath.Join(t.TempDir(), "absent.json"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if state == nil || len(state.Files) != 0 {
		t.Fatalf("state = %+v", state)
	}
}

func TestLoadRejectsUnsupportedSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cursor.json")
	if err := os.WriteFile(path, []byte(`{"version":99,"files":{}}`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	state, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "unsupported cursor schema") {
		t.Fatalf("error = %v", err)
	}
	if state == nil || len(state.Files) != 0 {
		t.Fatalf("state = %+v", state)
	}
}

func TestNormalizePathIsAbsolute(t *testing.T) {
	got := NormalizePath(".")
	want, err := filepath.Abs(".")
	if err != nil {
		t.Fatalf("Abs: %v", err)
	}
	want = filepath.Clean(want)
	if runtime.GOOS == "windows" {
		want = strings.ToLower(want)
	}
	if got != want {
		t.Fatalf("NormalizePath = %q, want %q", got, want)
	}
}
