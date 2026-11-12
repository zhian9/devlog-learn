package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"devlog-learn/internal/cache"
	"devlog-learn/internal/questions"
)

type questionsOptions struct {
	cacheDir string
	project  string
	review   string
	docID    string
	status   string
	asJSON   bool
	note     string
}

func newQuestionsCmd() *cobra.Command {
	opt := &questionsOptions{}
	cmd := &cobra.Command{
		Use:   "questions",
		Short: "List open questions across Docs, or mark one resolved",
		Long: `List the open questions extracted into Doc memory and track which ones a
human has resolved. Resolution state is stored in <cache-dir>/questions.json,
keyed by Doc ID plus the normalized question text, so re-extraction does not
lose it.

By default only open questions are listed. The digest command omits resolved
questions from the agent-facing AGENTS.md.

Examples:
  devlog-learn questions
  devlog-learn questions --project cli --all
  devlog-learn questions resolve q_ab12cd34ef56 --note "fixed in v0.9"
  devlog-learn questions reopen q_ab12cd34ef56`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runQuestionList(cmd, opt)
		},
	}
	cmd.PersistentFlags().StringVar(&opt.cacheDir, "cache-dir", ".devlog-cache", "cache directory containing docs/ and questions.json")
	flags := cmd.Flags()
	flags.StringVar(&opt.project, "project", "", "project filter: empty/all, auto, or a project name")
	flags.StringVar(&opt.review, "review", "any", "review filter: any, approved, pending, or rejected")
	flags.StringVar(&opt.docID, "doc", "", "only questions from this Doc ID")
	flags.StringVar(&opt.status, "status", "open", "question status: open, resolved, or all")
	flags.BoolVar(&opt.asJSON, "json", false, "print JSON instead of text")

	resolve := &cobra.Command{
		Use:   "resolve <question-id>",
		Short: "Mark one open question resolved",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runQuestionResolve(cmd, opt, args[0])
		},
	}
	resolve.Flags().StringVar(&opt.note, "note", "", "free-form resolution note")
	cmd.AddCommand(resolve)

	reopen := &cobra.Command{
		Use:   "reopen <question-id>",
		Short: "Remove a resolution and show the question as open again",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runQuestionReopen(cmd, opt, args[0])
		},
	}
	cmd.AddCommand(reopen)
	return cmd
}

func runQuestionList(cmd *cobra.Command, opt *questionsOptions) error {
	review, err := normalizeReviewFilter(opt.review)
	if err != nil {
		return err
	}
	if !questions.ValidStatus(opt.status) {
		return fmt.Errorf("invalid --status %q (want open, resolved, or all)", opt.status)
	}
	infos, state, warnings, err := loadQuestionContext(opt.cacheDir)
	if err != nil {
		return err
	}
	project := resolveAgentProject(opt.project)
	entries := questions.Collect(infos, state, questions.Filter{
		Project: project,
		Review:  review,
		DocID:   opt.docID,
		Status:  opt.status,
	})
	printDryRunWarnings(cmd.ErrOrStderr(), warnings)

	stdout := cmd.OutOrStdout()
	if opt.asJSON {
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(struct {
			Count     int               `json:"count"`
			Questions []questions.Entry `json:"questions"`
		}{Count: len(entries), Questions: entries}); err != nil {
			return err
		}
	} else {
		writer := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(writer, "STATUS\tID\tPROJECT\tDOC\tQUESTION")
		for _, entry := range entries {
			question := entry.Question
			if entry.Note != "" {
				question += "  (note: " + entry.Note + ")"
			}
			fmt.Fprintf(writer, "%s\t%s\t%s\t%s\t%s\n",
				entry.Status, entry.ID, emptyDash(entry.Project), entry.DocID, question)
		}
		if err := writer.Flush(); err != nil {
			return err
		}
		if len(entries) == 0 {
			fmt.Fprintln(stdout, "no matching questions")
		}
	}
	open, resolved := 0, 0
	for _, entry := range entries {
		if entry.Status == questions.StatusResolved {
			resolved++
		} else {
			open++
		}
	}
	fmt.Fprintf(cmd.ErrOrStderr(), "questions: status=%s open=%d resolved=%d\n",
		strings.ToLower(strings.TrimSpace(opt.status)), open, resolved)
	return nil
}

func runQuestionResolve(cmd *cobra.Command, opt *questionsOptions, id string) error {
	lock, err := cache.TryLock(opt.cacheDir)
	if err != nil {
		return err
	}
	defer lock.Close()
	infos, state, warnings, err := loadQuestionContext(opt.cacheDir)
	if err != nil {
		return err
	}
	printDryRunWarnings(cmd.ErrOrStderr(), warnings)
	entry, err := findQuestion(infos, state, id)
	if err != nil {
		return err
	}
	now := time.Now()
	note := strings.TrimSpace(opt.note)
	if !cmd.Flags().Changed("note") && entry.Status == questions.StatusResolved {
		note = entry.Note
	}
	state.Resolve(entry, note, now)
	if err := state.Save(questions.Path(opt.cacheDir), now); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "resolved %s doc=%s\n", entry.ID, entry.DocID)
	if note != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "note: %s\n", note)
	}
	return nil
}

func runQuestionReopen(cmd *cobra.Command, opt *questionsOptions, id string) error {
	lock, err := cache.TryLock(opt.cacheDir)
	if err != nil {
		return err
	}
	defer lock.Close()
	_, state, warnings, err := loadQuestionContext(opt.cacheDir)
	if err != nil {
		return err
	}
	printDryRunWarnings(cmd.ErrOrStderr(), warnings)
	id = strings.TrimSpace(id)
	if !state.Reopen(id) {
		return fmt.Errorf("question %s is not marked resolved", id)
	}
	if err := state.Save(questions.Path(opt.cacheDir), time.Now()); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "reopened %s\n", id)
	return nil
}

func loadQuestionContext(cacheDir string) ([]cache.DocInfo, *questions.State, []string, error) {
	store := &cache.Store{Root: cacheDir}
	infos, warnings := store.ListActiveDocs()
	state, err := questions.Load(questions.Path(cacheDir))
	if err != nil {
		return nil, nil, warnings, err
	}
	return infos, state, warnings, nil
}

func findQuestion(infos []cache.DocInfo, state *questions.State, id string) (questions.Entry, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return questions.Entry{}, fmt.Errorf("question id must not be empty")
	}
	for _, entry := range questions.Collect(infos, state, questions.Filter{Status: questions.StatusAll}) {
		if entry.ID == id {
			return entry, nil
		}
	}
	return questions.Entry{}, fmt.Errorf("question %s not found in active Docs (it may belong to a stale Doc or have been removed by re-extraction)", id)
}
