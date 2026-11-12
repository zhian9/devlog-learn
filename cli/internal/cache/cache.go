// Package cache persists extraction results, canonical documents, and redacted
// segments. It never decides whether to re-parse a file (that is the cursor's
// job); it only answers whether a segment's extraction can be reused.
package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"devlog-learn/internal/extract"
	"devlog-learn/internal/segment"
)

// ErrMiss marks a cache lookup that must be treated as a miss. Corrupt or
// tampered entries wrap this error rather than failing the run.
var ErrMiss = errors.New("cache miss")

const (
	StaleSegmentRemoved = "segment_removed"
	StaleSourceMissing  = "source_missing"
)

// Meta carries every value that participates in a cache key and must be
// reconfirmed when reading an entry.
type Meta struct {
	SchemaVersion    int
	ParserVersion    int
	SegmenterVersion int
	RedactionVersion int
	BuildVersion     int
	PromptVersion    string
	PromptHash       string
	RulesHash        string
	Provider         string
	Model            string
	CLIVersion       string
}

// Entry is the on-disk cache envelope.
type Entry struct {
	Key string `json:"key"`

	SchemaVersion    int    `json:"schema_version"`
	ParserVersion    int    `json:"parser_version"`
	SegmenterVersion int    `json:"segmenter_version"`
	RedactionVersion int    `json:"redaction_version"`
	BuildVersion     int    `json:"build_version"`
	PromptVersion    string `json:"prompt_version"`
	PromptHash       string `json:"prompt_hash"`
	RulesHash        string `json:"rules_hash"`

	Provider   string `json:"provider"`
	Model      string `json:"model"`
	CLIVersion string `json:"cli_version"`

	SegmentID   string `json:"segment_id"`
	SegmentHash string `json:"segment_hash"`
	DocID       string `json:"doc_id"`

	Doc       json.RawMessage `json:"doc"`
	DocSHA256 string          `json:"doc_sha256"`
	CreatedAt time.Time       `json:"created_at"`
}

// Key builds the versioned cache key. SegmentID participates because the
// cached Doc is normalized to one concrete segment identity: two sessions with
// byte-identical events still have different IDs, and their docs are not
// interchangeable. All components are length-prefixed so no two distinct
// inputs can produce the same byte stream.
func Key(meta Meta, seg segment.Segment) string {
	write := func(h interface{ Write([]byte) (int, error) }, value string) {
		_, _ = h.Write([]byte(strconv.Itoa(len(value))))
		_, _ = h.Write([]byte{0})
		_, _ = h.Write([]byte(value))
		_, _ = h.Write([]byte{0})
	}
	h := sha256.New()
	write(h, "dv")
	write(h, "schema="+strconv.Itoa(meta.SchemaVersion))
	write(h, "parser="+strconv.Itoa(meta.ParserVersion))
	write(h, "segmenter="+strconv.Itoa(meta.SegmenterVersion))
	write(h, "redact="+strconv.Itoa(meta.RedactionVersion))
	write(h, "build="+strconv.Itoa(meta.BuildVersion))
	write(h, "prompt="+meta.PromptHash)
	write(h, "rules="+meta.RulesHash)
	write(h, "provider="+meta.Provider)
	write(h, "model="+meta.Model)
	write(h, "cli="+meta.CLIVersion)
	write(h, "segment="+seg.Hash)
	write(h, "segment_id="+seg.ID)
	return hex.EncodeToString(h.Sum(nil))[:32]
}

// Store owns a cache directory tree.
type Store struct {
	Root string
}

// Open validates the cache root and returns a store. Directories are created
// lazily by writers.
func Open(root string) (*Store, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		root = ".devlog-cache"
	}
	root = filepath.Clean(root)
	if info, err := os.Stat(root); err == nil && !info.IsDir() {
		return nil, fmt.Errorf("cache path %q is not a directory", root)
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, fmt.Errorf("create cache directory %q: %w", root, err)
	}
	return &Store{Root: root}, nil
}

func (s *Store) entriesDir() string  { return filepath.Join(s.Root, "entries") }
func (s *Store) docsDir() string     { return filepath.Join(s.Root, "docs") }
func (s *Store) segmentsDir() string { return filepath.Join(s.Root, "segments") }

// RawDir is the conventional location for redacted raw model responses.
func (s *Store) RawDir() string { return filepath.Join(s.Root, "raw") }

