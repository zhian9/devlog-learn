package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeConfig(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

func TestLoadFromMergesInOrder(t *testing.T) {
	dir := t.TempDir()
	user := writeConfig(t, dir, "user.json", `{
		"cache_dir": "user-cache",
		"provider": "claude-code",
		"since": "30d",
		"concurrency": 4
	}`)
	project := writeConfig(t, dir, "project.json", `{
		"cache_dir": "project-cache",
		"model": "test-model"
	}`)

	cfg, err := LoadFrom(filepath.Join(dir, "missing.json"), user, project)
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	if cfg.CacheDir != "project-cache" {
		t.Errorf("CacheDir = %q, want project override", cfg.CacheDir)
	}
	if cfg.Provider != "claude-code" || cfg.Since != "30d" || cfg.Model != "test-model" {
		t.Errorf("merged config = %+v", cfg)
	}
	if cfg.Concurrency != 4 {
		t.Errorf("Concurrency = %d, want 4", cfg.Concurrency)
	}
}

func TestLoadFromRejectsUnknownFields(t *testing.T) {
	dir := t.TempDir()
	path := writeConfig(t, dir, "bad.json", `{"cache_dir": "x", "cashe_dir": "y"}`)
	_, err := LoadFrom(path)
	if err == nil || !strings.Contains(err.Error(), "cashe_dir") {
		t.Fatalf("unknown field error = %v", err)
	}
}

func TestLoadFromRejectsNegativeConcurrency(t *testing.T) {
	dir := t.TempDir()
	path := writeConfig(t, dir, "bad.json", `{"concurrency": -1}`)
	if _, err := LoadFrom(path); err == nil {
		t.Fatal("negative concurrency accepted")
	}
}

func TestLoadFromIgnoresMissingFiles(t *testing.T) {
	cfg, err := LoadFrom(filepath.Join(t.TempDir(), "nope.json"))
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	if cfg != (File{}) {
		t.Fatalf("config = %+v, want zero", cfg)
	}
}

func TestApplyEnvOverridesFileValues(t *testing.T) {
	cfg := File{CacheDir: "file-cache", Provider: "claude-code"}
	t.Setenv("DEVLOG_LEARN_CACHE_DIR", "env-cache")
	t.Setenv("DEVLOG_LEARN_PROVIDER", "all")
	t.Setenv("DEVLOG_LEARN_CONCURRENCY", "3")
	t.Setenv("DEVLOG_LEARN_MODEL", "env-model")

	if err := cfg.ApplyEnv(); err != nil {
		t.Fatalf("ApplyEnv: %v", err)
	}
	if cfg.CacheDir != "env-cache" || cfg.Provider != "all" || cfg.Model != "env-model" {
		t.Fatalf("config = %+v", cfg)
	}
	if cfg.Concurrency != 3 {
		t.Fatalf("Concurrency = %d", cfg.Concurrency)
	}
}

func TestApplyEnvRejectsInvalidConcurrency(t *testing.T) {
	t.Setenv("DEVLOG_LEARN_CONCURRENCY", "many")
	var cfg File
	if err := cfg.ApplyEnv(); err == nil {
		t.Fatal("invalid concurrency accepted")
	}
}

func TestApplyEnvIgnoresEmptyValues(t *testing.T) {
	cfg := File{CacheDir: "keep"}
	t.Setenv("DEVLOG_LEARN_CACHE_DIR", "  ")
	if err := cfg.ApplyEnv(); err != nil {
		t.Fatalf("ApplyEnv: %v", err)
	}
	if cfg.CacheDir != "keep" {
		t.Fatalf("empty env should be treated as unset, got %q", cfg.CacheDir)
	}
}
