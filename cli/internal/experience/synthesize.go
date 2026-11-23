package experience

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"
	"time"

	"devlog-learn/internal/extract"
)

// SynthesisPromptVersion changes when the synthesis template semantics change.
const SynthesisPromptVersion = "experience-v1"

// SynthesisCacheVersion covers the cached synthesis envelope.
const SynthesisCacheVersion = 1

const (
	synthesisTemplateName = "experience.md.tmpl"
	synthesisDirName      = "experience-synthesis"
)

//go:embed experience.md.tmpl
var synthesisTemplates embed.FS

var synthesisPromptHash = func() string {
	data, err := synthesisTemplates.ReadFile(synthesisTemplateName)
	if err != nil {
		panic(fmt.Sprintf("experience: read synthesis template: %v", err))
	}
	sum := sha256.Sum256(append([]byte(SynthesisPromptVersion+"\x00"), data...))
	return hex.EncodeToString(sum[:])
}()

// Synthesis is optional, model-generated narrative. It never replaces the
// deterministic baseline and is always marked unverified.
type Synthesis struct {
	Version     int       `json:"version"`
	Project     string    `json:"project"`
	Provider    string    `json:"provider"`
	Model       string    `json:"model"`
	GeneratedAt time.Time `json:"generated_at"`
	PromptHash  string    `json:"prompt_hash"`
	InputHash   string    `json:"input_hash"`

	Summary       string         `json:"summary,omitempty"`
	Background    string         `json:"background,omitempty"`
	Contributions []Highlight    `json:"contributions"`
	Challenges    []Highlight    `json:"challenges"`
	Outcomes      []Highlight    `json:"outcomes"`
	Stories       []Highlight    `json:"stories"`
	DocIDs        []string       `json:"doc_ids"`
	Unverified    bool           `json:"unverified"`
	Usage         *extract.Usage `json:"usage,omitempty"`
}

type synthesisOutput struct {
	Summary       string               `json:"summary"`
	Background    string               `json:"background"`
	Contributions []synthesisHighlight `json:"contributions"`
	Challenges    []synthesisHighlight `json:"challenges"`
	Outcomes      []synthesisHighlight `json:"outcomes"`
	Stories       []synthesisHighlight `json:"stories"`
}

type synthesisHighlight struct {
	Text   string   `json:"text"`
	DocIDs []string `json:"doc_ids"`
}

type synthesisPromptData struct {
	Project   string
	KindLabel string
	Period    string
	Stack     string
	Docs      []synthesisPromptDoc
}

type synthesisPromptDoc struct {
	ID        string
	Title     string
	Summary   string
	RootCause string
	Lessons   string
}

type cachedSynthesis struct {
	Version    int       `json:"version"`
	InputHash  string    `json:"input_hash"`
	PromptHash string    `json:"prompt_hash"`
	Synthesis  Synthesis `json:"synthesis"`
}

// BuildPrompt renders the compact, already-redacted Doc summary used for
// synthesis. Raw session events and evidence quotes are never included.
func BuildPrompt(exp *Experience, docs []*extract.Doc) (string, error) {
	if exp == nil {
		return "", errors.New("nil experience")
	}
	ranked := rankDocs(docs)
	if len(ranked) > maxChecklistDocs {
		ranked = ranked[:maxChecklistDocs]
	}
	data := synthesisPromptData{
		Project:   exp.Project,
		KindLabel: exp.KindLabel,
		Period:    nonEmpty(exp.Period, "未记录"),
		Stack:     nonEmpty(strings.Join(exp.Stack, ", "), "未记录"),
		Docs:      make([]synthesisPromptDoc, 0, len(ranked)),
	}
	for _, doc := range ranked {
		data.Docs = append(data.Docs, synthesisPromptDoc{
			ID:        doc.ID,
			Title:     truncateRunes(doc.Title, 160),
			Summary:   truncateRunes(doc.Summary, 500),
			RootCause: truncateRunes(doc.RootCause.Text, 500),
			Lessons:   truncateRunes(strings.Join(doc.Lessons, "；"), 500),
		})
	}

	funcs := template.FuncMap{"join": strings.Join}
	tmpl, err := template.New(synthesisTemplateName).Funcs(funcs).Option("missingkey=error").ParseFS(synthesisTemplates, synthesisTemplateName)
	if err != nil {
		return "", fmt.Errorf("parse synthesis template: %w", err)
	}
	var b strings.Builder
	if err := tmpl.Execute(&b, data); err != nil {
		return "", fmt.Errorf("render synthesis prompt: %w", err)
	}
	return b.String(), nil
}

