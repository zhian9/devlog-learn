// Package cursor stores per-file fingerprints for incremental scanning.
//
// SHA-256 is authoritative: size and mtime are recorded for diagnostics but a
// skip decision never depends on them. Parser and segmenter versions gate reuse
// so behavioural changes force a full re-parse and re-segmentation.
package cursor

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"devlog-learn/internal/version"
)

// SchemaVersion covers the cursor JSON shape itself.
const SchemaVersion = 1

const (
	StatusOK    = "ok"
	StatusError = "error"
)

// Fingerprint is the last successful (or failed) observation of one file.
type Fingerprint struct {
	Source           string    `json:"source,omitempty"`
	Size             int64     `json:"size"`
	ModTimeNS        int64     `json:"mod_time_ns"`
	SHA256           string    `json:"sha256"`
	ParserVersion    int       `json:"parser_version"`
	SegmenterVersion int       `json:"segmenter_version"`
	Status           string    `json:"status"`
	LastRun          time.Time `json:"last_run"`
	LastError        string    `json:"last_error,omitempty"`
	SegmentIDs       []string  `json:"segment_ids,omitempty"`
}

// State is the complete cursor document.
type State struct {
	Version          int                    `json:"version"`
	ParserVersion    int                    `json:"parser_version"`
	SegmenterVersion int                    `json:"segmenter_version"`
	UpdatedAt        time.Time              `json:"updated_at"`
	Files            map[string]Fingerprint `json:"files"`
}

// NewState returns an empty cursor for the current parser/segmenter versions.
func NewState() *State {
	return &State{
		Version:          SchemaVersion,
		ParserVersion:    version.ParserVersion,
		SegmenterVersion: version.SegmenterVersion,
		Files:            map[string]Fingerprint{},
	}
}

// Load reads a cursor file. A missing file is not an error. A corrupt or
// unsupported file returns an empty state plus an error so the caller can warn
// and continue safely.
func Load(path string) (*State, error) {
	state := NewState()
	if strings.TrimSpace(path) == "" {
		return state, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return state, nil
		}
		return state, fmt.Errorf("read cursor %q: %w", path, err)
	}
	var loaded State
	dec := json.NewDecoder(strings.NewReader(string(data)))
	if err := dec.Decode(&loaded); err != nil {
		return NewState(), fmt.Errorf("parse cursor %q: %w", path, err)
	}
	if loaded.Version != SchemaVersion {
		return NewState(), fmt.Errorf("unsupported cursor schema version %d in %q", loaded.Version, path)
	}
	if loaded.Files == nil {
		loaded.Files = map[string]Fingerprint{}
	}
	return &loaded, nil
}

// Save writes the cursor atomically.
func (s *State) Save(path string, now time.Time) error {
	if s == nil {
		return nil
	}
	if strings.TrimSpace(path) == "" {
		return nil
	}
	if s.Files == nil {
		s.Files = map[string]Fingerprint{}
	}
	s.Version = SchemaVersion
	s.ParserVersion = version.ParserVersion
	s.SegmenterVersion = version.SegmenterVersion
	s.UpdatedAt = now.UTC()

	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("encode cursor: %w", err)
	}
	data = append(data, '\n')
	if err := writeAtomic(path, data); err != nil {
		return fmt.Errorf("write cursor %q: %w", path, err)
	}
	return nil
}

// VersionsMatch reports whether the whole cursor was produced by the current
// parser and segmenter. When false, every file must be treated as changed.
func (s *State) VersionsMatch() bool {
	return s != nil &&
		s.ParserVersion == version.ParserVersion &&
		s.SegmenterVersion == version.SegmenterVersion
}

// CanSkip reports whether the file can be skipped. SHA-256 and both versions
// must match, and the previous observation must have succeeded.
func (s *State) CanSkip(key, sha string) bool {
	if s == nil || !s.VersionsMatch() || sha == "" {
		return false
	}
	fp, ok := s.Files[key]
	if !ok || fp.Status != StatusOK {
		return false
	}
	if fp.ParserVersion != version.ParserVersion || fp.SegmenterVersion != version.SegmenterVersion {
		return false
	}
	return fp.SHA256 == sha
}

// MarkOK records a successful parse (and possibly zero segments).
func (s *State) MarkOK(key string, fp Fingerprint, segmentIDs []string, now time.Time) {
	if s == nil {
		return
	}
	if s.Files == nil {
		s.Files = map[string]Fingerprint{}
	}
	fp.Status = StatusOK
	fp.LastRun = now.UTC()
	fp.LastError = ""
	fp.ParserVersion = version.ParserVersion
	fp.SegmenterVersion = version.SegmenterVersion
	fp.SegmentIDs = append([]string(nil), segmentIDs...)
	s.Files[key] = fp
}

// MarkError records a failed attempt. Error entries are never skipped, so the
// next run retries the file even when its bytes are unchanged.
func (s *State) MarkError(key string, fp Fingerprint, cause error, now time.Time) {
	if s == nil {
		return
	}
	if s.Files == nil {
		s.Files = map[string]Fingerprint{}
	}
	fp.Status = StatusError
	fp.LastRun = now.UTC()
	fp.ParserVersion = version.ParserVersion
	fp.SegmenterVersion = version.SegmenterVersion
	if cause != nil {
		fp.LastError = cause.Error()
	}
	s.Files[key] = fp
}

// SortedKeys returns fingerprint keys in deterministic order.
func (s *State) SortedKeys() []string {
	if s == nil {
		return nil
	}
	keys := make([]string, 0, len(s.Files))
	for key := range s.Files {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// HashFile streams the file through SHA-256 and returns the digest, byte count
// and modification time.
func HashFile(path string) (string, int64, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, 0, err
	}
	defer f.Close()

	h := sha256.New()
	size, err := io.Copy(h, f)
	if err != nil {
		return "", 0, 0, err
	}
	info, err := f.Stat()
	if err != nil {
		return "", 0, 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), size, info.ModTime().UnixNano(), nil
}

// NormalizePath makes a path a stable cursor key. Windows path comparisons are
// case-insensitive, so keys are lower-cased there.
func NormalizePath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = filepath.Clean(path)
	}
	abs = filepath.Clean(abs)
	if runtime.GOOS == "windows" {
		abs = strings.ToLower(abs)
	}
	return abs
}

func writeAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	cleanup := func() {
		_ = f.Close()
		_ = os.Remove(tmp)
	}
	if _, err := f.Write(data); err != nil {
		cleanup()
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}
