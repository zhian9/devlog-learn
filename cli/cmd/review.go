package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"devlog-learn/internal/cache"
	"devlog-learn/internal/extract"
)

type reviewOptions struct {
	cacheDir string
	status   string
	note     string
	list     bool
	asJSON   bool
}

type reviewEntry struct {
	ID           string `json:"id"`
	Title        string `json:"title"`
	Project      string `json:"project"`
	ReviewStatus string `json:"review_status"`
	ReviewNote   string `json:"review_note,omitempty"`
	Lifecycle    string `json:"lifecycle"`
}

func newReviewCmd() *cobra.Command {
	opt := &reviewOptions{}
	cmd := &cobra.Command{
		Use:   "review [doc-id]",
		Short: "List or update human review status of a document",
		Long: `Set a human review decision on a canonical Doc:

  review <doc-id> --status approved --note "verified locally"
  review <doc-id>                    # print current review state
  review --list --status pending     # list documents awaiting review

Review status is one of pending, approved, rejected. Updates only rewrite the
canonical docs/<id>.json file; the segment cache and stale documents are never
deleted. Re-extraction preserves review state only when the segment content is
unchanged.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runReview(cmd, opt, args)
		},
	}
	cmd.Flags().StringVar(&opt.cacheDir, "cache-dir", ".devlog-cache", "cache directory containing docs/")
	cmd.Flags().StringVar(&opt.status, "status", "", "review status: pending, approved, or rejected")
	cmd.Flags().StringVar(&opt.note, "note", "", "free-form review note (use with --status or alone)")
	cmd.Flags().BoolVar(&opt.list, "list", false, "list documents instead of updating one")
	cmd.Flags().BoolVar(&opt.asJSON, "json", false, "print JSON instead of text")
	return cmd
}

func runReview(cmd *cobra.Command, opt *reviewOptions, args []string) error {
	stdout := cmd.OutOrStdout()
	statusChanged := cmd.Flags().Changed("status")
	if statusChanged && !extract.ValidReviewStatus(opt.status) {
		return fmt.Errorf("--status must be pending, approved, or rejected")
	}

	store, err := cache.Open(opt.cacheDir)
	if err != nil {
		return err
	}

	if opt.list {
		if len(args) > 0 {
			return fmt.Errorf("--list does not take a document id")
		}
		entries, err := listReviews(store, opt.status, cmd.ErrOrStderr())
		if err != nil {
			return err
		}
		if opt.asJSON {
			return json.NewEncoder(stdout).Encode(entries)
		}
		for _, entry := range entries {
			fmt.Fprintf(stdout, "%s\t%s\t%s\t%s\n", entry.ID, entry.ReviewStatus, entry.Project, entry.Title)
		}
		fmt.Fprintf(cmd.ErrOrStderr(), "summary: docs=%d status=%s\n", len(entries), displayStatus(opt.status))
		return nil
	}

	if len(args) != 1 {
		return fmt.Errorf("review requires a document id (or use --list)")
	}
	lock, err := cache.TryLock(opt.cacheDir)
	if err != nil {
		return err
	}
	defer lock.Close()
	id := strings.TrimSpace(args[0])
	doc, err := store.LoadDoc(id)
	if err != nil {
		return fmt.Errorf("load doc %q: %w", id, err)
	}

	noteChanged := cmd.Flags().Changed("note")
	if !statusChanged && !noteChanged {
		printReview(stdout, reviewEntryFromDoc(doc), opt.asJSON)
		return nil
	}
	if statusChanged {
		doc.ReviewStatus = extract.NormalizeReviewStatus(opt.status)
	}
	if noteChanged {
		doc.ReviewNote = strings.TrimSpace(opt.note)
	}
	if err := store.SaveDoc(doc); err != nil {
		return err
	}
	printReview(stdout, reviewEntryFromDoc(doc), opt.asJSON)
	fmt.Fprintf(cmd.ErrOrStderr(), "updated doc %s\n", doc.ID)
	return nil
}

func listReviews(store *cache.Store, status string, stderr io.Writer) ([]reviewEntry, error) {
	infos, warnings := store.ListDocs()
	printDryRunWarnings(stderr, warnings)
	want := ""
	if strings.TrimSpace(status) != "" {
		want = extract.NormalizeReviewStatus(status)
	}
	entries := make([]reviewEntry, 0, len(infos))
	for _, info := range infos {
		if info.Doc == nil {
			continue
		}
		if want != "" && extract.NormalizeReviewStatus(info.Doc.ReviewStatus) != want {
			continue
		}
		entries = append(entries, reviewEntryFromDoc(info.Doc))
	}
	return entries, nil
}

func reviewEntryFromDoc(doc *extract.Doc) reviewEntry {
	return reviewEntry{
		ID:           doc.ID,
		Title:        doc.Title,
		Project:      doc.Project,
		ReviewStatus: extract.NormalizeReviewStatus(doc.ReviewStatus),
		ReviewNote:   doc.ReviewNote,
		Lifecycle:    doc.Lifecycle,
	}
}

func printReview(stdout io.Writer, entry reviewEntry, asJSON bool) {
	if asJSON {
		_ = json.NewEncoder(stdout).Encode(entry)
		return
	}
	note := ""
	if entry.ReviewNote != "" {
		note = " note=" + fmt.Sprintf("%q", entry.ReviewNote)
	}
	fmt.Fprintf(stdout, "%s: review_status=%s%s lifecycle=%s\n",
		entry.ID, entry.ReviewStatus, note, entry.Lifecycle)
}

func displayStatus(status string) string {
	if strings.TrimSpace(status) == "" {
		return "all"
	}
	return extract.NormalizeReviewStatus(status)
}
