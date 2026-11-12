package main

import (
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"devlog-learn/internal/cache"
	"devlog-learn/internal/cursor"
	"devlog-learn/internal/extract"
	"devlog-learn/internal/questions"
	"devlog-learn/internal/version"
)

type statusOptions struct {
	cacheDir  string
	cursorRef string
	outDir    string
	claudeDir string
	codexDir  string
	claudeBin string
	codexBin  string
	asJSON    bool
}

type statusReport struct {
	ToolVersion   string           `json:"tool_version"`
	SchemaVersion int              `json:"schema_version"`
	Cache         statusCache      `json:"cache"`
	Questions     statusQuestions  `json:"questions"`
	Cursor        statusCursor     `json:"cursor"`
	Outputs       statusOutputs    `json:"outputs"`
	Providers     []statusProvider `json:"providers"`
	Warnings      []string         `json:"warnings,omitempty"`
}

type statusCache struct {
	Dir       string `json:"dir"`
	Exists    bool   `json:"exists"`
	SizeBytes int64  `json:"size_bytes"`
	Active    int    `json:"active_docs"`
	Stale     int    `json:"stale_docs"`
	Pending   int    `json:"pending"`
	Approved  int    `json:"approved"`
	Rejected  int    `json:"rejected"`
}

type statusQuestions struct {
	Open     int `json:"open"`
	Resolved int `json:"resolved"`
}

type statusCursor struct {
	Path          string     `json:"path"`
	Exists        bool       `json:"exists"`
	UpdatedAt     *time.Time `json:"updated_at,omitempty"`
	Files         int        `json:"files"`
	Errors        int        `json:"errors"`
	VersionsMatch bool       `json:"versions_match"`
}

type statusOutputs struct {
	Dir              string     `json:"dir"`
	IndexExists      bool       `json:"index_exists"`
	IndexGeneratedAt *time.Time `json:"index_generated_at,omitempty"`
	IndexStale       bool       `json:"index_stale"`
	DigestExists     bool       `json:"digest_exists"`
	DigestModifiedAt *time.Time `json:"digest_modified_at,omitempty"`
	DigestStale      bool       `json:"digest_stale"`
}

type statusProvider struct {
	Name         string `json:"name"`
	Bin          string `json:"bin"`
	BinFound     bool   `json:"bin_found"`
	Dir          string `json:"dir"`
	DirExists    bool   `json:"dir_exists"`
	SessionFiles int    `json:"session_files"`
}

