package pipeline

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"devlog-learn/internal/ingest"
)

// LoadOptions controls source discovery and parsing.
type LoadOptions struct {
	Provider         string
	ClaudeDir        string
	CodexDir         string
	Since            time.Time
	IncludeSubagents bool
	Limit            int
}

// LoadedSession pairs a discovered file with its parsed session.
type LoadedSession struct {
	Ref     ingest.FileRef
	Session *ingest.Session
}

type sourceSpec struct {
	source ingest.Source
	dir    string
}

// discoveredFile is one file found by discovery, before parsing.
type discoveredFile struct {
	source ingest.Source
	ref    ingest.FileRef
}

// discovery is the result of walking the selected provider roots. scanned holds
// the providers whose root existed and was walked successfully; callers use it
// to avoid declaring docs from an unavailable provider as source_missing.
type discovery struct {
	files    []discoveredFile
	warnings []string
	scanned  map[string]bool
	err      error
}

const maxReportWarnings = 200

// LoadSessions discovers and parses the selected providers. Parse problems
// become warnings; file/root failures become errors. Both may be returned
// together so the caller can print a partial report.
func LoadSessions(opt LoadOptions) ([]LoadedSession, []string, error) {
	disc := discover(opt)
	var loaded []LoadedSession
	var errs []error
	if disc.err != nil {
		errs = append(errs, disc.err)
	}
	for _, file := range disc.files {
		sess, parseErr := file.source.Parse(file.ref, ingest.ParseOptions{
			IncludeSubagent: opt.IncludeSubagents,
		})
		if parseErr != nil {
			errs = append(errs, parseErr)
			continue
		}
		loaded = append(loaded, LoadedSession{Ref: file.ref, Session: sess})
		for _, warning := range sess.Warnings {
			disc.warnings = appendWarning(disc.warnings, "%s:%d: %s", warning.File, warning.Line, warning.Message)
		}
		if sess.Stats.WarningsDropped > 0 {
			disc.warnings = appendWarning(disc.warnings, "%s: %d additional parse warnings suppressed", sess.File, sess.Stats.WarningsDropped)
		}
	}
	return loaded, disc.warnings, errors.Join(errs...)
}

// discover walks provider roots without parsing anything.
func discover(opt LoadOptions) *discovery {
	disc := &discovery{scanned: map[string]bool{}}
	specs, err := sourcesFor(opt.Provider, opt.ClaudeDir, opt.CodexDir)
	if err != nil {
		disc.err = err
		return disc
	}

	var errs []error
	for _, spec := range specs {
		root := strings.TrimSpace(spec.dir)
		explicit := root != ""
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
				disc.warnings = appendWarning(disc.warnings, "%s: provider directory not found, skipped: %s", spec.source.Name(), root)
			}
			continue
		}

		refs, discoverErr := spec.source.Discover(root, ingest.DiscoverOptions{
			Since:           opt.Since,
			IncludeSubagent: opt.IncludeSubagents,
		})
		if discoverErr != nil {
			errs = append(errs, discoverErr)
			continue
		}
		disc.scanned[spec.source.Name()] = true
		if opt.Limit > 0 && len(refs) > opt.Limit {
			refs = refs[:opt.Limit]
		}
		for _, ref := range refs {
			disc.files = append(disc.files, discoveredFile{source: spec.source, ref: ref})
		}
	}
	disc.err = errors.Join(errs...)
	return disc
}

func appendWarning(warnings []string, format string, args ...any) []string {
	if len(warnings) >= maxReportWarnings {
		if len(warnings) == maxReportWarnings {
			return append(warnings, fmt.Sprintf("... additional warnings suppressed after %d entries", maxReportWarnings))
		}
		return warnings
	}
	return append(warnings, fmt.Sprintf(format, args...))
}

func sourcesFor(provider, claudeDir, codexDir string) ([]sourceSpec, error) {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "claude-code":
		return []sourceSpec{{source: ingest.NewClaudeCode(), dir: claudeDir}}, nil
	case "codex":
		return []sourceSpec{{source: ingest.NewCodex(), dir: codexDir}}, nil
	case "all":
		return []sourceSpec{
			{source: ingest.NewClaudeCode(), dir: claudeDir},
			{source: ingest.NewCodex(), dir: codexDir},
		}, nil
	default:
		return nil, fmt.Errorf("unsupported provider %q: supported providers are claude-code and codex (Cursor is not supported)", provider)
	}
}
