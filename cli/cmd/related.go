package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"devlog-learn/internal/cache"
	"devlog-learn/internal/extract"
	"devlog-learn/internal/index"
)

type relatedOptions struct {
	cacheDir string
	asJSON   bool
}

func newRelatedCmd() *cobra.Command {
	opt := &relatedOptions{}
	cmd := &cobra.Command{
		Use:   "related <doc-id>",
		Short: "Show read-only related documents for one Doc",
		Long: `Compute related documents from shared tags, stack, knowledge type and
project. This is a read-only view: it never merges, rewrites, or links Doc
content. Stale documents are excluded, matching the index behavior.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRelated(cmd, opt, args[0])
		},
	}
	cmd.Flags().StringVar(&opt.cacheDir, "cache-dir", ".devlog-cache", "cache directory containing docs/")
	cmd.Flags().BoolVar(&opt.asJSON, "json", false, "print JSON instead of text")
	return cmd
}

func runRelated(cmd *cobra.Command, opt *relatedOptions, id string) error {
	stdout := cmd.OutOrStdout()
	store, err := cache.Open(opt.cacheDir)
	if err != nil {
		return err
	}
	infos, warnings := store.ListActiveDocs()
	printDryRunWarnings(cmd.ErrOrStderr(), warnings)

	docs := make([]*extract.Doc, 0, len(infos))
	var target *extract.Doc
	key := strings.TrimSpace(id)
	for _, info := range infos {
		if info.Doc == nil {
			continue
		}
		docs = append(docs, info.Doc)
		if info.Doc.ID == key {
			target = info.Doc
		}
	}
	if target == nil {
		if doc, err := store.LoadDoc(key); err == nil && doc.Lifecycle == extract.LifecycleStale {
			return fmt.Errorf("doc %s is stale and excluded from related results", key)
		}
		return fmt.Errorf("doc %s not found in active documents", key)
	}

	related := index.RelatedDocs(target, docs)
	if opt.asJSON {
		return json.NewEncoder(stdout).Encode(related)
	}
	for _, item := range related {
		fmt.Fprintf(stdout, "%d\t%s\t%s\t%s\n",
			item.Score, item.ID, item.Title, strings.Join(item.Reasons, ","))
	}
	fmt.Fprintf(cmd.ErrOrStderr(), "summary: doc=%s related=%d\n", target.ID, len(related))
	return nil
}