func newStatusCmd() *cobra.Command {
	opt := &statusOptions{}
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show a read-only health report for cache, cursor, and outputs",
		Long: `Report cache size, active/stale Doc counts, review and open-question
counts, cursor state, output freshness, and whether the claude/codex binaries
and session directories are present.

This command is read-only: it never creates the cache directory, writes cache
or cursor files, or calls a model.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runStatus(cmd, opt)
		},
	}
	flags := cmd.Flags()
	flags.StringVar(&opt.cacheDir, "cache-dir", ".devlog-cache", "cache directory containing docs/")
	flags.StringVar(&opt.cursorRef, "cursor", ".devlog-cursor.json", "cursor file to inspect")
	flags.StringVar(&opt.outDir, "out", ".devlog-docs", "rendered output directory to inspect")
	flags.StringVar(&opt.claudeDir, "claude-dir", "", "Claude Code projects directory (default ~/.claude/projects)")
	flags.StringVar(&opt.codexDir, "codex-dir", "", "Codex sessions directory (default ~/.codex/sessions)")
	flags.StringVar(&opt.claudeBin, "claude-bin", "claude", "Claude Code executable to probe")
	flags.StringVar(&opt.codexBin, "codex-bin", "codex", "Codex executable to probe")
	flags.BoolVar(&opt.asJSON, "json", false, "print JSON instead of text")
	return cmd
}

func runStatus(cmd *cobra.Command, opt *statusOptions) error {
	report := statusReport{
		ToolVersion:   version.ToolVersion,
		SchemaVersion: version.SchemaVersion,
		Cache:         statusCache{Dir: opt.cacheDir},
		Cursor:        statusCursor{Path: opt.cursorRef},
		Outputs:       statusOutputs{Dir: opt.outDir},
	}

	store := &cache.Store{Root: opt.cacheDir}
	allInfos, listWarnings := store.ListDocs()
	report.Warnings = append(report.Warnings, listWarnings...)

	active := make([]cache.DocInfo, 0, len(allInfos))
	var newestDoc *time.Time
	for _, info := range allInfos {
		if info.Doc == nil {
			continue
		}
		if info.Doc.Lifecycle == extract.LifecycleStale {
			report.Cache.Stale++
			continue
		}
		active = append(active, info)
		report.Cache.Active++
		switch strings.ToLower(strings.TrimSpace(info.Doc.ReviewStatus)) {
		case extract.ReviewApproved:
			report.Cache.Approved++
		case extract.ReviewRejected:
			report.Cache.Rejected++
		default:
			report.Cache.Pending++
		}
		if stat, err := os.Stat(info.Path); err == nil {
			if newestDoc == nil || stat.ModTime().After(*newestDoc) {
				modified := stat.ModTime()
				newestDoc = &modified
			}
		}
	}

	if info, err := os.Stat(opt.cacheDir); err == nil && info.IsDir() {
		report.Cache.Exists = true
		report.Cache.SizeBytes = dirSize(opt.cacheDir)
	}

	state, err := questions.Load(questions.Path(opt.cacheDir))
	if err != nil {
		report.Warnings = append(report.Warnings, err.Error())
		state = questions.New()
	}
	for _, entry := range questions.Collect(active, state, questions.Filter{Status: questions.StatusAll}) {
		if entry.Status == questions.StatusResolved {
			report.Questions.Resolved++
		} else {
			report.Questions.Open++
		}
	}

	report.Cursor = inspectCursor(opt.cursorRef, &report.Warnings)
	report.Outputs = inspectOutputs(opt.outDir, newestDoc)

	home, _ := os.UserHomeDir()
	claudeDir := opt.claudeDir
	if strings.TrimSpace(claudeDir) == "" {
		claudeDir = filepath.Join(home, ".claude", "projects")
	}
	codexDir := opt.codexDir
	if strings.TrimSpace(codexDir) == "" {
		codexDir = filepath.Join(home, ".codex", "sessions")
	}
	report.Providers = []statusProvider{
		inspectProvider("claude-code", opt.claudeBin, claudeDir, false),
		inspectProvider("codex", opt.codexBin, codexDir, true),
	}

	printDryRunWarnings(cmd.ErrOrStderr(), report.Warnings)
	stdout := cmd.OutOrStdout()
	if opt.asJSON {
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(report)
	}
	printStatus(stdout, report)
	return nil
}

func inspectCursor(path string, warnings *[]string) statusCursor {
	out := statusCursor{Path: path}
	if strings.TrimSpace(path) == "" {
		return out
	}
	if _, err := os.Stat(path); err == nil {
		out.Exists = true
	}
	state, err := cursor.Load(path)
	if err != nil {
		*warnings = append(*warnings, err.Error())
		return out
	}
	out.Files = len(state.Files)
	for _, fp := range state.Files {
		if fp.Status != cursor.StatusOK {
			out.Errors++
		}
	}
	out.VersionsMatch = state.VersionsMatch()
	if !state.UpdatedAt.IsZero() {
		updated := state.UpdatedAt
		out.UpdatedAt = &updated
	}
	return out
}

func inspectOutputs(outDir string, newestDoc *time.Time) statusOutputs {
	out := statusOutputs{Dir: outDir}

	indexPath := filepath.Join(outDir, "index.json")
	if stat, err := os.Stat(indexPath); err == nil {
		out.IndexExists = true
		generated := stat.ModTime().UTC()
		if data, readErr := os.ReadFile(indexPath); readErr == nil {
			var index struct {
				GeneratedAt time.Time `json:"generated_at"`
			}
			if json.Unmarshal(data, &index) == nil && !index.GeneratedAt.IsZero() {
				generated = index.GeneratedAt
			}
		}
		out.IndexGeneratedAt = &generated
		out.IndexStale = newestDoc != nil && generated.Before(*newestDoc)
	}

	digestPath := filepath.Join(outDir, "AGENTS.md")
	if stat, err := os.Stat(digestPath); err == nil {
		out.DigestExists = true
		modified := stat.ModTime().UTC()
		out.DigestModifiedAt = &modified
		out.DigestStale = newestDoc != nil && modified.Before(*newestDoc)
	}
	return out
}

func inspectProvider(name, bin, dir string, codex bool) statusProvider {
	out := statusProvider{Name: name, Bin: bin, Dir: dir}
	if strings.TrimSpace(bin) != "" {
		if _, err := exec.LookPath(bin); err == nil {
			out.BinFound = true
		}
	}
	if info, err := os.Stat(dir); err == nil && info.IsDir() {
		out.DirExists = true
		out.SessionFiles = countSessionFiles(dir, codex)
	}
	return out
}

func countSessionFiles(root string, codex bool) int {
	count := 0
	_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return nil
		}
		if !strings.EqualFold(filepath.Ext(path), ".jsonl") {
			return nil
		}
		if codex && !strings.HasPrefix(filepath.Base(path), "rollout-") {
			return nil
		}
		count++
		return nil
	})
	return count
}

func dirSize(root string) int64 {
	var total int64
	_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return nil
		}
		if info, statErr := entry.Info(); statErr == nil {
			total += info.Size()
		}
		return nil
	})
	return total
}

func printStatus(out io.Writer, report statusReport) {
	w := out
	line := func(format string, args ...any) {
		_, _ = fmt.Fprintf(w, format+"\n", args...)
	}
	line("devlog-learn %s (schema %d)", report.ToolVersion, report.SchemaVersion)
	line("cache     %s  %s  size=%s", report.Cache.Dir, yesNo(report.Cache.Exists), statusBytes(report.Cache.SizeBytes))
	line("docs      active=%d stale=%d", report.Cache.Active, report.Cache.Stale)
	line("review    pending=%d approved=%d rejected=%d", report.Cache.Pending, report.Cache.Approved, report.Cache.Rejected)
	line("questions open=%d resolved=%d", report.Questions.Open, report.Questions.Resolved)
	line("cursor    %s  %s  updated=%s  files=%d errors=%d versions=%s",
		report.Cursor.Path, yesNo(report.Cursor.Exists), formatTimePtr(report.Cursor.UpdatedAt),
		report.Cursor.Files, report.Cursor.Errors, currentOrNot(report.Cursor.VersionsMatch))
	line("outputs   %s  index=%s updated=%s stale=%s  digest=%s updated=%s stale=%s",
		report.Outputs.Dir,
		yesNo(report.Outputs.IndexExists), formatTimePtr(report.Outputs.IndexGeneratedAt), yesNo(report.Outputs.IndexStale),
		yesNo(report.Outputs.DigestExists), formatTimePtr(report.Outputs.DigestModifiedAt), yesNo(report.Outputs.DigestStale))
	for _, provider := range report.Providers {
		line("provider  %-12s bin=%s(%s) dir=%s(%s files=%d)",
			provider.Name, provider.Bin, foundOrMissing(provider.BinFound),
			provider.Dir, foundOrMissing(provider.DirExists), provider.SessionFiles)
	}
}

func yesNo(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}

func foundOrMissing(found bool) string {
	if found {
		return "found"
	}
	return "missing"
}

func currentOrNot(current bool) string {
	if current {
		return "current"
	}
	return "stale"
}

func formatTimePtr(value *time.Time) string {
	if value == nil {
		return "-"
	}
	return formatTime(*value)
}

func statusBytes(value int64) string {
	const unit = 1024
	if value < unit {
		return fmt.Sprintf("%d B", value)
	}
	div, exp := int64(unit), 0
	for n := value / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(value)/float64(div), "KMGTPE"[exp])
}
