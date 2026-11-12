package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"devlog-learn/internal/pipeline"
	"devlog-learn/internal/segment"
)

var errWizardQuit = errors.New("wizard canceled")

type wizardOptions struct {
	extractOptions
	yes bool
}

func newWizardCmd() *cobra.Command {
	opt := &wizardOptions{}
	cmd := &cobra.Command{
		Use:   "wizard",
		Short: "Interactively pick sessions, then extract and render Markdown",
		Long: `Discover and prepare sessions without calling a model, show them as a
numbered list, and let you pick whole sessions. After confirmation the normal
"run" pipeline executes: extract selected sessions, render Markdown, and build
indexes.

Use --yes to skip the confirmation prompt. In non-interactive environments,
use "sessions" plus "extract --session" instead.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runWizard(cmd, opt)
		},
	}
	addExtractionFlags(cmd, &opt.extractOptions)
	cmd.Flags().StringVar(&opt.outDir, "out", ".devlog-docs", "output directory for Markdown and indexes")
	cmd.Flags().BoolVar(&opt.yes, "yes", false, "skip the confirmation prompt")
	return cmd
}

func runWizard(cmd *cobra.Command, opt *wizardOptions) error {
	if err := validateExtractOptions(cmd, &opt.extractOptions); err != nil {
		return err
	}
	if cmd.Flags().Changed("segments") {
		return fmt.Errorf("wizard selects whole sessions; use \"extract --segments\" for count-based selection")
	}
	if strings.TrimSpace(opt.outDir) == "" {
		return fmt.Errorf("--out must not be empty")
	}
	cutoff, redactCfg, err := opt.cutoffAndRedact()
	if err != nil {
		return err
	}

	stdout := cmd.OutOrStdout()
	stderr := cmd.ErrOrStderr()

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
	printDryRunWarnings(stderr, list.Warnings)
	if runErr != nil {
		return runErr
	}
	if len(list.Sessions) == 0 {
		fmt.Fprintln(stdout, "no sessions found")
		return nil
	}

	printSessions(stdout, list)
	reader := bufio.NewReader(cmd.InOrStdin())
	input, err := readPrompt(reader, stderr, "选择会话编号（如 1,3 或 1-3；all=全部；q=退出）: ")
	if err != nil {
		return promptError(err)
	}
	indexes, err := parseSelection(input, len(list.Sessions))
	if errors.Is(err, errWizardQuit) {
		fmt.Fprintln(stderr, "canceled")
		return nil
	}
	if err != nil {
		return err
	}

	selectors := make([]string, 0, len(indexes))
	segments, already, pending := 0, 0, 0
	for _, index := range indexes {
		session := list.Sessions[index-1]
		selectors = append(selectors, session.Selector)
		segments += session.SegmentCount
		already += session.Extracted
		pending += session.SegmentCount - session.Extracted
	}
	fmt.Fprintf(stderr, "selected %d session(s), %d segment(s); %d already extracted, %d expected model call(s)\n",
		len(indexes), segments, already, pending)

	if !opt.yes {
		answer, err := readPrompt(reader, stderr, "继续？[y/N]: ")
		if err != nil {
			return promptError(err)
		}
		if !isYes(answer) {
			fmt.Fprintln(stderr, "canceled")
			return nil
		}
	}

	opt.sessions = selectors
	opt.segmentIDs = nil
	return runRun(cmd, &runOptions{extractOptions: opt.extractOptions})
}

// parseSelection accepts "1,3", "1-3", "all"/"*", or q/empty to cancel.
func parseSelection(input string, count int) ([]int, error) {
	value := strings.ToLower(strings.TrimSpace(input))
	if value == "" || value == "q" || value == "quit" {
		return nil, errWizardQuit
	}
	if value == "all" || value == "*" {
		out := make([]int, count)
		for i := range out {
			out[i] = i + 1
		}
		return out, nil
	}

	chosen := make(map[int]bool)
	parts := strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t'
	})
	for _, part := range parts {
		if part == "" {
			continue
		}
		if strings.Contains(part, "-") {
			bounds := strings.SplitN(part, "-", 2)
			first, errA := strconv.Atoi(strings.TrimSpace(bounds[0]))
			last, errB := strconv.Atoi(strings.TrimSpace(bounds[1]))
			if errA != nil || errB != nil || first < 1 || last < first || last > count {
				return nil, fmt.Errorf("invalid range %q; expected 1-%d", part, count)
			}
			for number := first; number <= last; number++ {
				chosen[number] = true
			}
			continue
		}
		number, err := strconv.Atoi(part)
		if err != nil || number < 1 || number > count {
			return nil, fmt.Errorf("invalid session number %q; expected 1-%d", part, count)
		}
		chosen[number] = true
	}
	if len(chosen) == 0 {
		return nil, fmt.Errorf("no session numbers selected")
	}
	out := make([]int, 0, len(chosen))
	for number := range chosen {
		out = append(out, number)
	}
	sort.Ints(out)
	return out, nil
}

func readPrompt(reader *bufio.Reader, out io.Writer, prompt string) (string, error) {
	fmt.Fprint(out, prompt)
	line, err := reader.ReadString('\n')
	if err != nil {
		if errors.Is(err, io.EOF) && strings.TrimSpace(line) != "" {
			return strings.TrimSpace(line), nil
		}
		return "", err
	}
	return strings.TrimSpace(line), nil
}

func promptError(err error) error {
	if errors.Is(err, io.EOF) {
		return fmt.Errorf("no interactive input; use \"devlog-learn sessions\" and \"extract --session\" in scripts")
	}
	return err
}

func isYes(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "y", "yes", "是":
		return true
	}
	return false
}
