package ingest

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// FileRef is one discovered session file.
type FileRef struct {
	Source    string    `json:"source"`
	Path      string    `json:"path"`
	Project   string    `json:"project,omitempty"`
	SessionID string    `json:"session_id,omitempty"`
	ModTime   time.Time `json:"mod_time"`
	Size      int64     `json:"size"`
}

// DiscoverOptions controls file discovery. A zero Since means no cutoff.
type DiscoverOptions struct {
	Since           time.Time
	IncludeSubagent bool
}

// ParseOptions controls provider parsing.
type ParseOptions struct {
	IncludeSubagent bool
	MaxLineBytes    int
}

// Source discovers and parses one provider's session logs.
//
// Only claude-code and codex are supported in this version. The interface is
// intentionally generic so another provider can be added later without
// changing callers.
type Source interface {
	Name() string
	DefaultRoot() (string, error)
	Discover(root string, opt DiscoverOptions) ([]FileRef, error)
	Parse(ref FileRef, opt ParseOptions) (*Session, error)
}

// NewClaudeCode returns the Claude Code source.
func NewClaudeCode() Source { return claudeSource{} }

// NewCodex returns the Codex source.
func NewCodex() Source { return codexSource{} }

// Lookup resolves a provider name. Cursor is intentionally unsupported.
func Lookup(name string) (Source, error) {
	switch name {
	case "claude-code":
		return NewClaudeCode(), nil
	case "codex":
		return NewCodex(), nil
	default:
		return nil, fmt.Errorf("unsupported provider %q: supported providers are \"claude-code\" and \"codex\" (Cursor is not supported)", name)
	}
}

func defaultHomeRoot(elem ...string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	parts := append([]string{home}, elem...)
	return filepath.Join(parts...), nil
}
