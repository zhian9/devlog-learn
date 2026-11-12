package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// writeFakeCLI creates a provider-shaped fake executable. On Windows it is a
// .cmd shim (the production code exercises the PowerShell wrapper); elsewhere
// it is a POSIX shell script. Both consume stdin and print the same one-line
// model response, so tests are deterministic and never call a real LLM.
func writeFakeCLI(t *testing.T, dir, base, version, response string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create fake CLI dir: %v", err)
	}

	if runtime.GOOS == "windows" {
		path := filepath.Join(dir, base+".cmd")
		body := strings.Join([]string{
			`@echo off`,
			`if "%~1"=="--version" (echo ` + version + `& exit /b 0)`,
			`if "%~1"=="--help" (echo --output-format& echo --no-session-persistence& echo --model& echo --ephemeral& echo --skip-git-repo-check& echo --sandbox& echo --color& echo --output-last-message& exit /b 0)`,
			`if "%~1"=="exec" if "%~2"=="--help" (echo --output-format& echo --no-session-persistence& echo --model& echo --ephemeral& echo --skip-git-repo-check& echo --sandbox& echo --color& echo --output-last-message& exit /b 0)`,
			`more > nul`,
			`echo ` + response,
			`exit /b 0`,
		}, "\n")
		body = strings.ReplaceAll(body, "\n", "\r\n")
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatalf("write fake CLI: %v", err)
		}
		return path
	}

	path := filepath.Join(dir, base+".sh")
	body := strings.Join([]string{
		`#!/bin/sh`,
		`case "$1" in`,
		`  --version) echo '` + version + `'; exit 0;;`,
		`  --help) printf '%s\n' --output-format --no-session-persistence --model --ephemeral --skip-git-repo-check --sandbox --color --output-last-message; exit 0;;`,
		`esac`,
		`if [ "$1" = "exec" ] && [ "$2" = "--help" ]; then printf '%s\n' --output-format --no-session-persistence --model --ephemeral --skip-git-repo-check --sandbox --color --output-last-message; exit 0; fi`,
		`cat >/dev/null`,
		`printf '%s\n' '` + response + `'`,
		`exit 0`,
	}, "\n") + "\n"
	writeExecutable(t, path, body)
	return path
}

// writeSequenceFakeCLI returns invalid output on the first model call and the
// valid response afterwards. Probes (--version/--help) never touch the marker,
// so this specifically exercises the one-shot JSON repair path.
func writeSequenceFakeCLI(t *testing.T, dir, base, version, invalid, response string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create fake CLI dir: %v", err)
	}

	if runtime.GOOS == "windows" {
		path := filepath.Join(dir, base+".cmd")
		body := strings.Join([]string{
			`@echo off`,
			`if "%~1"=="--version" (echo ` + version + `& exit /b 0)`,
			`if "%~1"=="--help" (echo --output-format& echo --no-session-persistence& echo --model& echo --ephemeral& echo --skip-git-repo-check& echo --sandbox& echo --color& echo --output-last-message& exit /b 0)`,
			`if "%~1"=="exec" if "%~2"=="--help" (echo --output-format& echo --no-session-persistence& echo --model& echo --ephemeral& echo --skip-git-repo-check& echo --sandbox& echo --color& echo --output-last-message& exit /b 0)`,
			`more > nul`,
			`if exist "%~dp0repair-marker" (echo ` + response + `& exit /b 0)`,
			`echo marker > "%~dp0repair-marker"`,
			`echo ` + invalid,
			`exit /b 0`,
		}, "\n")
		body = strings.ReplaceAll(body, "\n", "\r\n")
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatalf("write fake CLI: %v", err)
		}
		return path
	}

	path := filepath.Join(dir, base+".sh")
	body := strings.Join([]string{
		`#!/bin/sh`,
		`case "$1" in`,
		`  --version) echo '` + version + `'; exit 0;;`,
		`  --help) printf '%s\n' --output-format --no-session-persistence --model --ephemeral --skip-git-repo-check --sandbox --color --output-last-message; exit 0;;`,
		`esac`,
		`if [ "$1" = "exec" ] && [ "$2" = "--help" ]; then printf '%s\n' --output-format --no-session-persistence --model --ephemeral --skip-git-repo-check --sandbox --color --output-last-message; exit 0; fi`,
		`cat >/dev/null`,
		`marker="$(dirname "$0")/repair-marker"`,
		`if [ -f "$marker" ]; then printf '%s\n' '` + response + `'; exit 0; fi`,
		`: > "$marker"`,
		`printf '%s\n' '` + invalid + `'`,
		`exit 0`,
	}, "\n") + "\n"
	writeExecutable(t, path, body)
	return path
}

func writeExecutable(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatalf("write fake CLI: %v", err)
	}
	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatalf("chmod fake CLI: %v", err)
	}
}
