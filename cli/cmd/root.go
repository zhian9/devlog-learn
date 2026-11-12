package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"devlog-learn/internal/config"
	"devlog-learn/internal/version"
)

var verbose bool

func newRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "devlog-learn",
		Short: "Turn Claude Code and Codex session logs into structured knowledge documents",
		Long: `devlog-learn reads local session logs and produces structured learning
documents.

Supported providers in this version: claude-code, codex.
Cursor sessions are not supported.

Commands:
  ingest    discovery/parsing diagnostics
  dry-run   segmentation/redaction preview
  sessions  list selectable sessions and segment IDs
  extract   incremental, cache-aware LLM extraction into Doc JSON
  run       extract + render + index in one command
  wizard    interactively pick sessions, then run the pipeline
  render    render active Docs as Markdown with YAML front matter
  index     build global and per-project index.json/README.md
  search    search active Docs from the terminal
  projects  list projects and their review counts
  status    read-only health report for cache, cursor, and outputs
  questions list open questions or mark one resolved
  related   show read-only related documents for one Doc
  review    list or update human review status
  digest    write a compact AGENTS.md memory digest for agents
  mcp       serve read-only project memory over the Model Context Protocol
  serve     open the local web UI in an application window

Defaults can come from DEVLOG_LEARN_* environment variables, the user config
file (<user-config>/devlog-learn/config.json), or .devlog-learn.json in the
current directory. Explicit flags always win.`,
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       version.ToolVersion,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			return applyConfigDefaults(cmd, cfg)
		},
	}
	cmd.SetVersionTemplate("devlog-learn {{.Version}}\n")

	cmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "enable debug logs on stderr")
	cmd.AddCommand(newIngestCmd())
	cmd.AddCommand(newDryRunCmd())
	cmd.AddCommand(newSessionsCmd())
	cmd.AddCommand(newExtractCmd())
	cmd.AddCommand(newRunCmd())
	cmd.AddCommand(newWizardCmd())
	cmd.AddCommand(newRenderCmd())
	cmd.AddCommand(newIndexCmd())
	cmd.AddCommand(newSearchCmd())
	cmd.AddCommand(newProjectsCmd())
	cmd.AddCommand(newStatusCmd())
	cmd.AddCommand(newQuestionsCmd())
	cmd.AddCommand(newRelatedCmd())
	cmd.AddCommand(newReviewCmd())
	cmd.AddCommand(newDigestCmd())
	cmd.AddCommand(newMCPCmd())
	cmd.AddCommand(newServeCmd())
	return cmd
}

// Execute runs the CLI and returns the first error for main to report.
func Execute() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	root := newRootCmd()
	root.SetContext(ctx)
	root.SetOut(os.Stdout)
	root.SetErr(os.Stderr)
	return root.ExecuteContext(ctx)
}

func newLogger() *slog.Logger {
	level := slog.LevelInfo
	if verbose {
		level = slog.LevelDebug
	}
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
}
