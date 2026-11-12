//go:build windows

package extract

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeBatch(t *testing.T, path, body string) {
	t.Helper()
	body = strings.ReplaceAll(body, "\n", "\r\n")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write batch: %v", err)
	}
}

func TestResolveExecutableFindsKnownExtensions(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "测试 & dir")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	cmdPath := filepath.Join(dir, "tool.cmd")
	writeBatch(t, cmdPath, "@echo off\n")

	got, err := resolveExecutable(filepath.Join(dir, "tool"))
	if err != nil {
		t.Fatalf("resolveExecutable(.cmd): %v", err)
	}
	if !strings.EqualFold(filepath.Clean(got), filepath.Clean(cmdPath)) {
		t.Fatalf("resolved = %q, want %q", got, cmdPath)
	}

	batDir := filepath.Join(t.TempDir(), "bat & dir")
	if err := os.MkdirAll(batDir, 0o755); err != nil {
		t.Fatalf("mkdir bat: %v", err)
	}
	batPath := filepath.Join(batDir, "tool.bat")
	writeBatch(t, batPath, "@echo off\n")
	got, err = resolveExecutable(filepath.Join(batDir, "tool"))
	if err != nil {
		t.Fatalf("resolveExecutable(.bat): %v", err)
	}
	if !strings.EqualFold(filepath.Clean(got), filepath.Clean(batPath)) {
		t.Fatalf("resolved = %q, want %q", got, batPath)
	}
}

func TestCLIClientWindowsBatShim(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "bat 目录 & space")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	fake := filepath.Join(dir, "fake cli.bat")
	writeBatch(t, fake, `@echo off
if "%~1"=="--version" (echo fake-bat 1.0& exit /b 0)
if "%~1"=="--help" (echo --output-format& echo --no-session-persistence& echo --model& exit /b 0)
more > nul
echo {"type":"result","result":"{\"title\":\"ok\"}","modelUsage":{"fake-bat":{}}}
exit /b 0
`)

	client, err := NewCLIClient(CLIOptions{Provider: "claude-code", Bin: fake, Timeout: 10 * time.Second})
	if err != nil {
		t.Fatalf("NewCLIClient: %v", err)
	}
	version, err := client.Version(context.Background())
	if err != nil || !strings.Contains(version, "fake-bat 1.0") {
		t.Fatalf("version = %q, err = %v", version, err)
	}
	response, err := client.Complete(context.Background(), Request{Prompt: "hello 世界 & more"})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if response.Text != `{"title":"ok"}` {
		t.Fatalf("text = %q", response.Text)
	}
}

func TestCLIClientWindowsCmdShim(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "测试 目录 & space")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	fake := filepath.Join(dir, "fake cli.cmd")
	writeBatch(t, fake, `@echo off
if "%~1"=="--version" (
  echo fake-cli 1.0
  exit /b 0
)
if "%~1"=="--help" (
  echo --output-format
  echo --no-session-persistence
  echo --model
  echo --ephemeral
  echo --skip-git-repo-check
  echo --sandbox
  echo --color
  echo --output-last-message
  exit /b 0
)
echo ARGS:%* > "%~dp0args.txt"
more > "%~dp0stdin.txt"
echo stderr-line 1>&2
echo {"type":"result","result":"{\"title\":\"ok\"}","modelUsage":{"fake-model":{}}}
exit /b 0
`)

	client, err := NewCLIClient(CLIOptions{
		Provider: "claude-code",
		Bin:      fake,
		Timeout:  10 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewCLIClient: %v", err)
	}

	version, err := client.Version(context.Background())
	if err != nil || !strings.Contains(version, "fake-cli 1.0") {
		t.Fatalf("version = %q, err = %v", version, err)
	}
	caps, err := client.Capabilities(context.Background())
	if err != nil {
		t.Fatalf("Capabilities: %v", err)
	}
	for _, flag := range []string{"--output-format", "--no-session-persistence", "--ephemeral", "--output-last-message"} {
		if !caps[flag] {
			t.Fatalf("missing capability %q: %+v", flag, caps)
		}
	}

	prompt := "hello 世界\nsecond line"
	response, err := client.Complete(context.Background(), Request{Prompt: prompt})
	if err != nil {
		t.Fatalf("Complete: %v\nstderr: %s", err, response.Stderr)
	}
	if response.Text != `{"title":"ok"}` {
		t.Fatalf("text = %q", response.Text)
	}
	if !strings.Contains(response.Stderr, "stderr-line") {
		t.Fatalf("stderr = %q", response.Stderr)
	}

	args, err := os.ReadFile(filepath.Join(dir, "args.txt"))
	if err != nil {
		t.Fatalf("read args: %v", err)
	}
	argsText := string(args)
	for _, want := range []string{"-p", "--output-format", "json", "--no-session-persistence"} {
		if !strings.Contains(argsText, want) {
			t.Fatalf("args missing %q: %s", want, argsText)
		}
	}

	stdin, err := os.ReadFile(filepath.Join(dir, "stdin.txt"))
	if err != nil {
		t.Fatalf("read stdin: %v", err)
	}
	gotPrompt := strings.TrimSpace(strings.ReplaceAll(string(stdin), "\r\n", "\n"))
	if gotPrompt != prompt {
		t.Fatalf("stdin = %q, want %q", gotPrompt, prompt)
	}
}

func TestCLIClientTimeout(t *testing.T) {
	dir := t.TempDir()
	fake := filepath.Join(dir, "slow.cmd")
	writeBatch(t, fake, `@echo off
if "%~1"=="--version" (echo slow-cli 1.0& exit /b 0)
if "%~1"=="--help" (echo --output-format& exit /b 0)
ping -n 30 127.0.0.1 >nul
`)

	client, err := NewCLIClient(CLIOptions{
		Provider: "claude-code",
		Bin:      fake,
		Timeout:  300 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewCLIClient: %v", err)
	}
	_, err = client.Complete(context.Background(), Request{Prompt: "x"})
	if err == nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want deadline exceeded", err)
	}
}

func TestCLIClientExitCode(t *testing.T) {
	dir := t.TempDir()
	fake := filepath.Join(dir, "fail.cmd")
	writeBatch(t, fake, `@echo off
if "%~1"=="--version" (echo fail-cli 1.0& exit /b 0)
if "%~1"=="--help" (echo --output-format& exit /b 0)
echo failed 1>&2
exit /b 7
`)

	client, err := NewCLIClient(CLIOptions{Provider: "claude-code", Bin: fake, Timeout: 10 * time.Second})
	if err != nil {
		t.Fatalf("NewCLIClient: %v", err)
	}
	response, err := client.Complete(context.Background(), Request{Prompt: "x"})
	if err == nil {
		t.Fatal("expected an error")
	}
	if response.ExitCode != 7 {
		t.Fatalf("exit code = %d, want 7", response.ExitCode)
	}
	if !strings.Contains(response.Stderr, "failed") {
		t.Fatalf("stderr = %q", response.Stderr)
	}
}
