// Package redact removes credentials and personal identifiers before data is
// sent to a model or written to cache/output directories.
package redact

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	"devlog-learn/internal/ingest"
	"devlog-learn/internal/segment"
)

// RuleConfig adds a user-defined regex rule from --redact-config.
type RuleConfig struct {
	Name        string `json:"name"`
	Pattern     string `json:"pattern"`
	Replacement string `json:"replacement,omitempty"`
}

// Config is deliberately zero-value-safe: emails, phones, and public IPs are
// redacted by default; private IPs are kept unless RedactPrivateIP is set.
type Config struct {
	Disable         []string     `json:"disable,omitempty"`
	Rules           []RuleConfig `json:"rules,omitempty"`
	HexMinLength    int          `json:"hex_min_length,omitempty"`
	DisableEmails   bool         `json:"disable_emails,omitempty"`
	DisablePhones   bool         `json:"disable_phones,omitempty"`
	DisableIP       bool         `json:"disable_ip,omitempty"`
	RedactPrivateIP bool         `json:"redact_private_ip,omitempty"`
}

func DefaultConfig() Config {
	return Config{HexMinLength: 64}
}

// LoadConfig reads a JSON config over the defaults.
func LoadConfig(path string) (Config, error) {
	cfg := DefaultConfig()
	data, err := os.ReadFile(path)
	if err != nil {
		return cfg, fmt.Errorf("read redact config %q: %w", path, err)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(&cfg); err != nil {
		return cfg, fmt.Errorf("parse redact config %q: %w", path, err)
	}
	return cfg, nil
}

// Guard can veto a regex match based on nearby context, avoiding
// high-false-positive rules such as long hashes and private IPs.
type Guard func(match, before, after string) bool

// Rule is one ordered redaction rule.
type Rule struct {
	Name    string
	Pattern *regexp.Regexp
	Repl    string
	Guard   Guard
}

type Redactor struct {
	rules []Rule
	hash  string
}

// New compiles the effective rule set. Unknown names in Disable are errors so
// a typo cannot silently leave secrets exposed.
func New(cfg Config) (*Redactor, error) {
	if cfg.HexMinLength <= 0 {
		cfg.HexMinLength = 64
	}
	if cfg.HexMinLength < 16 {
		return nil, fmt.Errorf("hex_min_length must be at least 16, got %d", cfg.HexMinLength)
	}

	rules := builtinRules(cfg)
	known := map[string]bool{}
	for _, rule := range rules {
		known[rule.Name] = true
	}

	disabled := map[string]bool{}
	for _, name := range cfg.Disable {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if !known[name] {
			return nil, fmt.Errorf("unknown redaction rule %q in disable list", name)
		}
		disabled[name] = true
	}

	effective := make([]Rule, 0, len(rules)+len(cfg.Rules))
	seen := map[string]bool{}
	for _, rule := range rules {
		if disabled[rule.Name] {
			continue
		}
		effective = append(effective, rule)
		seen[rule.Name] = true
	}

	for _, custom := range cfg.Rules {
		name := strings.TrimSpace(custom.Name)
		if name == "" {
			return nil, fmt.Errorf("custom redaction rule is missing a name")
		}
		if seen[name] {
			return nil, fmt.Errorf("duplicate redaction rule name %q", name)
		}
		pattern, err := regexp.Compile(custom.Pattern)
		if err != nil {
			return nil, fmt.Errorf("compile redaction rule %q: %w", name, err)
		}
		repl := custom.Replacement
		if repl == "" {
			repl = "<REDACTED:" + name + ">"
		}
		effective = append(effective, Rule{Name: name, Pattern: pattern, Repl: repl})
		seen[name] = true
	}

	r := &Redactor{rules: effective}
	r.hash = hashRules(effective, cfg)
	return r, nil
}

// Hash identifies the effective rule set for cache keys.
func (r *Redactor) Hash() string { return r.hash }

// RuleNames returns effective rule names in application order.
func (r *Redactor) RuleNames() []string {
	out := make([]string, len(r.rules))
	for i, rule := range r.rules {
		out[i] = rule.Name
	}
	return out
}

// Text redacts one string.
func (r *Redactor) Text(s string) string {
	out, _ := r.apply(s, nil)
	return out
}

// Detect reports which rules would match without changing the input.
func (r *Redactor) Detect(s string) map[string]int {
	stats := map[string]int{}
	for _, rule := range r.rules {
		for _, loc := range rule.Pattern.FindAllStringSubmatchIndex(s, -1) {
			match := s[loc[0]:loc[1]]
			if !rule.allow(match, s, loc) {
				continue
			}
			stats[rule.Name]++
		}
	}
	return stats
}

// JSON redacts string values inside valid JSON without changing its shape.
// Numbers are decoded with UseNumber so large IDs keep their precision.
func (r *Redactor) JSON(raw []byte) []byte {
	out, _ := r.json(raw, nil)
	return out
}

func (r *Redactor) json(raw []byte, stats map[string]int) ([]byte, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return raw, nil
	}
	if stats == nil {
		stats = map[string]int{}
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var value any
	if err := dec.Decode(&value); err != nil {
		text, _ := r.apply(string(raw), stats)
		return []byte(text), err
	}
	value = r.redactValue(value, stats)
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(value); err != nil {
		text, _ := r.apply(string(raw), stats)
		return []byte(text), err
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}

func (r *Redactor) redactValue(value any, stats map[string]int) any {
	switch v := value.(type) {
	case string:
		out, _ := r.apply(v, stats)
		return out
	case []any:
		for i := range v {
			v[i] = r.redactValue(v[i], stats)
		}
		return v
	case map[string]any:
		for key := range v {
			if secretJSONKeyPattern.MatchString(key) {
				if _, ok := v[key].(string); ok {
					v[key] = "<REDACTED:credential>"
					stats["credential-assignment"]++
				} else {
					v[key] = r.redactValue(v[key], stats)
				}
				continue
			}
			v[key] = r.redactValue(v[key], stats)
		}
		return v
	default:
		return value
	}
}

// Event returns a redacted deep copy and per-rule hit counts.
func (r *Redactor) Event(ev ingest.Event) (ingest.Event, map[string]int) {
	stats := map[string]int{}
	out := ev

	out.Text, _ = r.apply(ev.Text, stats)
	out.ToolInputText, _ = r.apply(ev.ToolInputText, stats)
	out.ToolResult, _ = r.apply(ev.ToolResult, stats)
	// File and Cwd are persisted in the segment cache; they must not leak
	// user home paths even though they are not part of SegmentHash.
	out.File, _ = r.apply(ev.File, stats)
	out.Cwd, _ = r.apply(ev.Cwd, stats)
	if len(ev.ToolInput) > 0 {
		out.ToolInput, _ = r.json(ev.ToolInput, stats)
	}
	if len(ev.Raw) > 0 {
		out.Raw, _ = r.json(ev.Raw, stats)
	}
	if ev.Exec != nil {
		exec := *ev.Exec
		exec.Stdout, _ = r.apply(ev.Exec.Stdout, stats)
		exec.Stderr, _ = r.apply(ev.Exec.Stderr, stats)
		out.Exec = &exec
	}
	return out, stats
}

// Segment returns a redacted deep copy. Hash is intentionally not recomputed
// here; callers should call segment.SetHash after this step.
func (r *Redactor) Segment(seg segment.Segment) (segment.Segment, map[string]int) {
	out := seg
	stats := map[string]int{}
	out.Title, _ = r.apply(seg.Title, stats)
	out.Cwd, _ = r.apply(seg.Cwd, stats)
	out.Project, _ = r.apply(seg.Project, stats)
	out.Events = make([]ingest.Event, len(seg.Events))
	for i, ev := range seg.Events {
		redacted, hits := r.Event(ev)
		out.Events[i] = redacted
		mergeStats(stats, hits)
	}
	return out, stats
}

var secretJSONKeyPattern = regexp.MustCompile(`(?i)(password|passwd|pwd|secret|token|api[_-]?key|access[_-]?key|private[_-]?key|authorization|cookie)`)

func mergeStats(dst, src map[string]int) {
	for k, v := range src {
		dst[k] += v
	}
}

func (r *Redactor) apply(s string, stats map[string]int) (string, map[string]int) {
	if s == "" {
		return s, stats
	}
	if stats == nil {
		stats = map[string]int{}
	}
	for _, rule := range r.rules {
		s = r.applyRule(rule, s, stats)
	}
	return s, stats
}

func (r *Redactor) applyRule(rule Rule, s string, stats map[string]int) string {
	locations := rule.Pattern.FindAllStringSubmatchIndex(s, -1)
	if len(locations) == 0 {
		return s
	}

	var b strings.Builder
	last := 0
	for _, loc := range locations {
		if loc[0] < last {
			continue
		}
		match := s[loc[0]:loc[1]]
		if !rule.allow(match, s, loc) {
			continue
		}
		b.WriteString(s[last:loc[0]])
		b.Write(rule.Pattern.ExpandString(nil, rule.Repl, s, loc))
		last = loc[1]
		stats[rule.Name]++
	}
	if last == 0 {
		return s
	}
	b.WriteString(s[last:])
	return b.String()
}

func (rule Rule) allow(match, src string, loc []int) bool {
	if rule.Guard == nil {
		return true
	}
	start := loc[0] - 64
	if start < 0 {
		start = 0
	}
	end := loc[1] + 64
	if end > len(src) {
		end = len(src)
	}
	return rule.Guard(match, src[start:loc[0]], src[loc[1]:end])
}

func hashRules(rules []Rule, cfg Config) string {
	h := sha256.New()
	write := func(s string) {
		h.Write([]byte(strconv.Itoa(len(s))))
		h.Write([]byte{0})
		h.Write([]byte(s))
		h.Write([]byte{0})
	}
	write("rules-v1")
	write(strconv.Itoa(cfg.HexMinLength))
	write(strconv.FormatBool(cfg.DisableEmails))
	write(strconv.FormatBool(cfg.DisablePhones))
	write(strconv.FormatBool(cfg.DisableIP))
	write(strconv.FormatBool(cfg.RedactPrivateIP))
	for _, rule := range rules {
		write(rule.Name)
		write(rule.Pattern.String())
		write(rule.Repl)
	}
	return hex.EncodeToString(h.Sum(nil))
}
