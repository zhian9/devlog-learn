package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"devlog-learn/internal/config"
	"devlog-learn/internal/ingest"
	"devlog-learn/internal/version"
)

type ingestOptions struct {
	provider         string
	claudeDir        string
	codexDir         string
	since            string
	includeSubagents bool
	jsonOut          bool
	showEvents       bool
	limit            int
}

type providerSpec struct {
	source ingest.Source
	dir    string
}

func newIngestCmd() *cobra.Command {
	opt := &ingestOptions{}
	cmd := &cobra.Command{
		Use:   "ingest",
		Short: "Discover and parse Claude Code / Codex session logs",
		Long: `Discover and parse local session logs, then print a diagnostic summary.

Supported providers: claude-code, codex, all.
Cursor is not supported.

This command does not call an LLM and does not write cache or knowledge files.
Use --show-events to inspect parsed events (their text may contain sensitive
data; redaction is implemented in a later phase).`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runIngest(cmd, opt)
		},
	}

	flags := cmd.Flags()
	flags.StringVar(&opt.provider, "provider", "claude-code", "provider to read: claude-code, codex, or all (Cursor is not supported)")
	flags.StringVar(&opt.claudeDir, "claude-dir", "", "Claude Code projects directory (default ~/.claude/projects)")
	flags.StringVar(&opt.codexDir, "codex-dir", "", "Codex sessions directory (default ~/.codex/sessions)")
	flags.StringVar(&opt.since, "since", "7d", "only read files modified after this cutoff: 7d, 2w, 36h, RFC3339, or all")
	flags.BoolVar(&opt.includeSubagents, "include-subagents", false, "include Claude sidechains and Codex subagent sessions")
	flags.BoolVar(&opt.jsonOut, "json", false, "print the diagnostic summary as JSON on stdout")
	flags.BoolVar(&opt.showEvents, "show-events", false, "print parsed events on stdout (may contain sensitive text)")
	flags.IntVar(&opt.limit, "limit", 0, "maximum files per provider; 0 means no limit")
	return cmd
}

