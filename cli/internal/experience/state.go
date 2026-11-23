package experience

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	// StateVersion covers the experiences.json sidecar shape.
	StateVersion = 1
	// StateFileName is the sidecar stored in the cache directory.
	StateFileName = "experiences.json"
)

// State is the human-owned experience metadata sidecar. Manual metadata always
// wins over the automatic kind suggestion.
type State struct {
	Version   int                 `json:"version"`
	UpdatedAt time.Time           `json:"updated_at"`
	Items     map[string]Metadata `json:"items"`
}

// NewState returns an empty state.
func NewState() *State {
	return &State{Version: StateVersion, Items: map[string]Metadata{}}
}

// StatePath returns the sidecar path for a cache directory.
func StatePath(cacheDir string) string {
	cacheDir = strings.TrimSpace(cacheDir)
	if cacheDir == "" {
		cacheDir = ".devlog-cache"
	}
	return filepath.Join(cacheDir, StateFileName)
}

// LoadState reads the sidecar. A missing file is an empty state, not an error.
func LoadState(path string) (*State, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return NewState(), nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return NewState(), nil
		}
		return NewState(), fmt.Errorf("read experience state %q: %w", path, err)
	}
	var state State
	if err := json.Unmarshal(data, &state); err != nil {
		return NewState(), fmt.Errorf("parse experience state %q: %w", path, err)
	}
	if state.Version != StateVersion {
		return NewState(), fmt.Errorf("unsupported experience state version %d in %q", state.Version, path)
	}
	if state.Items == nil {
		state.Items = map[string]Metadata{}
	}
	return &state, nil
}

// Save writes the sidecar atomically.
func (s *State) Save(path string, now time.Time) error {
	if s == nil {
		return nil
	}
	if s.Items == nil {
		s.Items = map[string]Metadata{}
	}
	s.Version = StateVersion
	s.UpdatedAt = now.UTC()
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("encode experience state: %w", err)
	}
	data = append(data, '\n')
	if err := writeAtomic(path, data); err != nil {
		return fmt.Errorf("write experience state %q: %w", path, err)
	}
	return nil
}

// Get returns metadata for an exact project name.
func (s *State) Get(project string) (Metadata, bool) {
	if s == nil || s.Items == nil {
		return Metadata{}, false
	}
	meta, ok := s.Items[strings.TrimSpace(project)]
	return meta, ok
}

// Set stores metadata for an exact project name.
func (s *State) Set(project string, meta Metadata) {
	if s.Items == nil {
		s.Items = map[string]Metadata{}
	}
	project = strings.TrimSpace(project)
	if project == "" {
		return
	}
	s.Items[project] = meta
}

// Unset removes metadata and reports whether an entry existed.
func (s *State) Unset(project string) bool {
	if s == nil || s.Items == nil {
		return false
	}
	project = strings.TrimSpace(project)
	if _, ok := s.Items[project]; !ok {
		return false
	}
	delete(s.Items, project)
	return true
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