// InputHash fingerprints the exact prompt plus provider/model, so a cached
// synthesis is reused only for identical input.
func InputHash(prompt, provider, model string) string {
	sum := sha256.Sum256([]byte(prompt + "\x00" + provider + "\x00" + model))
	return hex.EncodeToString(sum[:])
}

// Synthesize calls the local model once and returns validated output. Unknown
// Doc IDs are dropped and items without a valid reference are discarded.
func Synthesize(ctx context.Context, client extract.Client, provider, model string, exp *Experience, docs []*extract.Doc, now time.Time) (*Synthesis, error) {
	if client == nil {
		return nil, errors.New("no model client configured")
	}
	if exp == nil {
		return nil, errors.New("nil experience")
	}
	prompt, err := BuildPrompt(exp, docs)
	if err != nil {
		return nil, err
	}
	response, err := client.Complete(ctx, extract.Request{Prompt: prompt})
	if err != nil {
		return nil, err
	}
	usage := response.Usage
	output, err := parseSynthesisOutput(response.Text)
	if err != nil {
		return nil, err
	}

	provider = strings.TrimSpace(provider)
	if provider == "" {
		provider = client.Name()
	}
	model = strings.TrimSpace(model)
	if model == "" {
		if value, ok := client.(interface{ Model() string }); ok {
			model = strings.TrimSpace(value.Model())
		}
	}

	valid := map[string]bool{}
	for _, doc := range docs {
		if doc != nil && strings.TrimSpace(doc.ID) != "" {
			valid[doc.ID] = true
		}
	}
	seenDoc := map[string]bool{}
	clean := func(items []synthesisHighlight) []Highlight {
		out := make([]Highlight, 0, len(items))
		for _, item := range items {
			text := strings.TrimSpace(item.Text)
			if text == "" {
				continue
			}
			ids := make([]string, 0, len(item.DocIDs))
			for _, id := range item.DocIDs {
				id = strings.TrimSpace(id)
				if !valid[id] {
					continue
				}
				seenDoc[id] = true
				if !contains(ids, id) {
					ids = append(ids, id)
				}
			}
			if len(ids) == 0 {
				continue
			}
			out = append(out, Highlight{Text: truncateRunes(text, 600), DocIDs: ids})
		}
		return out
	}

	synthesis := &Synthesis{
		Version:       SynthesisCacheVersion,
		Project:       exp.Project,
		Provider:      provider,
		Model:         model,
		GeneratedAt:   now.UTC(),
		PromptHash:    synthesisPromptHash,
		InputHash:     InputHash(prompt, provider, model),
		Summary:       truncateRunes(output.Summary, 1000),
		Background:    truncateRunes(output.Background, 2000),
		Contributions: clean(output.Contributions),
		Challenges:    clean(output.Challenges),
		Outcomes:      clean(output.Outcomes),
		Stories:       clean(output.Stories),
		Unverified:    true,
		Usage:         &usage,
	}
	synthesis.DocIDs = sortedKeys(seenDoc)
	if len(synthesis.DocIDs) == 0 {
		return nil, errors.New("synthesis output does not reference any valid doc_id")
	}
	if strings.TrimSpace(synthesis.Summary) == "" &&
		strings.TrimSpace(synthesis.Background) == "" &&
		len(synthesis.Contributions) == 0 &&
		len(synthesis.Challenges) == 0 &&
		len(synthesis.Outcomes) == 0 &&
		len(synthesis.Stories) == 0 {
		return nil, errors.New("synthesis output is empty")
	}
	return synthesis, nil
}