func runIngest(cmd *cobra.Command, opt *ingestOptions) error {
	provider, err := config.ValidateProvider(opt.provider)
	if err != nil {
		return err
	}
	opt.provider = provider

	if opt.jsonOut && opt.showEvents {
		return fmt.Errorf("--json and --show-events are mutually exclusive")
	}
	if opt.limit < 0 {
		return fmt.Errorf("--limit must be zero or positive")
	}

	logger := newLogger()
	stdout := cmd.OutOrStdout()
	stderr := cmd.ErrOrStderr()

	now := time.Now()
	cutoff, err := config.ParseSince(opt.since, now)
	if err != nil {
		return err
	}

	specs, err := selectProviders(opt)
	if err != nil {
		return err
	}

	report := ingestReport{
		GeneratedAt:   now,
		Since:         opt.since,
		ParserVersion: version.ParserVersion,
		Providers:     map[string]*providerTotals{},
	}
	var errs []error

	for _, spec := range specs {
		root := spec.dir
		explicit := strings.TrimSpace(root) != ""
		if !explicit {
			root, err = spec.source.DefaultRoot()
			if err != nil {
				errs = append(errs, fmt.Errorf("resolve %s root: %w", spec.source.Name(), err))
				continue
			}
		}

		if _, statErr := os.Stat(root); statErr != nil {
			if explicit {
				errs = append(errs, fmt.Errorf("provider %s directory %q: %w", spec.source.Name(), root, statErr))
			} else {
				logger.Warn("provider directory not found; skipping", "provider", spec.source.Name(), "root", root)
			}
			continue
		}

		refs, err := spec.source.Discover(root, ingest.DiscoverOptions{
			Since:           cutoff,
			IncludeSubagent: opt.includeSubagents,
		})
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if opt.limit > 0 && len(refs) > opt.limit {
			logger.Debug("limiting discovered files", "provider", spec.source.Name(), "discovered", len(refs), "limit", opt.limit)
			refs = refs[:opt.limit]
		}
		logger.Debug("discovered sessions", "provider", spec.source.Name(), "root", root, "files", len(refs))

		for _, ref := range refs {
			sess, err := spec.source.Parse(ref, ingest.ParseOptions{
				IncludeSubagent: opt.includeSubagents,
			})
			if err != nil {
				errs = append(errs, err)
				fmt.Fprintf(stderr, "error: %v\n", err)
				continue
			}

			report.addSession(sess)
			if opt.showEvents && !sess.Excluded {
				printEvents(stdout, sess)
			}
			if !opt.jsonOut {
				printFileSummary(stdout, sess)
				printWarnings(stderr, sess, 5)
			}
		}
	}

	report.computeTotals()

	if opt.jsonOut {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(report); err != nil {
			return fmt.Errorf("write JSON report: %w", err)
		}
	} else {
		printTotals(stdout, &report)
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

func selectProviders(opt *ingestOptions) ([]providerSpec, error) {
	provider, err := config.ValidateProvider(opt.provider)
	if err != nil {
		return nil, err
	}
	switch provider {
	case "claude-code":
		return []providerSpec{{source: ingest.NewClaudeCode(), dir: opt.claudeDir}}, nil
	case "codex":
		return []providerSpec{{source: ingest.NewCodex(), dir: opt.codexDir}}, nil
	case "all":
		return []providerSpec{
			{source: ingest.NewClaudeCode(), dir: opt.claudeDir},
			{source: ingest.NewCodex(), dir: opt.codexDir},
		}, nil
	}
	return nil, fmt.Errorf("unsupported provider %q", opt.provider)
}

type ingestReport struct {
	GeneratedAt   time.Time                  `json:"generated_at"`
	Since         string                     `json:"since"`
	ParserVersion int                        `json:"parser_version"`
	Files         []ingestFileReport         `json:"files"`
	Providers     map[string]*providerTotals `json:"providers"`
	Totals        ingestTotals               `json:"totals"`
}

type ingestFileReport struct {
	Source        string            `json:"source"`
	Path          string            `json:"path"`
	Project       string            `json:"project"`
	SessionID     string            `json:"session_id"`
	Events        int               `json:"events"`
	Excluded      bool              `json:"excluded,omitempty"`
	ExcludeReason string            `json:"exclude_reason,omitempty"`
	Stats         ingest.ParseStats `json:"stats"`
	Warnings      []ingest.Warning  `json:"warnings,omitempty"`
}

type providerTotals struct {
	Files    int `json:"files"`
	Events   int `json:"events"`
	Excluded int `json:"excluded"`
}

type ingestTotals struct {
	Files        int            `json:"files"`
	Events       int            `json:"events"`
	Warnings     int            `json:"warnings"`
	BadJSON      int            `json:"bad_json"`
	TooLongLines int            `json:"too_long_lines"`
	Excluded     int            `json:"excluded"`
	UnknownTypes map[string]int `json:"unknown_types,omitempty"`
	IgnoredTypes map[string]int `json:"ignored_types,omitempty"`
}

func (r *ingestReport) addSession(sess *ingest.Session) {
	file := ingestFileReport{
		Source:        sess.Source,
		Path:          sess.File,
		Project:       sess.Project,
		SessionID:     sess.ID,
		Events:        len(sess.Events),
		Excluded:      sess.Excluded,
		ExcludeReason: sess.ExcludeReason,
		Stats:         sess.Stats,
		Warnings:      sess.Warnings,
	}
	r.Files = append(r.Files, file)

	pt := r.Providers[sess.Source]
	if pt == nil {
		pt = &providerTotals{}
		r.Providers[sess.Source] = pt
	}
	pt.Files++
	pt.Events += len(sess.Events)
	if sess.Excluded {
		pt.Excluded++
	}
}

func (r *ingestReport) computeTotals() {
	t := ingestTotals{
		UnknownTypes: map[string]int{},
		IgnoredTypes: map[string]int{},
	}
	for _, f := range r.Files {
		t.Files++
		t.Events += f.Events
		t.Warnings += len(f.Warnings) + f.Stats.WarningsDropped
		t.BadJSON += f.Stats.BadJSON
		t.TooLongLines += f.Stats.TooLongLines
		if f.Excluded {
			t.Excluded++
		}
		for k, v := range f.Stats.UnknownTypes {
			t.UnknownTypes[k] += v
		}
		for k, v := range f.Stats.IgnoredTypes {
			t.IgnoredTypes[k] += v
		}
	}
	r.Totals = t
}

func printFileSummary(w io.Writer, sess *ingest.Session) {
	status := "ok"
	if sess.Excluded {
		status = "excluded:" + sess.ExcludeReason
	}
	fmt.Fprintf(w, "[%s] %s\n", sess.Source, sess.File)
	fmt.Fprintf(w, "  project=%s session=%s events=%d warnings=%d status=%s\n",
		sess.Project, sess.ID, len(sess.Events), len(sess.Warnings), status)
	if sess.Title != "" {
		fmt.Fprintf(w, "  title=%q\n", sess.Title)
	}
	if len(sess.Stats.UnknownTypes) > 0 || len(sess.Stats.IgnoredTypes) > 0 {
		fmt.Fprintf(w, "  unknown_types=%s ignored_types=%s\n",
			formatCounts(sess.Stats.UnknownTypes), formatCounts(sess.Stats.IgnoredTypes))
	}
}

func printWarnings(w io.Writer, sess *ingest.Session, limit int) {
	if len(sess.Warnings) == 0 {
		return
	}
	if limit <= 0 || limit > len(sess.Warnings) {
		limit = len(sess.Warnings)
	}
	for _, warning := range sess.Warnings[:limit] {
		fmt.Fprintf(w, "  warning: line %d: %s\n", warning.Line, warning.Message)
	}
	if len(sess.Warnings) > limit {
		fmt.Fprintf(w, "  warning: ... %d more\n", len(sess.Warnings)-limit)
	}
	if sess.Stats.WarningsDropped > 0 {
		fmt.Fprintf(w, "  warning: %d additional warnings suppressed\n", sess.Stats.WarningsDropped)
	}
}

func printTotals(w io.Writer, r *ingestReport) {
	fmt.Fprintf(w, "\nsummary: files=%d events=%d warnings=%d bad_json=%d too_long_lines=%d excluded=%d\n",
		r.Totals.Files, r.Totals.Events, r.Totals.Warnings, r.Totals.BadJSON, r.Totals.TooLongLines, r.Totals.Excluded)
	fmt.Fprintf(w, "parser_version=%d\n", r.ParserVersion)
	if len(r.Totals.UnknownTypes) > 0 {
		fmt.Fprintf(w, "unknown_types=%s\n", formatCounts(r.Totals.UnknownTypes))
	}
	if len(r.Totals.IgnoredTypes) > 0 {
		fmt.Fprintf(w, "ignored_types=%s\n", formatCounts(r.Totals.IgnoredTypes))
	}
}

func formatCounts(counts map[string]int) string {
	if len(counts) == 0 {
		return "-"
	}
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for i, k := range keys {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "%s=%d", k, counts[k])
	}
	return b.String()
}

func printEvents(w io.Writer, sess *ingest.Session) {
	fmt.Fprintf(w, "\nevents for %s (%s/%s):\n", sess.ID, sess.Source, sess.Project)
	for _, ev := range sess.Events {
		text := ev.Text
		switch ev.Kind {
		case ingest.KindToolResult:
			text = ev.ToolResult
		case ingest.KindToolUse:
			if len(ev.ToolInput) > 0 {
				text = string(ev.ToolInput)
			} else {
				text = ev.ToolInputText
			}
		}
		fmt.Fprintf(w, "  seq=%03d %-18s role=%-9s tool=%-12s time=%s %q%s\n",
			ev.Seq, ev.Kind, ev.Role, emptyDash(ev.ToolName), formatTime(ev.Time), truncateRunes(text, 160), execStatus(ev.Exec))
	}
}

func execStatus(ex *ingest.ExecResult) string {
	if ex == nil {
		return ""
	}
	var parts []string
	if ex.ExitCode != nil {
		parts = append(parts, fmt.Sprintf("exit=%d", *ex.ExitCode))
	}
	if ex.TimedOut {
		parts = append(parts, "timed_out")
	}
	if ex.Canceled {
		parts = append(parts, "canceled")
	}
	if !ex.Known {
		parts = append(parts, "outcome=unknown")
	}
	if len(parts) == 0 {
		return " exec=known"
	}
	return " exec=" + strings.Join(parts, ",")
}

func emptyDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.Format(time.RFC3339)
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "..."
}
