package main

import (
	"os"
	"runtime"
	"testing"
)

// TestMain keeps command tests hermetic: config.Load must not read a developer's
// real user config, and stray DEVLOG_LEARN_* variables must not change defaults.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "devlog-learn-test-config-*")
	if err != nil {
		panic(err)
	}
	if runtime.GOOS == "windows" {
		_ = os.Setenv("APPDATA", dir)
	} else {
		_ = os.Setenv("XDG_CONFIG_HOME", dir)
	}
	for _, name := range []string{
		"DEVLOG_LEARN_CACHE_DIR",
		"DEVLOG_LEARN_CURSOR",
		"DEVLOG_LEARN_OUT",
		"DEVLOG_LEARN_PROVIDER",
		"DEVLOG_LEARN_SINCE",
		"DEVLOG_LEARN_MODEL",
		"DEVLOG_LEARN_CLAUDE_BIN",
		"DEVLOG_LEARN_CODEX_BIN",
		"DEVLOG_LEARN_CLAUDE_DIR",
		"DEVLOG_LEARN_CODEX_DIR",
		"DEVLOG_LEARN_REDACT_CONFIG",
		"DEVLOG_LEARN_CONCURRENCY",
	} {
		_ = os.Unsetenv(name)
	}
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}