// Save writes an extraction entry. Doc bytes are stored canonically (compact
// JSON) and fingerprinted so tampering is detectable on read.
func (s *Store) Save(key string, seg segment.Segment, meta Meta, doc *extract.Doc, now time.Time) error {
	if doc == nil {
		return errors.New("cannot cache a nil document")
	}
	docBytes, err := json.Marshal(doc)
	if err != nil {
		return fmt.Errorf("encode cached doc: %w", err)
	}
	sum := sha256.Sum256(docBytes)
	entry := Entry{
		Key:              key,
		SchemaVersion:    meta.SchemaVersion,
		ParserVersion:    meta.ParserVersion,
		SegmenterVersion: meta.SegmenterVersion,
		RedactionVersion: meta.RedactionVersion,
		BuildVersion:     meta.BuildVersion,
		PromptVersion:    meta.PromptVersion,
		PromptHash:       meta.PromptHash,
		RulesHash:        meta.RulesHash,
		Provider:         meta.Provider,
		Model:            meta.Model,
		CLIVersion:       meta.CLIVersion,
		SegmentID:        seg.ID,
		SegmentHash:      seg.Hash,
		DocID:            doc.ID,
		Doc:              docBytes,
		DocSHA256:        hex.EncodeToString(sum[:]),
		CreatedAt:        now.UTC(),
	}
	data, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		return fmt.Errorf("encode cache entry: %w", err)
	}
	data = append(data, '\n')
	if err := writeAtomic(filepath.Join(s.entriesDir(), key+".json"), data); err != nil {
		return fmt.Errorf("write cache entry %s: %w", key, err)
	}
	return nil
}

// Lookup returns the cached document only when the entry is structurally sound,
// matches the expected versions/key, and still belongs to the current segment.
func (s *Store) Lookup(key string, seg segment.Segment, meta Meta) (*extract.Doc, error) {
	path := filepath.Join(s.entriesDir(), key+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%w: no entry %s", ErrMiss, key)
		}
		return nil, fmt.Errorf("%w: read entry %s: %v", ErrMiss, key, err)
	}
	var entry Entry
	if err := json.Unmarshal(data, &entry); err != nil {
		return nil, fmt.Errorf("%w: entry %s is not valid JSON: %v", ErrMiss, key, err)
	}
	if entry.Key != key {
		return nil, fmt.Errorf("%w: entry %s key mismatch %q", ErrMiss, key, entry.Key)
	}
	if err := entry.matches(meta); err != nil {
		return nil, fmt.Errorf("%w: entry %s: %v", ErrMiss, key, err)
	}
	if entry.SegmentID != seg.ID || entry.SegmentHash != seg.Hash {
		return nil, fmt.Errorf("%w: entry %s belongs to segment %s/%s", ErrMiss, key, entry.SegmentID, entry.SegmentHash)
	}

	var doc extract.Doc
	if err := json.Unmarshal(entry.Doc, &doc); err != nil {
		return nil, fmt.Errorf("%w: entry %s doc is not valid JSON: %v", ErrMiss, key, err)
	}
	canonical, err := json.Marshal(&doc)
	if err != nil {
		return nil, fmt.Errorf("%w: entry %s doc cannot be re-encoded: %v", ErrMiss, key, err)
	}
	sum := sha256.Sum256(canonical)
	if hex.EncodeToString(sum[:]) != entry.DocSHA256 {
		return nil, fmt.Errorf("%w: entry %s doc checksum mismatch", ErrMiss, key)
	}
	if doc.SchemaVersion != extract.SchemaVersion {
		return nil, fmt.Errorf("%w: entry %s doc schema %d", ErrMiss, key, doc.SchemaVersion)
	}
	if doc.ID != entry.DocID || doc.ID != seg.ID {
		return nil, fmt.Errorf("%w: entry %s doc id %q does not match segment %q", ErrMiss, key, doc.ID, seg.ID)
	}
	if doc.Provenance.SegmentID != seg.ID || doc.Provenance.SegmentHash != seg.Hash {
		return nil, fmt.Errorf("%w: entry %s provenance %q/%q does not match segment %q/%q",
			ErrMiss, key, doc.Provenance.SegmentID, doc.Provenance.SegmentHash, seg.ID, seg.Hash)
	}
	if err := doc.Validate(); err != nil {
		return nil, fmt.Errorf("%w: entry %s doc is empty: %v", ErrMiss, key, err)
	}
	seqs := make(map[int]bool, len(seg.Events))
	for _, ev := range seg.Events {
		seqs[ev.Seq] = true
	}
	for _, ev := range doc.Evidence {
		if !seqs[ev.EventSeq] {
			return nil, fmt.Errorf("%w: entry %s evidence references seq %d outside segment", ErrMiss, key, ev.EventSeq)
		}
	}
	return &doc, nil
}

func (e Entry) matches(meta Meta) error {
	switch {
	case e.SchemaVersion != meta.SchemaVersion:
		return fmt.Errorf("schema version %d != %d", e.SchemaVersion, meta.SchemaVersion)
	case e.ParserVersion != meta.ParserVersion:
		return fmt.Errorf("parser version %d != %d", e.ParserVersion, meta.ParserVersion)
	case e.SegmenterVersion != meta.SegmenterVersion:
		return fmt.Errorf("segmenter version %d != %d", e.SegmenterVersion, meta.SegmenterVersion)
	case e.RedactionVersion != meta.RedactionVersion:
		return fmt.Errorf("redaction version %d != %d", e.RedactionVersion, meta.RedactionVersion)
	case e.BuildVersion != meta.BuildVersion:
		return fmt.Errorf("build version %d != %d", e.BuildVersion, meta.BuildVersion)
	case e.PromptVersion != meta.PromptVersion:
		return fmt.Errorf("prompt version %q != %q", e.PromptVersion, meta.PromptVersion)
	case e.PromptHash != meta.PromptHash:
		return errors.New("prompt hash mismatch")
	case e.RulesHash != meta.RulesHash:
		return errors.New("redaction rules hash mismatch")
	case e.Provider != meta.Provider:
		return fmt.Errorf("provider %q != %q", e.Provider, meta.Provider)
	case e.Model != meta.Model:
		return fmt.Errorf("model %q != %q", e.Model, meta.Model)
	case e.CLIVersion != meta.CLIVersion:
		return fmt.Errorf("CLI version %q != %q", e.CLIVersion, meta.CLIVersion)
	}
	return nil
}

