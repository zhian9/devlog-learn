package export

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"devlog-learn/internal/cache"
	"devlog-learn/internal/experience"
	"devlog-learn/internal/extract"
	"devlog-learn/internal/manifest"
	"devlog-learn/internal/render"
)

// ProfileExperience owns the EXPERIENCE.md files. It is deliberately separate
// from agent/learning/index so prune cannot remove another profile's output.
const ProfileExperience = "experience"

// ExperienceRenderOptions controls experience output maintenance.
type ExperienceRenderOptions struct {
	Prune   bool
	Project string
}

// RenderExperiences writes one EXPERIENCE.md per project plus the global
// overview. It never calls a model: a cached synthesis is attached to the
// aggregate when present, but the deterministic baseline is always written.
func RenderExperiences(store *cache.Store, outDir string, state *experience.State, opts ExperienceRenderOptions) (RenderReport, error) {
	report := RenderReport{}
	if store == nil {
		return report, fmt.Errorf("no cache store")
	}
	if state == nil {
		state = experience.NewState()
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return report, fmt.Errorf("create output directory: %w", err)
	}

	infos, warnings := store.ListActiveDocs()
	report.Warnings = append(report.Warnings, warnings...)
	now := time.Now().UTC()
	allExps := experience.Build(infos, store, state, now)
	overviewPlans := render.PlanExperiences(allExps)
	plans := overviewPlans
	selector := strings.TrimSpace(opts.Project)
	if selector != "" {
		target, err := experience.FindWith(allExps, selector, render.SafeProject)
		if err != nil {
			return report, err
		}
		filtered := make([]render.ExperiencePlan, 0, 1)
		for _, plan := range overviewPlans {
			if plan.Experience.Project == target.Project {
				filtered = append(filtered, plan)
			}
		}
		plans = filtered
	}
	for _, exp := range allExps {
		report.Warnings = append(report.Warnings, exp.Warnings...)
	}

	docs := make([]*extract.Doc, 0, len(infos))
	for _, info := range infos {
		if info.Doc != nil {
			docs = append(docs, info.Doc)
		}
	}
	docPaths := make(map[string]string, len(docs))
	for _, plan := range render.PlanDocs(docs) {
		docPaths[plan.Doc.ID] = plan.Path
	}

	generated := make([]manifest.GeneratedFile, 0, len(plans)+1)
	var errs []error
	for _, plan := range plans {
		rel := plan.Path
		path := filepath.Join(outDir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			errs = append(errs, fmt.Errorf("create output directory for %s: %w", rel, err))
			continue
		}
		if err := os.WriteFile(path, []byte(render.ExperienceMarkdown(plan.Experience, docPaths)), 0o644); err != nil {
			errs = append(errs, fmt.Errorf("write %s: %w", rel, err))
			continue
		}
		report.Paths = append(report.Paths, path)
		report.Written++
		generated = append(generated, manifest.GeneratedFile{Path: rel})
	}

	overviewPath := filepath.Join(outDir, render.ExperienceOverviewPath)
	if err := os.WriteFile(overviewPath, []byte(render.ExperienceOverviewMarkdown(overviewPlans)), 0o644); err != nil {
		errs = append(errs, fmt.Errorf("write %s: %w", render.ExperienceOverviewPath, err))
	} else {
		report.Paths = append(report.Paths, overviewPath)
		report.Written++
		generated = append(generated, manifest.GeneratedFile{Path: render.ExperienceOverviewPath})
	}

	prune := opts.Prune && selector == ""
	if opts.Prune && selector != "" {
		report.Warnings = append(report.Warnings, "--prune is ignored when --project is set")
	}
	if len(errs) > 0 {
		prune = false
	}
	manifestReport, manifestErr := manifest.Reconcile(outDir, ProfileExperience, generated, prune, now)
	report.Pruned = manifestReport.Pruned
	report.PruneSkipped = manifestReport.Skipped
	report.Warnings = append(report.Warnings, manifestReport.Warnings...)
	errs = append(errs, manifestErr)
	if joined := errors.Join(errs...); joined != nil {
		return report, joined
	}
	return report, nil
}
