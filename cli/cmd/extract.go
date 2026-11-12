package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"

	"devlog-learn/internal/config"
	"devlog-learn/internal/extract"
	"devlog-learn/internal/pipeline"
	"devlog-learn/internal/redact"
	"devlog-learn/internal/segment"
)

type extractOptions struct {
	provider         string
	claudeDir        string
	codexDir         string
	since            string
	includeSubagents bool
	limit            int
	segments         int
	sessions         []string
	segmentIDs       []string

	redactConfig    string
	lang            string
	model           string
	claudeBin       string
	codexBin        string
	timeout         time.Duration
	maxInputBytes   int
	includeThinking bool
	keepRaw         bool
	rawDir          string
	outDir          string

	cacheDir    string
	cursorPath  string
	full        bool
	force       bool
	concurrency int
}

func newExtractCmd() *cobra.Command {
	opt := &extractOptions{}
	cmd := &cobra.Command{
		Use:   "extract",
		Short: "Extract segments into structured Doc JSON with incremental cache",
		Long: `Run the configured local Claude Code or Codex CLI on prepared segments.

Supported providers: claude-code, codex, all.
Cursor is not supported.

Extraction is incremental: unchanged session files are skipped using a
SHA-256 cursor, and repeated segment content reuses a versioned cache entry
instead of calling the model again.

  default   incremental files, cursor + cache enabled
  --full    scan every file (ignore cursor), still reuse cache entries
  --force   scan every file and ignore cache, forcing fresh model calls

By default only ONE segment is selected because each new call costs model
tokens; use --segments 0 for every prepared segment. To target specific
sessions or segments, use --session / --segment (see "devlog-learn sessions");
explicit selection ignores --segments. JSON documents are printed to stdout,
or written to --out.

When --provider=all, each segment is sent to the CLI matching its source.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runExtract(cmd, opt)
		},
	}
	addExtractionFlags(cmd, opt)
	cmd.Flags().StringVar(&opt.outDir, "out", "", "write Doc JSON files here instead of stdout")
	return cmd
}

// addExtractionFlags registers the flags shared by extract, run and wizard.
// Commands add their own --out because its meaning differs.
func addExtractionFlags(cmd *cobra.Command, opt *extractOptions) {
	flags := cmd.Flags()
	flags.StringVar(&opt.provider, "provider", "claude-code", "log/LLM provider: claude-code, codex, or all (Cursor is not supported)")
	flags.StringVar(&opt.claudeDir, "claude-dir", "", "Claude Code projects directory (default ~/.claude/projects)")
	flags.StringVar(&opt.codexDir, "codex-dir", "", "Codex sessions directory (default ~/.codex/sessions)")
	flags.StringVar(&opt.since, "since", "7d", "only read files modified after this cutoff: 7d, 2w, 36h, RFC3339, or all")
	flags.BoolVar(&opt.includeSubagents, "include-subagents", false, "include Claude sidechains and Codex subagent sessions")
	flags.IntVar(&opt.limit, "limit", 0, "maximum files per provider; 0 means no limit")
	flags.IntVar(&opt.segments, "segments", 1, "number of segments to select; 0 means all (ignored with --session/--segment)")
	flags.StringArrayVar(&opt.sessions, "session", nil, "select a session by selector from \"sessions\" (repeatable)")
	flags.StringArrayVar(&opt.segmentIDs, "segment", nil, "select a prepared segment by id from \"sessions\" (repeatable)")
	flags.StringVar(&opt.redactConfig, "redact-config", "", "JSON file with additional redaction rules/options")
	flags.StringVar(&opt.lang, "lang", "zh", "output language: zh or en")
	flags.StringVar(&opt.model, "model", "", "model passed to the local CLI (empty means CLI default)")
	flags.StringVar(&opt.claudeBin, "claude-bin", "", "override the claude executable path")
	flags.StringVar(&opt.codexBin, "codex-bin", "", "override the codex executable path")
	flags.DurationVar(&opt.timeout, "timeout", 180*time.Second, "timeout for each model call")
	flags.IntVar(&opt.maxInputBytes, "max-input-bytes", 120000, "maximum prompt size in bytes")
	flags.BoolVar(&opt.includeThinking, "include-thinking", false, "include assistant thinking in prompts")
	flags.BoolVar(&opt.keepRaw, "keep-raw", false, "save redacted raw model responses on parse failure")
	flags.StringVar(&opt.rawDir, "raw-dir", "", "directory for redacted raw failures (default <cache-dir>/raw)")
	flags.StringVar(&opt.cacheDir, "cache-dir", ".devlog-cache", "cache directory for entries/docs/segments/raw")
	flags.StringVar(&opt.cursorPath, "cursor", ".devlog-cursor.json", "incremental file fingerprint cursor")
	flags.BoolVar(&opt.full, "full", false, "scan all files, ignoring the cursor, but reuse cache entries")
	flags.BoolVar(&opt.force, "force", false, "force full scanning and bypass the cache (implies --full)")
	flags.IntVar(&opt.concurrency, "concurrency", 2, "number of concurrent model extractions")
}

func validateExtractOptions(cmd *cobra.Command, opt *extractOptions) error {
	provider, err := config.ValidateProvider(opt.provider)
	if err != nil {
		return err
	}
	opt.provider = provider

	if opt.limit < 0 {
		return fmt.Errorf("--limit must be zero or positive")
	}
	if opt.segments < 0 {
		return fmt.Errorf("--segments must be zero or positive")
	}
	if opt.concurrency < 1 {
		return fmt.Errorf("--concurrency must be at least 1")
	}
	lang := strings.ToLower(strings.TrimSpace(opt.lang))
	if lang != "zh" && lang != "en" {
		return fmt.Errorf("--lang must be zh or en")
	}
	opt.lang = lang

	hasSelection := len(opt.sessions) > 0 || len(opt.segmentIDs) > 0
	if hasSelection && cmd.Flags().Changed("segments") {
		return fmt.Errorf("--segments cannot be combined with --session or --segment")
	}
	return nil
}

func (opt *extractOptions) cutoffAndRedact() (time.Time, redact.Config, error) {
	cutoff, err := config.ParseSince(opt.since, time.Now())
	if err != nil {
		return time.Time{}, redact.Config{}, err
	}
	redactCfg := redact.DefaultConfig()
	if opt.redactConfig != "" {
		redactCfg, err = redact.LoadConfig(opt.redactConfig)
		if err != nil {
			return time.Time{}, redact.Config{}, err
		}
	}
	return cutoff, redactCfg, nil
}

// newClientFactory builds provider clients lazily and caches them for one run.
func newClientFactory(opt *extractOptions) pipeline.ClientFor {
	clients := map[string]extract.Client{}
	var mu sync.Mutex
	return func(ctx context.Context, source string) (extract.Client, error) {
		provider := source
		if provider == "" {
			if opt.provider == "all" {
				return nil, fmt.Errorf("segment has no source provider")
			}
			provider = opt.provider
		}
		mu.Lock()
		if client, ok := clients[provider]; ok {
			mu.Unlock()
			return client, nil
		}
		mu.Unlock()

		bin := ""
		switch provider {
		case "claude-code":
			bin = opt.claudeBin
		case "codex":
			bin = opt.codexBin
		default:
			return nil, fmt.Errorf("unsupported segment source %q", provider)
		}
		client, err := extract.NewCLIClient(extract.CLIOptions{
			Provider: provider,
			Bin:      bin,
			Model:    opt.model,
			Timeout:  opt.timeout,
		})
		if err != nil {
			return nil, err
		}
		mu.Lock()
		clients[provider] = client
		mu.Unlock()
		return client, nil
	}
}

func (opt *extractOptions) pipelineRunOptions(cutoff time.Time, redactCfg redact.Config, logger *slog.Logger) pipeline.RunOptions {
	return pipeline.RunOptions{
		Load: pipeline.LoadOptions{
			Provider:         opt.provider,
			ClaudeDir:        opt.claudeDir,
			CodexDir:         opt.codexDir,
			Since:            cutoff,
			IncludeSubagents: opt.includeSubagents,
			Limit:            opt.limit,
		},
		Segment:     segment.DefaultOptions(),
		Redact:      redactCfg,
		Build:       extract.BuildOptions{MaxInputBytes: opt.maxInputBytes, IncludeThinking: opt.includeThinking, Language: opt.lang},
		CacheDir:    opt.cacheDir,
		CursorPath:  opt.cursorPath,
		Full:        opt.full,
		Force:       opt.force,
		Concurrency: opt.concurrency,
		Segments:    opt.segments,
		Selection:   pipeline.Selection{Sessions: opt.sessions, Segments: opt.segmentIDs},
		ClientFor:   newClientFactory(opt),
		RawDir:      opt.rawDir,
		KeepRaw:     opt.keepRaw,
		Logger:      logger,
		Now:         time.Now,
	}
}

func runExtract(cmd *cobra.Command, opt *extractOptions) error {
	if err := validateExtractOptions(cmd, opt); err != nil {
		return err
	}
	cutoff, redactCfg, err := opt.cutoffAndRedact()
	if err != nil {
		return err
	}

	stderr := cmd.ErrOrStderr()
	stdout := cmd.OutOrStdout()
	logger := newLogger()

	result, runErr := pipeline.Run(cmd.Context(), opt.pipelineRunOptions(cutoff, redactCfg, logger))
	if result == nil {
		return runErr
	}
	printDryRunWarnings(stderr, result.Warnings)

	if len(result.Reports) == 0 {
		fmt.Fprintf(stderr, "no segments to extract (files=%d parsed=%d skipped=%d)\n",
			result.Files, result.Parsed, result.Skipped)
	}
	docs := printExtractionReports(stderr, result)

	var errs []error
	if runErr != nil {
		errs = append(errs, runErr)
	}

	if opt.outDir != "" {
		if err := os.MkdirAll(opt.outDir, 0o755); err != nil {
			return errors.Join(append(errs, fmt.Errorf("create output directory: %w", err))...)
		}
		for _, doc := range docs {
			path := filepath.Join(opt.outDir, doc.ID+".json")
			data, err := json.MarshalIndent(doc, "", "  ")
			if err != nil {
				errs = append(errs, fmt.Errorf("encode doc %s: %w", doc.ID, err))
				continue
			}
			data = append(data, '\n')
			if err := os.WriteFile(path, data, 0o644); err != nil {
				errs = append(errs, fmt.Errorf("write doc %s: %w", path, err))
				continue
			}
			fmt.Fprintln(stdout, path)
		}
	} else {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(docs); err != nil {
			errs = append(errs, fmt.Errorf("write docs: %w", err))
		}
	}

	printExtractionSummary(stderr, result)
	return errors.Join(errs...)
}

// printExtractionReports prints one line per segment and returns the documents
// that are safe to emit (extracted or cache hits).
func printExtractionReports(w io.Writer, result *pipeline.RunResult) []*extract.Doc {
	docs := make([]*extract.Doc, 0, len(result.Reports))
	for i, report := range result.Reports {
		seg := report.Segment
		mode := "extract"
		if report.Cached {
			mode = "cache"
		}
		fmt.Fprintf(w, "[%d/%d] %s segment=%s project=%s session=%s events=%d bytes=%d\n",
			i+1, len(result.Reports), mode, seg.ID, seg.Project, seg.SessionID, len(seg.Events), report.Bytes)
		switch {
		case report.Err != nil:
			fmt.Fprintf(w, "  error: %v\n", report.Err)
		case report.Cached:
			fmt.Fprintf(w, "  cached title=%q evidence=%d/%d confidence=%.2f\n",
				report.Doc.Title,
				report.Doc.Validation.EvidenceVerified,
				report.Doc.Validation.EvidenceChecked,
				report.Doc.Confidence)
			fmt.Fprintln(w, "  usage: cached (no model call)")
			docs = append(docs, report.Doc)
		case report.Doc != nil:
			fmt.Fprintf(w, "  ok title=%q evidence=%d/%d confidence=%.2f repairs=%d prompt_bytes=%d\n",
				report.Doc.Title,
				report.Doc.Validation.EvidenceVerified,
				report.Doc.Validation.EvidenceChecked,
				report.Doc.Confidence,
				report.Repairs,
				report.PromptBytes)
			fmt.Fprintf(w, "  usage: %s\n", formatUsage(report.Usage))
			docs = append(docs, report.Doc)
		}
	}
	return docs
}

func printExtractionSummary(w io.Writer, result *pipeline.RunResult) {
	fmt.Fprintf(w, "summary: files=%d skipped=%d parsed=%d excluded=%d segments=%d selected=%d cached=%d extracted=%d failed_segments=%d failed_files=%d stale=%d restored=%d source_missing=%d\n",
		result.Files, result.Skipped, result.Parsed, result.Excluded,
		result.Segments, result.Selected, result.CacheHits, result.Extracted,
		result.FailedSegments, result.FailedFiles, len(result.StaleMarked), len(result.Restored), len(result.SourceMissing))
	fmt.Fprintf(w, "tokens: %s\n", summarizeUsage(result))
}

// formatUsage renders one provider's token accounting. Cached input tokens are
// shown separately and the provider total wins when supplied, so cached input
// is never double counted.
func formatUsage(usage extract.Usage) string {
	if !usage.Known && usage.TotalTokens() == 0 && usage.CostUSD == 0 {
		return "unknown (provider did not report tokens)"
	}
	parts := make([]string, 0, 6)
	if usage.InputTokens > 0 {
		parts = append(parts, fmt.Sprintf("in=%d", usage.InputTokens))
	}
	if usage.OutputTokens > 0 {
		parts = append(parts, fmt.Sprintf("out=%d", usage.OutputTokens))
	}
	if usage.CacheCreationInputTokens > 0 {
		parts = append(parts, fmt.Sprintf("cache_write=%d", usage.CacheCreationInputTokens))
	}
	if usage.CacheReadInputTokens > 0 {
		parts = append(parts, fmt.Sprintf("cache_read=%d", usage.CacheReadInputTokens))
	}
	if usage.TotalTokens() > 0 {
		parts = append(parts, fmt.Sprintf("total=%d", usage.TotalTokens()))
	}
	if usage.CostUSD > 0 {
		parts = append(parts, "cost_usd="+strconv.FormatFloat(usage.CostUSD, 'f', -1, 64))
	}
	if len(parts) == 0 {
		return "unknown (provider did not report tokens)"
	}
	return strings.Join(parts, " ")
}

func summarizeUsage(result *pipeline.RunResult) string {
	if result == nil {
		return "unknown"
	}
	if result.Extracted == 0 && result.Usage.TotalTokens() == 0 && !result.Usage.Known {
		return "0 (no model calls)"
	}
	if !result.Usage.Known && result.Usage.TotalTokens() == 0 {
		return fmt.Sprintf("unknown (%d extracted segment(s) did not report usage)", result.Extracted)
	}
	text := formatUsage(result.Usage)
	if result.UsageUnknownReports > 0 {
		text += fmt.Sprintf(" (%d extracted segment(s) did not report usage)", result.UsageUnknownReports)
	}
	return text
}
