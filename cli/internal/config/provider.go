// Package config holds small, dependency-free option parsers shared by the
// CLI and pipeline.
package config

import (
	"fmt"
	"strings"
)

// SupportedProviders is the complete provider set for this version. Cursor is
// intentionally absent: devlog-learn only reads Claude Code and Codex logs.
var SupportedProviders = []string{"claude-code", "codex", "all"}

// ValidateProvider canonicalizes a provider flag. It is called before any
// filesystem side effect so an unsupported value never creates cache or cursor
// files. "Cursor" is explicitly rejected with a clear message.
func ValidateProvider(value string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	switch normalized {
	case "claude-code", "codex", "all":
		return normalized, nil
	}
	return "", fmt.Errorf("unsupported provider %q: supported providers are claude-code and codex (Cursor is not supported)", value)
}
