//go:build windows

package extract

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// resolveExecutable handles npm-style .cmd/.bat shims and extensionless paths
// that Windows CreateProcess cannot execute directly.
func resolveExecutable(name string) (string, error) {
	path, err := exec.LookPath(name)
	if err != nil {
		if _, statErr := os.Stat(name); statErr != nil {
			return "", err
		}
		path = name
	}
	if filepath.Ext(path) == "" {
		for _, suffix := range []string{".exe", ".cmd", ".bat"} {
			candidate := path + suffix
			if _, statErr := os.Stat(candidate); statErr == nil {
				return candidate, nil
			}
		}
	}
	return path, nil
}

// newCommand wraps .cmd/.bat shims in PowerShell. Arguments are passed as a
// JSON environment variable and splatted, so cmd.exe metacharacters, quotes,
// spaces, ampersands, and non-ASCII paths never enter a command line.
func newCommand(ctx context.Context, exe string, args []string, dir string) (*exec.Cmd, error) {
	ext := strings.ToLower(filepath.Ext(exe))
	if ext != ".cmd" && ext != ".bat" {
		cmd := exec.CommandContext(ctx, exe, args...)
		cmd.Dir = dir
		return cmd, nil
	}

	shell, err := exec.LookPath("powershell.exe")
	if err != nil {
		shell, err = exec.LookPath("pwsh.exe")
		if err != nil {
			return nil, fmt.Errorf("Windows batch shim %q requires powershell.exe or pwsh.exe: %w", exe, err)
		}
	}

	argJSON, err := json.Marshal(args)
	if err != nil {
		return nil, fmt.Errorf("encode batch arguments: %w", err)
	}

	const script = `$ErrorActionPreference='Stop'; ` +
		`$exe=$env:DEVLOG_LEARN_WIN_EXE; ` +
		`$raw=$env:DEVLOG_LEARN_WIN_ARGS; ` +
		`if([string]::IsNullOrEmpty($raw)){$a=@()}else{$a=@(ConvertFrom-Json -InputObject $raw)}; ` +
		`& $exe @a; ` +
		`$code=$LASTEXITCODE; if($null -eq $code){$code=1}; exit $code`

	cmd := exec.CommandContext(ctx, shell,
		"-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass",
		"-Command", script,
	)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"DEVLOG_LEARN_WIN_EXE="+exe,
		"DEVLOG_LEARN_WIN_ARGS="+string(argJSON),
	)
	return cmd, nil
}
