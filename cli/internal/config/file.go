package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// File holds user-editable defaults. Every field is optional; an empty value
// means "not configured". Flags always win over environment variables, which
// win over the project file, which wins over the user file.
//
// The file intentionally has no version field yet: unknown fields are rejected
// so a typo in a key is reported instead of silently ignored.
type File struct {
	CacheDir     string `json:"cache_dir,omitempty"`
	Cursor       string `json:"cursor,omitempty"`
	Out          string `json:"out,omitempty"`
	Provider     string `json:"provider,omitempty"`
	Since        string `json:"since,omitempty"`
	Model        string `json:"model,omitempty"`
	ClaudeBin    string `json:"claude_bin,omitempty"`
	CodexBin     string `json:"codex_bin,omitempty"`
	ClaudeDir    string `json:"claude_dir,omitempty"`
	CodexDir     string `json:"codex_dir,omitempty"`
	RedactConfig string `json:"redact_config,omitempty"`
	Concurrency  int    `json:"concurrency,omitempty"`
}

// ConfigFileName is the project-local config file looked up in the current
// working directory.
const ConfigFileName = ".devlog-learn.json"

// Load reads the user config, then the project config, and finally applies
// environment variables. Missing files are ignored.
func Load() (File, error) {
	var paths []string
	if dir, err := os.UserConfigDir(); err == nil && strings.TrimSpace(dir) != "" {
		paths = append(paths, filepath.Join(dir, "devlog-learn", "config.json"))
	}
	if cwd, err := os.Getwd(); err == nil && strings.TrimSpace(cwd) != "" {
		paths = append(paths, filepath.Join(cwd, ConfigFileName))
	}
	cfg, err := LoadFrom(paths...)
	if err != nil {
		return File{}, err
	}
	if err := cfg.ApplyEnv(); err != nil {
		return File{}, err
	}
	return cfg, nil
}

// LoadFrom merges the given config files in order; later files override
// earlier ones. Missing files are skipped.
func LoadFrom(paths ...string) (File, error) {
	var merged File
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return merged, fmt.Errorf("read config %s: %w", path, err)
		}
		var layer File
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&layer); err != nil {
			return merged, fmt.Errorf("parse config %s: %w", path, err)
		}
		if layer.Concurrency < 0 {
			return merged, fmt.Errorf("config %s: concurrency must be positive", path)
		}
		merged.merge(layer)
	}
	return merged, nil
}

// ApplyEnv overlays DEVLOG_LEARN_* environment variables. Empty variables are
// treated as unset.
func (f *File) ApplyEnv() error {
	set := func(env string, dst *string) {
		if value, ok := os.LookupEnv(env); ok {
			if value = strings.TrimSpace(value); value != "" {
				*dst = value
			}
		}
	}
	set("DEVLOG_LEARN_CACHE_DIR", &f.CacheDir)
	set("DEVLOG_LEARN_CURSOR", &f.Cursor)
	set("DEVLOG_LEARN_OUT", &f.Out)
	set("DEVLOG_LEARN_PROVIDER", &f.Provider)
	set("DEVLOG_LEARN_SINCE", &f.Since)
	set("DEVLOG_LEARN_MODEL", &f.Model)
	set("DEVLOG_LEARN_CLAUDE_BIN", &f.ClaudeBin)
	set("DEVLOG_LEARN_CODEX_BIN", &f.CodexBin)
	set("DEVLOG_LEARN_CLAUDE_DIR", &f.ClaudeDir)
	set("DEVLOG_LEARN_CODEX_DIR", &f.CodexDir)
	set("DEVLOG_LEARN_REDACT_CONFIG", &f.RedactConfig)
	if value, ok := os.LookupEnv("DEVLOG_LEARN_CONCURRENCY"); ok {
		value = strings.TrimSpace(value)
		if value != "" {
			concurrency, err := strconv.Atoi(value)
			if err != nil || concurrency <= 0 {
				return fmt.Errorf("DEVLOG_LEARN_CONCURRENCY must be a positive integer, got %q", value)
			}
			f.Concurrency = concurrency
		}
	}
	return nil
}

func (f *File) merge(layer File) {
	overlayString(&f.CacheDir, layer.CacheDir)
	overlayString(&f.Cursor, layer.Cursor)
	overlayString(&f.Out, layer.Out)
	overlayString(&f.Provider, layer.Provider)
	overlayString(&f.Since, layer.Since)
	overlayString(&f.Model, layer.Model)
	overlayString(&f.ClaudeBin, layer.ClaudeBin)
	overlayString(&f.CodexBin, layer.CodexBin)
	overlayString(&f.ClaudeDir, layer.ClaudeDir)
	overlayString(&f.CodexDir, layer.CodexDir)
	overlayString(&f.RedactConfig, layer.RedactConfig)
	if layer.Concurrency > 0 {
		f.Concurrency = layer.Concurrency
	}
}

func overlayString(dst *string, value string) {
	if strings.TrimSpace(value) != "" {
		*dst = strings.TrimSpace(value)
	}
}
