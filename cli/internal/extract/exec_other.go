//go:build !windows

package extract

import (
	"context"
	"os/exec"
)

func resolveExecutable(name string) (string, error) {
	return exec.LookPath(name)
}

func newCommand(ctx context.Context, exe string, args []string, dir string) (*exec.Cmd, error) {
	cmd := exec.CommandContext(ctx, exe, args...)
	cmd.Dir = dir
	return cmd, nil
}
