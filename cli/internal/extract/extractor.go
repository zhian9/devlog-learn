package extract

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"devlog-learn/internal/prompts"
	"devlog-learn/internal/redact"
	"devlog-learn/internal/segment"
)

// ExtractorOptions controls one or more extraction calls.
type ExtractorOptions struct {
	Build       BuildOptions
	Redactor    *redact.Redactor
	RawDir      string
	KeepRaw     bool
	MaxAttempts int
	Logger      *slog.Logger
	Now         func() time.Time
}

// Extractor converts a redacted segment into a validated Doc.
type Extractor struct {
	client   Client
	build    BuildOptions
	redactor *redact.Redactor
	rawDir   string
	keepRaw  bool
	attempts int
	log      *slog.Logger
	now      func() time.Time
}

// Result reports extraction diagnostics alongside the document.
type Result struct {
	Doc      *Doc
	Prompt   Prompt
	Repairs  int
	RawPath  string
	Duration time.Duration
	Usage    Usage
}

func NewExtractor(client Client, opt ExtractorOptions) *Extractor {
	if opt.MaxAttempts <= 0 {
		opt.MaxAttempts = 2
	}
	if opt.MaxAttempts > 2 {
		opt.MaxAttempts = 2 // one initial call + one JSON repair
	}
	if opt.Logger == nil {
		opt.Logger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	}
	if opt.Now == nil {
		opt.Now = time.Now
	}
	if opt.RawDir == "" {
		opt.RawDir = filepath.Join(".devlog-cache", "raw")
	}
	return &Extractor{
		client:   client,
		build:    opt.Build.normalize(),
		redactor: opt.Redactor,
		rawDir:   opt.RawDir,
		keepRaw:  opt.KeepRaw,
		attempts: opt.MaxAttempts,
		log:      opt.Logger,
		now:      opt.Now,
	}
}

// Extract builds the prompt, calls the model, repairs invalid JSON once, and
// validates evidence before returning. Raw model responses are never persisted
// unless KeepRaw is set, and then they are redacted first.
func (e *Extractor) Extract(ctx context.Context, seg segment.Segment) (*Result, error) {
	if e.client == nil {
		return nil, fmt.Errorf("no LLM client configured")
	}
	prompt, err := Build(seg, e.build)
	if err != nil {
		return nil, fmt.Errorf("build prompt for segment %s: %w", seg.ID, err)
	}
	e.log.Debug("extraction prompt built",
		"segment", seg.ID, "bytes", prompt.Bytes, "events", len(seg.Events), "truncated", prompt.Truncated)

	started := time.Now()
	response, completeErr := e.client.Complete(ctx, Request{Prompt: prompt.Text})
	usage := response.Usage
	if completeErr != nil {
		raw := firstNonEmpty(response.Text, response.Stdout, response.Stderr)
		rawPath := e.saveRaw(seg, raw, completeErr)
		if rawPath != "" {
			return nil, fmt.Errorf("extract segment %s: %w (raw response: %s)", seg.ID, completeErr, rawPath)
		}
		return nil, fmt.Errorf("extract segment %s: %w", seg.ID, completeErr)
	}

	raw := response.Text
	doc, parseErr := ParseOutput(raw)
	repairs := 0
	rawPath := ""
	if parseErr != nil && e.attempts > 1 {
		repairs = 1
		e.log.Warn("model output invalid; attempting one JSON repair", "segment", seg.ID, "error", parseErr)
		previous := raw
		if e.redactor != nil {
			previous = e.redactor.Text(previous)
		}
		repairPrompt, err := BuildRepair(previous, parseErr.Error(), e.build.Language)
		if err != nil {
			return nil, fmt.Errorf("build repair prompt for segment %s: %w", seg.ID, err)
		}
		repairResponse, repairErr := e.client.Complete(ctx, Request{Prompt: repairPrompt})
		usage.Add(repairResponse.Usage)
		if repairErr != nil {
			rawPath = e.saveRaw(seg, firstNonEmpty(repairResponse.Text, repairResponse.Stdout), repairErr)
			if rawPath != "" {
				return nil, fmt.Errorf("repair segment %s: %w (raw response: %s)", seg.ID, repairErr, rawPath)
			}
			return nil, fmt.Errorf("repair segment %s: %w", seg.ID, repairErr)
		}
		raw = repairResponse.Text
		doc, parseErr = ParseOutput(raw)
	}
	if parseErr != nil {
		rawPath = e.saveRaw(seg, raw, parseErr)
		if rawPath != "" {
			return nil, fmt.Errorf("parse model output for segment %s: %w (raw response: %s)", seg.ID, parseErr, rawPath)
		}
		return nil, fmt.Errorf("parse model output for segment %s: %w", seg.ID, parseErr)
	}

	cliVersion := "unknown"
	if v, err := e.client.Version(ctx); err == nil && strings.TrimSpace(v) != "" {
		cliVersion = v
	}
	model := "default"
	if named, ok := e.client.(interface{ Model() string }); ok {
		if value := strings.TrimSpace(named.Model()); value != "" {
			model = value
		}
	}
	redactionHash := ""
	if e.redactor != nil {
		redactionHash = e.redactor.Hash()
	}

	doc.Normalize(seg, Provenance{
		Provider:      e.client.Name(),
		Model:         model,
		CLIVersion:    cliVersion,
		PromptVersion: prompts.PromptVersion,
		PromptHash:    prompt.Hash,
		RedactionHash: redactionHash,
		ExtractedAt:   e.now().UTC(),
	})
	ValidateEvidence(doc, seg, prompt.IncludedSeqs)

	// Defense in depth: never persist model-echoed secrets even if the input
	// was already redacted. Program-owned 64-hex fingerprints must survive:
	// the long-hex rule would otherwise replace them with a redaction marker
	// and make the document impossible to verify against its segment.
	if e.redactor != nil {
		provenance := doc.Provenance
		e.redactor.WalkStrings(doc, e.redactor.Text)
		doc.Provenance.SegmentHash = provenance.SegmentHash
		doc.Provenance.PromptHash = provenance.PromptHash
		doc.Provenance.RedactionHash = provenance.RedactionHash
	}

	return &Result{
		Doc:      doc,
		Prompt:   prompt,
		Repairs:  repairs,
		RawPath:  rawPath,
		Duration: time.Since(started),
		Usage:    usage,
	}, nil
}

func (e *Extractor) saveRaw(seg segment.Segment, raw string, cause error) string {
	if !e.keepRaw || strings.TrimSpace(raw) == "" {
		return ""
	}
	if e.redactor != nil {
		raw = e.redactor.Text(raw)
	}
	if err := os.MkdirAll(e.rawDir, 0o755); err != nil {
		e.log.Warn("cannot create raw response directory", "dir", e.rawDir, "error", err)
		return ""
	}
	name := fmt.Sprintf("%s-%d.txt", seg.ID, e.now().Unix())
	path := filepath.Join(e.rawDir, name)
	header := fmt.Sprintf("# segment: %s\n# error: %v\n# note: redacted raw model response\n\n", seg.ID, cause)
	if err := os.WriteFile(path, []byte(header+raw), 0o644); err != nil {
		e.log.Warn("cannot save raw model response", "path", path, "error", err)
		return ""
	}
	return path
}