// SaveDoc writes the canonical document for a segment.
func (s *Store) SaveDoc(doc *extract.Doc) error {
	if doc == nil || strings.TrimSpace(doc.ID) == "" {
		return errors.New("cannot save a doc without an id")
	}
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return fmt.Errorf("encode doc %s: %w", doc.ID, err)
	}
	data = append(data, '\n')
	if err := writeAtomic(filepath.Join(s.docsDir(), doc.ID+".json"), data); err != nil {
		return fmt.Errorf("write doc %s: %w", doc.ID, err)
	}
	return nil
}

// EnsureDoc writes a document only when its canonical file is absent, so a
// cache hit never overwrites human review edits made after extraction.
func (s *Store) EnsureDoc(doc *extract.Doc) (bool, error) {
	if doc == nil || strings.TrimSpace(doc.ID) == "" {
		return false, errors.New("cannot save a doc without an id")
	}
	path := filepath.Join(s.docsDir(), doc.ID+".json")
	if _, err := os.Stat(path); err == nil {
		return false, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	return true, s.SaveDoc(doc)
}

// LoadDoc reads one canonical document.
func (s *Store) LoadDoc(id string) (*extract.Doc, error) {
	data, err := os.ReadFile(filepath.Join(s.docsDir(), id+".json"))
	if err != nil {
		return nil, err
	}
	var doc extract.Doc
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse doc %s: %w", id, err)
	}
	return &doc, nil
}

// DocInfo pairs a document with its file path.
type DocInfo struct {
	ID   string
	Path string
	Doc  *extract.Doc
}

// ListDocs reads every canonical document. Corrupt files are reported as
// warnings and skipped so one bad file cannot block the run.
func (s *Store) ListDocs() ([]DocInfo, []string) {
	entries, err := os.ReadDir(s.docsDir())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, []string{fmt.Sprintf("read docs directory: %v", err)}
	}
	infos := make([]DocInfo, 0, len(entries))
	var warnings []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := filepath.Join(s.docsDir(), entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("read doc %s: %v", entry.Name(), err))
			continue
		}
		var doc extract.Doc
		if err := json.Unmarshal(data, &doc); err != nil {
			warnings = append(warnings, fmt.Sprintf("parse doc %s: %v", entry.Name(), err))
			continue
		}
		if strings.TrimSpace(doc.ID) == "" {
			warnings = append(warnings, fmt.Sprintf("doc %s has no id", entry.Name()))
			continue
		}
		infos = append(infos, DocInfo{ID: doc.ID, Path: path, Doc: &doc})
	}
	sort.Slice(infos, func(i, j int) bool { return infos[i].ID < infos[j].ID })
	return infos, warnings
}

// ListActiveDocs returns non-stale documents. Index builders must use this so
// stale documents stay on disk but never enter an index.
func (s *Store) ListActiveDocs() ([]DocInfo, []string) {
	docs, warnings := s.ListDocs()
	out := docs[:0]
	for _, info := range docs {
		if info.Doc.Lifecycle == extract.LifecycleStale {
			continue
		}
		out = append(out, info)
	}
	return out, warnings
}

// SaveSegment stores a redacted segment for provenance and later tooling.
func (s *Store) SaveSegment(seg segment.Segment) error {
	if strings.TrimSpace(seg.ID) == "" {
		return errors.New("cannot save a segment without an id")
	}
	data, err := json.MarshalIndent(seg, "", "  ")
	if err != nil {
		return fmt.Errorf("encode segment %s: %w", seg.ID, err)
	}
	data = append(data, '\n')
	if err := writeAtomic(filepath.Join(s.segmentsDir(), seg.ID+".json"), data); err != nil {
		return fmt.Errorf("write segment %s: %w", seg.ID, err)
	}
	return nil
}

// LoadSegment reads a stored segment.
func (s *Store) LoadSegment(id string) (segment.Segment, error) {
	data, err := os.ReadFile(filepath.Join(s.segmentsDir(), id+".json"))
	if err != nil {
		return segment.Segment{}, err
	}
	var seg segment.Segment
	if err := json.Unmarshal(data, &seg); err != nil {
		return segment.Segment{}, fmt.Errorf("parse segment %s: %w", id, err)
	}
	return seg, nil
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
