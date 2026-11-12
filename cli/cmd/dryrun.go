package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"devlog-learn/internal/config"
	"devlog-learn/internal/pipeline"
	"devlog-learn/internal/redact"
	"devlog-learn/internal/segment"
)

type dryRunOptions struct {
	provider         string
	claudeDir        string
	codexDir         string
	since            string
	includeSubagents bool
	limit            int
	redactConfig     string
	jsonOut          bool
	showDrops        bool
	showEvents       bool
}

func newDryRunCmd() *cobra.Command {
	opt := &dryRunOptions{}
	cmd := &cobra.Command{
		Use:   "dry-run",
		Short: "Preview discovery, segmentation, redaction, and payload sizes",
		Long: `Preview what would be sent for knowledge extraction without calling an LLM.

Supported providers: claude-code, codex, all.
Cursor is not supported.

The command discovers sessions, parses events, splits segments, applies
redaction, and reports drops and payload sizes. It does not write cache files.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runDryRun(cmd, opt)
		},
	}

	flags := cmd.Flags()
	flags.StringVar(&opt.provider, "provider", "claude-code", "provider to read: claude-code, codex, or all (Cursor is not supported)")
	flags.StringVar(&opt.claudeDir, "claude-dir", "", "Claude Code projects directory (default ~/.claude/projects)")
	flags.StringVar(&opt.codexDir, "codex-dir", "", "Codex sessions directory (default ~/.codex/sessions)")
	flags.StringVar(&opt.since, "since", "7d", "only read files modified after this cutoff: 7d, 2w, 36h, RFC3339, or all")
	flags.BoolVar(&opt.includeSubagents, "include-subagents", false, "include Claude sidechains and Codex subagent sessions")
	flags.IntVar(&opt.limit, "limit", 0, "maximum files per provider; 0 means no limit")
	flags.StringVar(&opt.redactConfig, "redact-config", "", "JSON file with additional redaction rules/options")
	flags.BoolVar(&opt.jsonOut, "json", false, "print the report as JSON on stdout")
	flags.BoolVar(&opt.showDrops, "show-drops", false, "print details for every filtered segment")
	flags.BoolVar(&opt.showEvents, "show-events", false, "print redacted event previews for every segment")
	return cmd
}

func runDryRun(cmd *cobra.Command, opt *dryRunOptions) error {
	provider, err := config.ValidateProvider(opt.provider)
	if err != nil {
		return err
	}
	opt.provider = provider

	if opt.limit < 0 {
		return fmt.Errorf("--limit must be zero or positive")
	}

	cutoff, err := config.ParseSince(opt.since, time.Now())
	if err != nil {
		return err
	}

	redactCfg := redact.DefaultConfig()
	if opt.redactConfig != "" {
		redactCfg, err = redact.LoadConfig(opt.redactConfig)
		if err != nil {
			return err
		}
	}

	report, runErr := pipeline.DryRun(cmd.Context(), pipeline.Options{
		Load: pipeline.LoadOptions{
			Provider:         opt.provider,
			ClaudeDir:        opt.claudeDir,
			CodexDir:         opt.codexDir,
			Since:            cutoff,
			IncludeSubagents: opt.includeSubagents,
			Limit:            opt.limit,
		},
		SinceLabel:    opt.since,
		Segment:       segment.DefaultOptions(),
		Redact:        redactCfg,
		IncludeEvents: opt.showEvents,
		Now:           time.Now,
	})
	if report == nil {
		return runErr
	}

	stdout := cmd.OutOrStdout()
	stderr := cmd.ErrOrStderr()
	if opt.jsonOut {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(report); err != nil && runErr == nil {
			runErr = fmt.Errorf("write JSON report: %w", err)
		}
	} else {
		printDryRun(stdout, report, opt.showDrops, opt.showEvents)
		printDryRunWarnings(stderr, report.Warnings)
	}
	return runErr
}

func printDryRun(w io.Writer, report *pipeline.DryRunReport, showDrops, showEvents bool) {
	for _, item := range report.Segments {
		fmt.Fprintf(w, "[%s] %s/%s\n", item.Source, item.Project, item.SessionID)
		fmt.Fprintf(w, "  segment=%s hash=%s boundary=%s events=%d users=%d bytes=%s\n",
			item.SegmentID, shortHash(item.Hash), item.Boundary, item.EventCount, item.UserTextCount, humanBytes(item.Bytes))
		fmt.Fprintf(w, "  title=%q\n", item.Title)
		fmt.Fprintf(w, "  redactions=%s\n", formatCounts(item.Redactions))
		if showEvents {
			for _, ev := range item.Events {
				fmt.Fprintf(w, "    seq=%03d %-18s role=%-9s tool=%-12s time=%s %q\n",
					ev.Seq, ev.Kind, ev.Role, emptyDash(ev.Tool), formatTime(ev.Time), ev.Text)
			}
		}
	}

	fmt.Fprintf(w, "\nsummary: files=%d sessions=%d excluded=%d segments=%d dropped=%d events=%d bytes=%s\n",
		report.Files, report.Sessions, report.Excluded, report.SegmentCount, report.DropCount, report.EventCount, humanBytes(report.Bytes))
	fmt.Fprintf(w, "versions: parser=%d segmenter=%d redaction=%d\n",
		report.ParserVersion, report.SegmenterVersion, report.RedactionVersion)
	fmt.Fprintf(w, "redactions: %s\n", formatCounts(report.Redactions))

	if report.DropCount > 0 {
		counts := map[string]int{}
		for _, drop := range report.Drops {
			for _, reason := range drop.Reasons {
				counts[string(reason)]++
			}
		}
		fmt.Fprintf(w, "drop reasons: %s\n", formatCounts(counts))
	}
	if showDrops {
		for _, drop := range report.Drops {
			reasons := make([]string, len(drop.Reasons))
			for i, reason := range drop.Reasons {
				reasons[i] = string(reason)
			}
			fmt.Fprintf(w, "  drop [%s] %s/%s segment=%s reasons=%s events=%d detail=%s\n",
				drop.Source, drop.Project, drop.SessionID, drop.SegmentID,
				strings.Join(reasons, ","), drop.EventCount, drop.Detail)
		}
	}
	if report.SegmentCount == 0 {
		fmt.Fprintln(w, "no segments would be sent to the model")
	}
}

func printDryRunWarnings(w io.Writer, warnings []string) {
	if len(warnings) == 0 {
		return
	}
	const limit = 10
	for i, warning := range warnings {
		if i >= limit {
			fmt.Fprintf(w, "warning: ... %d more warnings\n", len(warnings)-limit)
			break
		}
		fmt.Fprintf(w, "warning: %s\n", warning)
	}
}

func shortHash(hash string) string {
	if len(hash) <= 12 {
		return hash
	}
	return hash[:12]
}

func humanBytes(n int) string {
	if n < 1024 {
		return fmt.Sprintf("%d B", n)
	}
	if n < 1024*1024 {
		return fmt.Sprintf("%.1f KiB (%d B)", float64(n)/1024, n)
	}
	return fmt.Sprintf("%.1f MiB (%d B)", float64(n)/(1024*1024), n)
}
