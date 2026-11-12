package main

import (
	"strings"
	"testing"

	"devlog-learn/internal/config"
)

func TestApplyConfigDefaultsOnlySetsUnchangedFlags(t *testing.T) {
	cmd := newIndexCmd()
	if err := cmd.Flags().Set("cache-dir", "explicit-cache"); err != nil {
		t.Fatalf("set cache-dir: %v", err)
	}
	cfg := config.File{CacheDir: "config-cache", Out: "config-out"}

	if err := applyConfigDefaults(cmd, cfg); err != nil {
		t.Fatalf("applyConfigDefaults: %v", err)
	}
	cacheDir, _ := cmd.Flags().GetString("cache-dir")
	if cacheDir != "explicit-cache" {
		t.Fatalf("cache-dir = %q, want explicit flag to win", cacheDir)
	}
	out, _ := cmd.Flags().GetString("out")
	if out != "config-out" {
		t.Fatalf("out = %q, want config default", out)
	}
}

func TestApplyConfigDefaultsSkipsExtractOut(t *testing.T) {
	cmd := newExtractCmd()
	cfg := config.File{Out: "must-not-apply", Provider: "all"}

	if err := applyConfigDefaults(cmd, cfg); err != nil {
		t.Fatalf("applyConfigDefaults: %v", err)
	}
	out, _ := cmd.Flags().GetString("out")
	if out != "" {
		t.Fatalf("extract --out = %q, config must not change stdout semantics", out)
	}
	provider, _ := cmd.Flags().GetString("provider")
	if provider != "all" {
		t.Fatalf("provider = %q, want config value", provider)
	}
}

func TestConfigEnvAppliesToCommand(t *testing.T) {
	doc := commandDoc("env1", "demo", "Env doc", outputTime)
	cacheDir := seedCache(t, doc)
	t.Setenv("DEVLOG_LEARN_CACHE_DIR", cacheDir)

	stdout, _, err := runCommand("review", "--list")
	if err != nil {
		t.Fatalf("review --list: %v", err)
	}
	if !strings.Contains(stdout, doc.ID) {
		t.Fatalf("env cache dir not applied, stdout = %q", stdout)
	}
}

func TestApplyConfigDefaultsIgnoresUnknownFlags(t *testing.T) {
	cmd := newReviewCmd()
	cfg := config.File{Since: "30d", CacheDir: "cfg-cache"}
	if err := applyConfigDefaults(cmd, cfg); err != nil {
		t.Fatalf("applyConfigDefaults: %v", err)
	}
	// review has no --since flag; it must be ignored rather than error.
	if cmd.Flags().Lookup("since") != nil {
		t.Fatal("review unexpectedly has a --since flag")
	}
	cacheDir, _ := cmd.Flags().GetString("cache-dir")
	if cacheDir != "cfg-cache" {
		t.Fatalf("cache-dir = %q", cacheDir)
	}
}