func parseSynthesisOutput(raw string) (*synthesisOutput, error) {
	candidate := locateJSON(raw)
	if candidate == "" {
		return nil, errors.New("model output contains no JSON object")
	}
	var output synthesisOutput
	if err := json.Unmarshal([]byte(candidate), &output); err != nil {
		return nil, fmt.Errorf("decode synthesis output: %w", err)
	}
	return &output, nil
}

func locateJSON(raw string) string {
	s := strings.TrimSpace(strings.TrimPrefix(raw, string(rune(0xFEFF))))
	if s == "" {
		return ""
	}
	if strings.HasPrefix(s, "```") {
		if nl := strings.IndexByte(s, '\n'); nl >= 0 {
			s = s[nl+1:]
		}
		if end := strings.Index(s, "```"); end >= 0 {
			s = s[:end]
		}
		s = strings.TrimSpace(s)
	}
	start := strings.IndexByte(s, '{')
	end := strings.LastIndexByte(s, '}')
	if start >= 0 && end > start {
		return s[start : end+1]
	}
	return ""
}

// AttachCachedSynthesis attaches a cached synthesis to each experience when the
// current input still matches. It never calls a model.
func AttachCachedSynthesis(exps []Experience, docsByProject map[string][]*extract.Doc, cacheDir string) {
	cacheDir = strings.TrimSpace(cacheDir)
	if cacheDir == "" {
		return
	}
	for i := range exps {
		if synthesis, ok := LoadCachedSynthesis(cacheDir, &exps[i], docsByProject[exps[i].Project]); ok {
			exps[i].Synthesized = synthesis
		}
	}
}

// LoadCachedSynthesis returns a cached synthesis only when it matches the
// current prompt, provider, model, and cache version.
func LoadCachedSynthesis(cacheDir string, exp *Experience, docs []*extract.Doc) (*Synthesis, bool) {
	if exp == nil {
		return nil, false
	}
	prompt, err := BuildPrompt(exp, docs)
	if err != nil {
		return nil, false
	}
	data, err := os.ReadFile(synthesisCachePath(cacheDir, exp.Project))
	if err != nil {
		return nil, false
	}
	var cached cachedSynthesis
	if err := json.Unmarshal(data, &cached); err != nil {
		return nil, false
	}
	if cached.Version != SynthesisCacheVersion ||
		cached.PromptHash != synthesisPromptHash ||
		cached.Synthesis.Project != exp.Project {
		return nil, false
	}
	if cached.InputHash != InputHash(prompt, cached.Synthesis.Provider, cached.Synthesis.Model) {
		return nil, false
	}
	cached.Synthesis.Unverified = true
	return &cached.Synthesis, true
}

// SaveCachedSynthesis writes a synthesis cache entry atomically.
func SaveCachedSynthesis(cacheDir string, synthesis *Synthesis) error {
	if synthesis == nil {
		return errors.New("cannot cache a nil synthesis")
	}
	envelope := cachedSynthesis{
		Version:    SynthesisCacheVersion,
		InputHash:  synthesis.InputHash,
		PromptHash: synthesis.PromptHash,
		Synthesis:  *synthesis,
	}
	data, err := json.MarshalIndent(envelope, "", "  ")
	if err != nil {
		return fmt.Errorf("encode synthesis cache: %w", err)
	}
	data = append(data, '\n')
	return writeAtomic(synthesisCachePath(cacheDir, synthesis.Project), data)
}

// SynthesisCachePath returns the cache file used for one project's synthesis.
func SynthesisCachePath(cacheDir, project string) string {
	return synthesisCachePath(cacheDir, project)
}

func synthesisCachePath(cacheDir, project string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(project)))
	return filepath.Join(cacheDir, synthesisDirName, hex.EncodeToString(sum[:8])+".json")
}

func nonEmpty(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
