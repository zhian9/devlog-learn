package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"devlog-learn/internal/config"
)

// applyConfigDefaults merges config-file and environment values into flags the
// user did not set explicitly. Cobra has already applied flag defaults, so the
// only reliable signal is Flags().Changed.
//
// "out" is deliberately not applied to extract: there it means "write Doc JSON
// files instead of stdout", while everywhere else it means the rendered
// Markdown/index directory.
func applyConfigDefaults(cmd *cobra.Command, cfg config.File) error {
	command := cmd.Name()
	values := [][2]string{
		{"cache-dir", cfg.CacheDir},
		{"cursor", cfg.Cursor},
		{"provider", cfg.Provider},
		{"since", cfg.Since},
		{"model", cfg.Model},
		{"claude-bin", cfg.ClaudeBin},
		{"codex-bin", cfg.CodexBin},
		{"claude-dir", cfg.ClaudeDir},
		{"codex-dir", cfg.CodexDir},
		{"redact-config", cfg.RedactConfig},
	}
	if command != "extract" {
		values = append(values, [2]string{"out", cfg.Out})
	}
	if cfg.Concurrency > 0 {
		values = append(values, [2]string{"concurrency", strconv.Itoa(cfg.Concurrency)})
	}

	for _, pair := range values {
		name, value := pair[0], pair[1]
		if strings.TrimSpace(value) == "" {
			continue
		}
		flag := cmd.Flags().Lookup(name)
		if flag == nil || flag.Changed {
			continue
		}
		if err := cmd.Flags().Set(name, value); err != nil {
			return fmt.Errorf("apply config %s: %w", name, err)
		}
	}
	return nil
}
