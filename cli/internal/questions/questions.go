// Package questions tracks human resolution state for Doc open questions.
//
// Open questions are produced by the extractor on every re-extraction. Human
// state must therefore live outside Doc content and be keyed by a stable
// identity derived from the Doc ID and the normalized question text. A sidecar
// file keeps Doc JSON and the RAG contract untouched.
package questions

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"devlog-learn/internal/cache"
	"devlog-learn/internal/extract"
	"devlog-learn/internal/render"
)

const (
	// Version covers the sidecar JSON shape.
	Version = 1
	// StatusResolved is the only persistent status. Unresolved questions have
	// no entry at all.
	StatusResolved = "resolved"
	// StatusOpen and StatusAll are filters, not persisted values.
	StatusOpen = "open"
	StatusAll  = "all"
	// FileName is the sidecar file inside the cache directory.
	FileName = "questions.json"
)

// Item is one persisted resolution.
type Item struct {
	DocID      string    `json:"doc_id"`
	Question   string    `json:"question"`
	Status     string    `json:"status"`
	Note       string    `json:"note,omitempty"`
	ResolvedAt time.Time `json:"resolved_at"`
}

// State is the complete sidecar document.
type State struct {
	Version   int             `json:"version"`
	UpdatedAt time.Time       `json:"updated_at"`
	Items     map[string]Item `json:"items"`
}

// Entry is one open question joined with its resolution state for display.
type Entry struct {
	ID         string     `json:"id"`
	DocID      string     `json:"doc_id"`
	DocTitle   string     `json:"doc_title"`
	Project    string     `json:"project"`
	Question   string     `json:"question"`
	Status     string     `json:"status"`
	Note       string     `json:"note,omitempty"`
	ResolvedAt *time.Time `json:"resolved_at,omitempty"`
	OccurredAt time.Time  `json:"occurred_at"`
}

// Filter narrows Collect. Empty values mean no restriction; Status defaults to
// open questions.
type Filter struct {
	Project string
	Review  string
	DocID   string
	Status  string
}

// New returns an empty state.
func New() *State {
	return &State{Version: Version, Items: map[string]Item{}}
}

// Path returns the sidecar path for a cache directory.
func Path(cacheDir string) string {
	cacheDir = strings.TrimSpace(cacheDir)
	if cacheDir == "" {
		cacheDir = ".devlog-cache"
	}
	return filepath.Join(cacheDir, FileName)
}

// NewID derives a stable question ID. Formatting-only differences (case and
// whitespace) map to the same ID; wording changes are intentionally treated as
// a new question.
func NewID(docID, question string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(docID) + "\x00" + identity(question)))
	return "q_" + hex.EncodeToString(sum[:])[:12]
}

// Load reads the sidecar. A missing file is an empty state, not an error.
func Load(path string) (*State, error) {
	state := New()
	path = strings.TrimSpace(path)
	if path == "" {
		return state, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return state, nil
		}
		return state, fmt.Errorf("read questions state %q: %w", path, err)
	}
	var loaded State
	if err := json.Unmarshal(data, &loaded); err != nil {
		return New(), fmt.Errorf("parse questions state %q: %w", path, err)
	}
	if loaded.Version != Version {
		return New(), fmt.Errorf("unsupported questions state version %d in %q", loaded.Version, path)
	}
	if loaded.Items == nil {
		loaded.Items = map[string]Item{}
	}
	return &loaded, nil
}

// Save writes the sidecar atomically.
func (s *State) Save(path string, now time.Time) error {
	if s == nil {
		return nil
	}
	if s.Items == nil {
		s.Items = map[string]Item{}
	}
	s.Version = Version
	s.UpdatedAt = now.UTC()
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("encode questions state: %w", err)
	}
	data = append(data, '\n')
	if err := writeAtomic(path, data); err != nil {
		return fmt.Errorf("write questions state %q: %w", path, err)
	}
	return nil
}

// Resolve marks one question resolved. The entry must come from Collect so the
// Doc ID and display text are recorded alongside the identity.
func (s *State) Resolve(entry Entry, note string, now time.Time) {
	if s.Items == nil {
		s.Items = map[string]Item{}
	}
	s.Items[entry.ID] = Item{
		DocID:      entry.DocID,
		Question:   entry.Question,
		Status:     StatusResolved,
		Note:       strings.TrimSpace(note),
		ResolvedAt: now.UTC(),
	}
}

