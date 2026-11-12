package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"devlog-learn/internal/cache"
	"devlog-learn/internal/config"
	"devlog-learn/internal/pipeline"
	"devlog-learn/internal/redact"
	"devlog-learn/internal/segment"
)

type sessionsOptions struct {
	provider         string
	claudeDir        string
	codexDir         string
	since            string
	includeSubagents bool
	limit            int
	redactConfig     string
	cacheDir         string
	asJSON           bool
}

func newSessionsCmd() *cobra.Command {
	opt := &sessionsOptions{}
	cmd := &cobra.Command{
		Use:   "sessions",
		Short: "List selectable sessions and their prepared segments",
		Long: `Discover and prepare sessions without calling a model, then print the
stable selectors accepted by "extract --session" and the segment IDs accepted
by "extract --segment".

This command does not write cache or cursor files. It reads the cache only to
mark segments whose canonical Doc already exists (doc=yes).`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runSessions(cmd, opt)
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
	flags.StringVar(&opt.cacheDir, "cache-dir", ".devlog-cache", "cache directory used to mark already extracted segments")
	flags.BoolVar(&opt.asJSON, "json", false, "print JSON instead of text")
	return cmd
}

func runSessions(cmd *cobra.Command, opt *sessionsOptions) error {
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

	prepared, runErr := pipeline.Prepare(cmd.Context(), pipeline.PrepareOptions{
		Load: pipeline.LoadOptions{
			Provider:         opt.provider,
			ClaudeDir:        opt.claudeDir,
			CodexDir:         opt.codexDir,
			Since:            cutoff,
			IncludeSubagents: opt.includeSubagents,
			Limit:            opt.limit,
		},
		Segment: segment.DefaultOptions(),
		Redact:  redactCfg,
	})
	if prepared == nil {
		return runErr
	}

	extracted, cacheWarnings := extractedDocIDs(opt.cacheDir)
	list := pipeline.BuildSessionList(prepared, extracted, time.Now(), opt.since)
	list.Warnings = append(list.Warnings, cacheWarnings...)

	stderr := cmd.ErrOrStderr()
	if opt.asJSON {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		if err := enc.Encode(list); err != nil {
			return err
		}
	} else {
		printSessions(cmd.OutOrStdout(), list)
		fmt.Fprintln(cmd.OutOrStdout(), `use: devlog-learn extract --session <selector>   or   devlog-learn extract --segment <segment-id>`)
	}
	printDryRunWarnings(stderr, list.Warnings)
	return runErr
}

// extractedDocIDs reads existing canonical docs without creating the cache
// directory. A stale Doc still counts as "already extracted".
func extractedDocIDs(cacheDir string) (map[string]bool, []string) {
	extracted := map[string]bool{}
	if strings.TrimSpace(cacheDir) == "" {
		return extracted, nil
	}
	if _, err := os.Stat(cacheDir); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return extracted, nil
		}
		return extracted, []string{fmt.Sprintf("read cache %s: %v", cacheDir, err)}
	}
	store, err := cache.Open(cacheDir)
	if err != nil {
		return extracted, []string{err.Error()}
	}
	infos, warnings := store.ListDocs()
	for _, info := range infos {
		extracted[info.ID] = true
	}
	return extracted, warnings
}

func printSessions(out io.Writer, list *pipeline.SessionList) {
	if list == nil || len(list.Sessions) == 0 {
		fmt.Fprintln(out, "no sessions found")
		return
	}
	fmt.Fprintf(out, "sessions (since=%s, sessions=%d, segments=%d, dropped=%d)\n\n",
		list.Since, len(list.Sessions), list.SegmentCount, list.DropCount)
	for i, session := range list.Sessions {
		date := "-"
		if !session.Start.IsZero() {
			date = session.Start.Local().Format("2006-01-02")
		}
		fmt.Fprintf(out, "[%d] %s  project=%s  date=%s  segments=%d  extracted=%d/%d\n",
			i+1, session.Selector, emptyDash(session.Project), date,
			session.SegmentCount, session.Extracted, session.SegmentCount)
		fmt.Fprintf(out, "    title: %s\n", truncateRunes(oneLine(session.Title, "-"), 120))
		for _, seg := range session.Segments {
			doc := "no"
			if seg.Extracted {
				doc = "yes"
			}
			fmt.Fprintf(out, "    - %s  %s  events=%d  bytes=%d  doc=%s  %s\n",
				seg.ID,
				seg.Start.Local().Format("2006-01-02T15:04"),
				seg.EventCount, seg.Bytes, doc,
				truncateRunes(oneLine(seg.Title, "-"), 100))
		}
		fmt.Fprintln(out)
	}
}

func oneLine(value, fallback string) string {
	value = strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	if value == "" {
		return fallback
	}
	return value
}
