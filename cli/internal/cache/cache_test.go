package cache

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"devlog-learn/internal/extract"
	"devlog-learn/internal/ingest"
	"devlog-learn/internal/segment"
	"devlog-learn/internal/version"
)

var cacheNow = time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)

func testMeta() Meta {
	return Meta{
		SchemaVersion:    extract.SchemaVersion,
		ParserVersion:    version.ParserVersion,
		SegmenterVersion: version.SegmenterVersion,
		RedactionVersion: version.RedactionVersion,
		BuildVersion:     version.BuildVersion,
		PromptVersion:    "extract-v1",
		PromptHash:       "prompt-hash",
		RulesHash:        "rules-hash",
		Provider:         "claude-code",
		Model:            "fake-model",
		CLIVersion:       "fake-cli 1.0",
	}
}

func testSegment() segment.Segment {
	return segment.Segment{
		ID: "seg-1", Hash: "segment-hash", Source: "claude-code", SessionID: "session-1",
		Project: "demo", Title: "Fix build", Start: cacheNow, End: cacheNow.Add(time.Minute),
		Events: []ingest.Event{
			{Kind: ingest.KindUserText, Role: "user", Text: "Fix build", Seq: 1},
			{Kind: ingest.KindAssistantText, Role: "assistant", Text: "Fixed", Seq: 2},
		},
	}
}

func testDoc(seg segment.Segment) *extract.Doc {
	return &extract.Doc{
		SchemaVersion: extract.SchemaVersion,
		ID:            seg.ID,
		Title:         "Fix build",
		Summary:       "summary",
		Lifecycle:     extract.LifecycleActive,
		Evidence: []extract.Evidence{
			{EventSeq: 1, Kind: "user_report", Quote: "Fix build", Verified: true},
		},
		Provenance: extract.Provenance{SegmentID: seg.ID, SegmentHash: seg.Hash},
	}
}

func openStore(t *testing.T) *Store {
	t.Helper()
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return store
}

func TestKeyIsSensitiveToEveryComponent(t *testing.T) {
	base := testMeta()
	seg := testSegment()
	initial := Key(base, seg)
	cases := map[string]func(*Meta){
		"schema":    func(m *Meta) { m.SchemaVersion++ },
		"parser":    func(m *Meta) { m.ParserVersion++ },
		"segmenter": func(m *Meta) { m.SegmenterVersion++ },
		"redaction": func(m *Meta) { m.RedactionVersion++ },
		"build":     func(m *Meta) { m.BuildVersion++ },
		"prompt":    func(m *Meta) { m.PromptHash = "other" },
		"rules":     func(m *Meta) { m.RulesHash = "other" },
		"provider":  func(m *Meta) { m.Provider = "codex" },
		"model":     func(m *Meta) { m.Model = "other" },
		"cli":       func(m *Meta) { m.CLIVersion = "other" },
	}
	for name, mutate := range cases {
		meta := base
		mutate(&meta)
		if got := Key(meta, seg); got == initial {
			t.Fatalf("changing %s did not change the key", name)
		}
	}
	otherHash := seg
	otherHash.Hash = "other"
	if Key(base, otherHash) == initial {
		t.Fatal("changing the segment hash did not change the key")
	}
	otherID := seg
	otherID.ID = "other"
	if Key(base, otherID) == initial {
		t.Fatal("changing the segment id did not change the key")
	}
	if len(initial) != 32 {
		t.Fatalf("key length = %d", len(initial))
	}
}

func TestSaveLookupRoundTrip(t *testing.T) {
	store := openStore(t)
	seg := testSegment()
	meta := testMeta()
	doc := testDoc(seg)
	key := Key(meta, seg)

	if err := store.Save(key, seg, meta, doc, cacheNow); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := store.Lookup(key, seg, meta)
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if got.ID != seg.ID || got.Title != doc.Title {
		t.Fatalf("doc = %+v", got)
	}
	if _, err := os.Stat(filepath.Join(store.Root, "entries", key+".json")); err != nil {
		t.Fatalf("entry file: %v", err)
	}
}

func TestLookupMissOnCorruption(t *testing.T) {
	seg := testSegment()
	meta := testMeta()
	key := Key(meta, seg)

	cases := []struct {
		name   string
		mutate func(t *testing.T, store *Store, path string)
	}{
		{
			name: "invalid json",
			mutate: func(t *testing.T, _ *Store, path string) {
				writeFile(t, path, []byte("{not json"))
			},
		},
		{
			name: "doc checksum",
			mutate: func(t *testing.T, _ *Store, path string) {
				entry := readEntry(t, path)
				entry.DocSHA256 = strings.Repeat("0", 64)
				writeEntry(t, path, entry)
			},
		},
		{
			name: "doc content",
			mutate: func(t *testing.T, _ *Store, path string) {
				entry := readEntry(t, path)
				field := "Fix build"
				entry.Doc = json.RawMessage(strings.Replace(string(entry.Doc), field, "Tampered!", 1))
				writeEntry(t, path, entry)
			},
		},
		{
			name: "key",
			mutate: func(t *testing.T, _ *Store, path string) {
				entry := readEntry(t, path)
				entry.Key = "different-key"
				writeEntry(t, path, entry)
			},
		},
		{
			name: "segment hash",
			mutate: func(t *testing.T, _ *Store, path string) {
				entry := readEntry(t, path)
				entry.SegmentHash = "other"
				writeEntry(t, path, entry)
			},
		},
		{
			name: "schema version",
			mutate: func(t *testing.T, _ *Store, path string) {
				entry := readEntry(t, path)
				entry.SchemaVersion++
				writeEntry(t, path, entry)
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := openStore(t)
			if err := store.Save(key, seg, meta, testDoc(seg), cacheNow); err != nil {
				t.Fatalf("Save: %v", err)
			}
			path := filepath.Join(store.Root, "entries", key+".json")
			tc.mutate(t, store, path)
			if _, err := store.Lookup(key, seg, meta); !errors.Is(err, ErrMiss) {
				t.Fatalf("error = %v, want ErrMiss", err)
			}
		})
	}
}