// Reopen removes a resolution. It reports whether an entry existed.
func (s *State) Reopen(id string) bool {
	if s == nil || s.Items == nil {
		return false
	}
	if _, ok := s.Items[id]; !ok {
		return false
	}
	delete(s.Items, id)
	return true
}

// ResolvedIDs returns a set of question IDs currently marked resolved. Digest
// uses it to omit settled questions from agent-facing memory.
func (s *State) ResolvedIDs() map[string]bool {
	out := map[string]bool{}
	if s == nil {
		return out
	}
	for id, item := range s.Items {
		if item.Status == StatusResolved {
			out[id] = true
		}
	}
	return out
}

// Collect joins active Docs with the resolution state. Stale and malformed Docs
// are excluded, matching every other read-only view.
func Collect(infos []cache.DocInfo, state *State, filter Filter) []Entry {
	status := normalizeStatus(filter.Status)
	seen := map[string]bool{}
	var out []Entry
	for _, info := range infos {
		doc := info.Doc
		if doc == nil || doc.Lifecycle == extract.LifecycleStale {
			continue
		}
		if !matchesString(filter.DocID, doc.ID) ||
			!matchesProject(filter.Project, doc.Project) ||
			!matchesReview(filter.Review, doc.ReviewStatus) {
			continue
		}
		for _, raw := range doc.OpenQuestions {
			question := oneLine(raw)
			if question == "" {
				continue
			}
			id := NewID(doc.ID, question)
			if seen[id] {
				continue
			}
			seen[id] = true
			entry := Entry{
				ID:         id,
				DocID:      doc.ID,
				DocTitle:   doc.Title,
				Project:    doc.Project,
				Question:   question,
				Status:     StatusOpen,
				OccurredAt: doc.OccurredAt,
			}
			if item, ok := stateItem(state, id); ok && item.Status == StatusResolved {
				entry.Status = StatusResolved
				entry.Note = item.Note
				resolvedAt := item.ResolvedAt
				entry.ResolvedAt = &resolvedAt
			}
			if !matchesStatus(status, entry.Status) {
				continue
			}
			out = append(out, entry)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Project != b.Project {
			return a.Project < b.Project
		}
		if a.DocID != b.DocID {
			return a.DocID < b.DocID
		}
		if !a.OccurredAt.Equal(b.OccurredAt) {
			return a.OccurredAt.After(b.OccurredAt)
		}
		return a.ID < b.ID
	})
	return out
}

func stateItem(state *State, id string) (Item, bool) {
	if state == nil || state.Items == nil {
		return Item{}, false
	}
	item, ok := state.Items[id]
	return item, ok
}

func normalizeStatus(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", StatusOpen, "unresolved", "pending":
		return StatusOpen
	case StatusResolved:
		return StatusResolved
	case StatusAll, "*", "any":
		return StatusAll
	default:
		return StatusOpen
	}
}

// ValidStatus reports whether a --status value is accepted by the CLI.
func ValidStatus(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", StatusOpen, "unresolved", "pending", StatusResolved, StatusAll, "*", "any":
		return true
	}
	return false
}

func matchesStatus(filter, value string) bool {
	return filter == StatusAll || filter == value
}

func matchesString(filter, value string) bool {
	filter = strings.TrimSpace(filter)
	return filter == "" || strings.EqualFold(filter, strings.TrimSpace(value))
}

func matchesProject(filter, project string) bool {
	filter = strings.TrimSpace(filter)
	if filter == "" || filter == "*" || strings.EqualFold(filter, "all") {
		return true
	}
	return strings.EqualFold(filter, strings.TrimSpace(project)) ||
		strings.EqualFold(filter, render.SafeProject(project))
}

func matchesReview(filter, review string) bool {
	filter = strings.ToLower(strings.TrimSpace(filter))
	if filter == "" || filter == "any" || filter == "*" {
		return true
	}
	return filter == strings.ToLower(strings.TrimSpace(review))
}

func identity(question string) string {
	return strings.ToLower(oneLine(question))
}

func oneLine(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
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
