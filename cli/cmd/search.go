package main

import (
	"encoding/json"
	"fmt"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"devlog-learn/internal/cache"
	"devlog-learn/internal/search"
)

type searchOptions struct {
	cacheDir string
	outDir   string
	project  string
	review   string
	tags     []string
	limit    int
	asJSON   bool
}

func newSearchCmd() *cobra.Command {
	opt := &searchOptions{}
	cmd := &cobra.Command{
		Use:   "search [query]",
		Short: "Search active Docs from the terminal",
		Long: `Rank active Docs with the same lexical scorer used by the MCP server.
An empty query returns the most recent documents. Stale Docs are excluded.

Examples:
  devlog-learn search "timer localStorage" --project test-1
  devlog-learn search "render fail" --review approved --tag go
  devlog-learn search "" --json`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			query := ""
			if len(args) == 1 {
				query = args[0]
			}
			return runSearch(cmd, opt, query)
		},
	}
	flags := cmd.Flags()
	flags.StringVar(&opt.cacheDir, "cache-dir", ".devlog-cache", "cache directory containing docs/")
	flags.StringVar(&opt.outDir, "out", ".devlog-docs", "rendered Markdown directory used for markdown_file paths")
	flags.StringVar(&opt.project, "project", "", "project filter: empty/all, auto, or a project name")
	flags.StringVar(&opt.review, "review", "any", "review filter: any, approved, pending, or rejected")
	flags.StringArrayVar(&opt.tags, "tag", nil, "require a tag (repeatable, case-insensitive)")
	flags.IntVar(&opt.limit, "limit", 10, "maximum results (1-50)")
	flags.BoolVar(&opt.asJSON, "json", false, "print JSON instead of text")
	return cmd
}

func runSearch(cmd *cobra.Command, opt *searchOptions, query string) error {
	review, err := normalizeReviewFilter(opt.review)
	if err != nil {
		return err
	}
	if opt.limit < 1 || opt.limit > search.MaxLimit {
		return fmt.Errorf("--limit must be between 1 and %d", search.MaxLimit)
	}
	project := resolveAgentProject(opt.project)

	store := &cache.Store{Root: opt.cacheDir}
	infos, warnings := store.ListActiveDocs()
	printDryRunWarnings(cmd.ErrOrStderr(), warnings)

	results := search.Search(infos, search.Options{
		Query:   query,
		Project: project,
		Tags:    opt.tags,
		Review:  review,
		Limit:   opt.limit,
		OutDir:  opt.outDir,
	})

	stdout := cmd.OutOrStdout()
	if opt.asJSON {
		payload := struct {
			Query   string          `json:"query"`
			Project string          `json:"project,omitempty"`
			Review  string          `json:"review"`
			Count   int             `json:"count"`
			Results []search.Result `json:"results"`
		}{
			Query:   query,
			Project: project,
			Review:  review,
			Count:   len(results),
			Results: results,
		}
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(payload); err != nil {
			return err
		}
	} else {
		for _, result := range results {
			fmt.Fprintf(stdout, "%.2f\t%s\t%s\t%s\t%d/%d\t%s\n",
				result.Score, result.ID, result.Project, result.ReviewStatus,
				result.EvidenceVerified, result.EvidenceChecked, result.Title)
			if result.MarkdownPath != "" {
				fmt.Fprintf(stdout, "      path: %s\n", result.MarkdownPath)
			}
		}
	}
	fmt.Fprintf(cmd.ErrOrStderr(), "search: query=%q project=%s review=%s results=%d\n",
		query, displayProject(project), review, len(results))
	return nil
}

type projectsOptions struct {
	cacheDir string
	asJSON   bool
}

func newProjectsCmd() *cobra.Command {
	opt := &projectsOptions{}
	cmd := &cobra.Command{
		Use:   "projects",
		Short: "List projects and their review counts",
		Long:  `Summarize active Docs by project. Stale Docs are excluded.`,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runProjects(cmd, opt)
		},
	}
	cmd.Flags().StringVar(&opt.cacheDir, "cache-dir", ".devlog-cache", "cache directory containing docs/")
	cmd.Flags().BoolVar(&opt.asJSON, "json", false, "print JSON instead of text")
	return cmd
}

func runProjects(cmd *cobra.Command, opt *projectsOptions) error {
	store := &cache.Store{Root: opt.cacheDir}
	infos, warnings := store.ListActiveDocs()
	printDryRunWarnings(cmd.ErrOrStderr(), warnings)
	projects := search.ListProjects(infos)

	stdout := cmd.OutOrStdout()
	if opt.asJSON {
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(struct {
			Count    int              `json:"count"`
			Projects []search.Project `json:"projects"`
		}{Count: len(projects), Projects: projects})
	}
	writer := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(writer, "PROJECT\tDOCS\tAPPROVED\tPENDING\tREJECTED")
	total := 0
	for _, project := range projects {
		total += project.Docs
		fmt.Fprintf(writer, "%s\t%d\t%d\t%d\t%d\n",
			project.Name, project.Docs, project.Approved, project.Pending, project.Rejected)
	}
	if err := writer.Flush(); err != nil {
		return err
	}
	fmt.Fprintf(cmd.ErrOrStderr(), "projects: projects=%d docs=%d\n", len(projects), total)
	return nil
}