func TestLookupRejectsForeignEvidenceAndEmptyDoc(t *testing.T) {
	store := openStore(t)
	seg := testSegment()
	meta := testMeta()
	key := Key(meta, seg)

	foreign := testDoc(seg)
	foreign.Evidence = []extract.Evidence{{EventSeq: 99, Quote: "nope"}}
	if err := store.Save(key, seg, meta, foreign, cacheNow); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, err := store.Lookup(key, seg, meta); !errors.Is(err, ErrMiss) {
		t.Fatalf("foreign evidence error = %v, want ErrMiss", err)
	}

	empty := &extract.Doc{
		SchemaVersion: extract.SchemaVersion,
		ID:            seg.ID,
		Provenance:    extract.Provenance{SegmentID: seg.ID, SegmentHash: seg.Hash},
	}
	if err := store.Save(key, seg, meta, empty, cacheNow); err != nil {
		t.Fatalf("Save empty: %v", err)
	}
	if _, err := store.Lookup(key, seg, meta); !errors.Is(err, ErrMiss) {
		t.Fatalf("empty doc error = %v, want ErrMiss", err)
	}
}

func TestDocsListAndStaleExclusion(t *testing.T) {
	store := openStore(t)
	seg := testSegment()
	active := testDoc(seg)
	active.ID = "active"
	stale := testDoc(seg)
	stale.ID = "stale"
	stale.Lifecycle = extract.LifecycleStale
	stale.StaleReason = StaleSegmentRemoved

	if err := store.SaveDoc(active); err != nil {
		t.Fatalf("SaveDoc active: %v", err)
	}
	if err := store.SaveDoc(stale); err != nil {
		t.Fatalf("SaveDoc stale: %v", err)
	}

	all, warnings := store.ListDocs()
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v", warnings)
	}
	if len(all) != 2 {
		t.Fatalf("docs = %d, want 2", len(all))
	}
	onlyActive, _ := store.ListActiveDocs()
	if len(onlyActive) != 1 || onlyActive[0].ID != "active" {
		t.Fatalf("active docs = %+v", onlyActive)
	}
}

func TestListDocsSkipsCorruptFiles(t *testing.T) {
	store := openStore(t)
	if err := store.SaveDoc(testDoc(testSegment())); err != nil {
		t.Fatalf("SaveDoc: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(store.Root, "docs"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeFile(t, filepath.Join(store.Root, "docs", "broken.json"), []byte("{bad"))
	docs, warnings := store.ListDocs()
	if len(docs) != 1 {
		t.Fatalf("docs = %d, want 1", len(docs))
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "broken.json") {
		t.Fatalf("warnings = %v", warnings)
	}
}

func TestEnsureDocDoesNotOverwrite(t *testing.T) {
	store := openStore(t)
	doc := testDoc(testSegment())
	doc.Title = "original"
	if err := store.SaveDoc(doc); err != nil {
		t.Fatalf("SaveDoc: %v", err)
	}
	replacement := testDoc(testSegment())
	replacement.Title = "replacement"
	written, err := store.EnsureDoc(replacement)
	if err != nil {
		t.Fatalf("EnsureDoc: %v", err)
	}
	if written {
		t.Fatal("EnsureDoc must not overwrite an existing doc")
	}
	got, err := store.LoadDoc(doc.ID)
	if err != nil {
		t.Fatalf("LoadDoc: %v", err)
	}
	if got.Title != "original" {
		t.Fatalf("title = %q", got.Title)
	}
}

func TestSegmentRoundTrip(t *testing.T) {
	store := openStore(t)
	seg := testSegment()
	if err := store.SaveSegment(seg); err != nil {
		t.Fatalf("SaveSegment: %v", err)
	}
	got, err := store.LoadSegment(seg.ID)
	if err != nil {
		t.Fatalf("LoadSegment: %v", err)
	}
	if got.ID != seg.ID || got.Hash != seg.Hash || len(got.Events) != len(seg.Events) {
		t.Fatalf("segment = %+v", got)
	}
}

func readEntry(t *testing.T, path string) Entry {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read entry: %v", err)
	}
	var entry Entry
	if err := json.Unmarshal(data, &entry); err != nil {
		t.Fatalf("decode entry: %v", err)
	}
	return entry
}

func writeEntry(t *testing.T, path string, entry Entry) {
	t.Helper()
	data, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		t.Fatalf("encode entry: %v", err)
	}
	writeFile(t, path, data)
}

func writeFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
