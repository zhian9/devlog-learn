package extract

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"

	"devlog-learn/internal/segment"
	"devlog-learn/internal/version"
)

// SchemaVersion is written into every Doc and checked before reuse.
const SchemaVersion = version.SchemaVersion

// Lifecycle values. Stale documents remain on disk but are excluded from
// indexes; they can return to active if their segment identity reappears.
const (
	LifecycleActive = "active"
	LifecycleStale  = "stale"
)

// Review values. Review state is human-controlled and never produced by the
// model. Re-extraction preserves it only when the segment content is unchanged.
const (
	ReviewPending  = "pending"
	ReviewApproved = "approved"
	ReviewRejected = "rejected"
)

// ValidReviewStatus reports whether value is an accepted review state.
func ValidReviewStatus(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case ReviewPending, ReviewApproved, ReviewRejected:
		return true
	}
	return false
}

// NormalizeReviewStatus canonicalizes a review status, defaulting to pending.
func NormalizeReviewStatus(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if ValidReviewStatus(value) {
		return value
	}
	return ReviewPending
}

// ErrEmptyDoc means the model returned JSON that contains no usable content.
var ErrEmptyDoc = errors.New("model returned an empty knowledge document")

// Doc is the stable, self-contained knowledge document produced for one
// segment. Additive changes only; field names are part of the RAG contract.
type Doc struct {
	SchemaVersion int    `json:"schema_version"`
	ID            string `json:"id"`
	Slug          string `json:"slug"`
	Title         string `json:"title"`
	Summary       string `json:"summary"`
	Project       string `json:"project"`
	Source        string `json:"source"`

	KnowledgeType string    `json:"knowledge_type"`
	OccurredAt    time.Time `json:"occurred_at"`
	EndedAt       time.Time `json:"ended_at"`
	DurationMin   int       `json:"duration_minutes"`

	Stack      []string `json:"stack"`
	Tags       []string `json:"tags"`
	Difficulty string   `json:"difficulty"`

	// Status is the resolution state: resolved, workaround, unresolved, unknown.
	Status string `json:"status"`

	// ReviewStatus is human-controlled: pending, approved, rejected.
	ReviewStatus string `json:"review_status"`
	ReviewNote   string `json:"review_note,omitempty"`

	// Lifecycle is active or stale. Stale docs stay on disk for provenance but
	// are excluded from indexes.
	Lifecycle   string     `json:"lifecycle"`
	StaleReason string     `json:"stale_reason,omitempty"`
	StaledAt    *time.Time `json:"staled_at,omitempty"`

	Problem            Problem              `json:"problem"`
	RootCause          RootCause            `json:"root_cause"`
	Diagnosis          []DiagnosisStep      `json:"diagnosis,omitempty"`
	Commands           []Command            `json:"commands,omitempty"`
	Concepts           []Concept            `json:"concepts,omitempty"`
	RejectedHypotheses []RejectedHypothesis `json:"rejected_hypotheses,omitempty"`
	BlindSpots         []BlindSpot          `json:"blind_spots,omitempty"`
	LearningValue      string               `json:"learning_value,omitempty"`
	Solution           []Step               `json:"solution"`
	Insights           []Insight            `json:"insights"`
	Pitfalls           []Pitfall            `json:"pitfalls"`
	Verification       []Verification       `json:"verification"`
	Lessons            []string             `json:"lessons"`
	OpenQuestions      []string             `json:"open_questions"`
	Artifacts          Artifacts            `json:"artifacts"`

	Evidence   []Evidence    `json:"evidence"`
	Uncertain  []Uncertainty `json:"uncertain"`
	Confidence float64       `json:"confidence"`

	// Validation is filled by the program, never by the model.
	Validation Validation `json:"validation"`
	Provenance Provenance `json:"provenance"`
}

type Problem struct {
	Statement       string   `json:"statement"`
	Symptoms        []string `json:"symptoms"`
	ErrorSignatures []string `json:"error_signatures"`
	Evidence        []int    `json:"evidence"`
}

type RootCause struct {
	Text       string `json:"text"`
	Known      bool   `json:"known"`
	Confidence string `json:"confidence"`
	Evidence   []int  `json:"evidence"`
}

type Step struct {
	Order    int    `json:"order"`
	Action   string `json:"action"`
	Detail   string `json:"detail"`
	Command  string `json:"command,omitempty"`
	Evidence []int  `json:"evidence"`
}

// DiagnosisStep is one step of the investigative path. It is intentionally
// separate from Solution: diagnosis records how the problem was narrowed down,
// including hypotheses that were rejected, which is the main learning value.
type DiagnosisStep struct {
	Order          int    `json:"order"`
	Observation    string `json:"observation,omitempty"`
	Hypothesis     string `json:"hypothesis,omitempty"`
	Action         string `json:"action,omitempty"`
	Command        string `json:"command,omitempty"`
	CommandKind    string `json:"command_kind,omitempty"`
	ExpectedSignal string `json:"expected_signal,omitempty"`
	Result         string `json:"result,omitempty"`
	Conclusion     string `json:"conclusion,omitempty"`
	Evidence       []int  `json:"evidence"`
}

// Command is an effective, reproducible command from the session. Kind is one
// of read, write, build, deploy, debug, or unknown. Commands are learning
// material, not an execution log: only commands that advanced the
// investigation or are needed to reproduce/verify the fix are kept.
type Command struct {
	Purpose        string `json:"purpose"`
	Command        string `json:"command"`
	Kind           string `json:"kind,omitempty"`
	ExpectedSignal string `json:"expected_signal,omitempty"`
	Risk           string `json:"risk,omitempty"`
	Evidence       []int  `json:"evidence"`
}

// Concept is one knowledge card attached to the case. Name alone is not
// enough: a useful card explains why the concept mattered and shows a minimal
// syntax/example taken from (or immediately justified by) the session.
type Concept struct {
	Name        string `json:"name"`
	Stack       string `json:"stack,omitempty"`
	WhyRelevant string `json:"why_relevant,omitempty"`
	Syntax      string `json:"syntax,omitempty"`
	Example     string `json:"example,omitempty"`
	DocsURL     string `json:"docs_url,omitempty"`
	Evidence    []int  `json:"evidence"`
}

// RejectedHypothesis records a hypothesis the investigation ruled out. It is
// the highest-value part of the diagnosis path for human learning.
type RejectedHypothesis struct {
	Hypothesis  string `json:"hypothesis"`
	WhyRejected string `json:"why_rejected"`
	Evidence    []int  `json:"evidence"`
}

// BlindSpot is a gap the session exposed. Kind is one of general_method,
// framework_knowledge, or project_context.
type BlindSpot struct {
	Point        string `json:"point"`
	Kind         string `json:"kind,omitempty"`
	WhyItMatters string `json:"why_it_matters,omitempty"`
	Evidence     []int  `json:"evidence"`
}

type Insight struct {
	Text       string `json:"text"`
	Confidence string `json:"confidence"`
	Evidence   []int  `json:"evidence"`
}

type Pitfall struct {
	Trap     string `json:"trap"`
	Why      string `json:"why"`
	Avoid    string `json:"avoid"`
	Evidence []int  `json:"evidence"`
}

type Verification struct {
	Check    string `json:"check"`
	Expected string `json:"expected"`
	Observed string `json:"observed"`
	Status   string `json:"status"` // passed|failed|not_run|unknown
	Evidence []int  `json:"evidence"`
}

type Artifacts struct {
	Files    []string `json:"files"`
	Commands []string `json:"commands"`
	URLs     []string `json:"urls"`
}

type Evidence struct {
	EventSeq int    `json:"event_seq"`
	Kind     string `json:"kind"`
	Quote    string `json:"quote"`
	// Verified is set by ValidateEvidence; model output is overwritten.
	Verified bool `json:"verified"`
}

type Uncertainty struct {
	Field  string `json:"field"`
	Reason string `json:"reason"`
	Known  bool   `json:"known"`
}

type Validation struct {
	EvidenceChecked  int      `json:"evidence_checked"`
	EvidenceVerified int      `json:"evidence_verified"`
	Warnings         []string `json:"warnings,omitempty"`
}

type Provenance struct {
	SessionID        string    `json:"session_id"`
	LogFile          string    `json:"log_file"`
	SegmentID        string    `json:"segment_id"`
	SegmentHash      string    `json:"segment_hash"`
	EventRange       [2]int    `json:"event_range"`
	SourceEventCount int       `json:"source_event_count"`
	Provider         string    `json:"provider"`
	Model            string    `json:"model"`
	CLIVersion       string    `json:"cli_version"`
	PromptVersion    string    `json:"prompt_version"`
	PromptHash       string    `json:"prompt_hash"`
	RedactionHash    string    `json:"redaction_hash"`
	SchemaVersion    int       `json:"schema_version"`
	SegmenterVersion int       `json:"segmenter_version"`
	RedactionVersion int       `json:"redaction_version"`
	BuildVersion     int       `json:"build_version"`
	CacheKey         string    `json:"cache_key,omitempty"`
	ExtractedAt      time.Time `json:"extracted_at"`
}

var (
	knowledgeTypes = setOf(
		"bug_fix", "learning", "architecture", "development",
		"refactoring", "configuration", "decision",
	)
	difficulties    = setOf("low", "medium", "high", "unknown")
	statusValues    = setOf("resolved", "workaround", "unresolved", "unknown")
	confidenceLevel = setOf("high", "medium", "low", "unknown")
	verifyStatuses  = setOf("passed", "failed", "not_run", "unknown")
	evidenceKinds   = setOf("user_report", "error_output", "tool_output", "file_change", "command_output", "assistant_analysis", "unknown")
	commandKinds    = setOf("read", "write", "build", "deploy", "debug", "unknown")
	learningValues  = setOf("low", "medium", "high")
	blindSpotKinds  = setOf("general_method", "framework_knowledge", "project_context")
)

func setOf(values ...string) map[string]bool {
	out := make(map[string]bool, len(values))
	for _, value := range values {
		out[value] = true
	}
	return out
}

// Normalize fills deterministic fields, validates enums, and sanitizes list
// data. Model-supplied IDs/time/provenance/review/lifecycle are ignored.
func (d *Doc) Normalize(seg segment.Segment, prov Provenance) {
	if d == nil {
		return
	}
	d.SchemaVersion = SchemaVersion
	d.ID = seg.ID
	d.Project = seg.Project
	d.Source = seg.Source
	d.OccurredAt = seg.Start
	d.EndedAt = seg.End
	if !seg.Start.IsZero() && !seg.End.IsZero() && seg.End.After(seg.Start) {
		d.DurationMin = int(seg.End.Sub(seg.Start).Minutes())
	}

	if strings.TrimSpace(d.Title) == "" {
		d.Title = seg.Title
	}
	d.Title = strings.TrimSpace(d.Title)
	d.Summary = strings.TrimSpace(d.Summary)
	d.Slug = MakeSlug(d.Title, seg.Start)

	d.KnowledgeType = normalizeEnum(d.KnowledgeType, knowledgeTypes, "learning")
	d.Difficulty = normalizeEnum(d.Difficulty, difficulties, "unknown")
	d.Status = normalizeEnum(d.Status, statusValues, "unknown")
	d.RootCause.Confidence = normalizeEnum(d.RootCause.Confidence, confidenceLevel, "unknown")
	for i := range d.Insights {
		d.Insights[i].Confidence = normalizeEnum(d.Insights[i].Confidence, confidenceLevel, "unknown")
	}
	for i := range d.Verification {
		d.Verification[i].Status = normalizeEnum(d.Verification[i].Status, verifyStatuses, "unknown")
	}
	for i := range d.Evidence {
		d.Evidence[i].Kind = normalizeEnum(d.Evidence[i].Kind, evidenceKinds, "unknown")
		d.Evidence[i].Quote = truncateRunes(strings.TrimSpace(d.Evidence[i].Quote), 200)
		d.Evidence[i].Verified = false
	}
	d.Evidence = dedupeEvidence(d.Evidence)

	// Review and lifecycle are never model-controlled.
	d.ReviewStatus = ReviewPending
	d.ReviewNote = ""
	d.Lifecycle = LifecycleActive
	d.StaleReason = ""
	d.StaledAt = nil

	d.Stack = cleanList(d.Stack, 20)
	d.Tags = cleanList(d.Tags, 30)
	d.Lessons = cleanList(d.Lessons, 30)
	d.OpenQuestions = cleanList(d.OpenQuestions, 30)
	d.Problem.Symptoms = cleanList(d.Problem.Symptoms, 20)
	d.Problem.ErrorSignatures = cleanList(d.Problem.ErrorSignatures, 20)
	d.Artifacts.Files = cleanList(d.Artifacts.Files, 50)
	d.Artifacts.Commands = cleanList(d.Artifacts.Commands, 50)
	d.Artifacts.URLs = cleanList(d.Artifacts.URLs, 30)

	for i := range d.Solution {
		if d.Solution[i].Order <= 0 {
			d.Solution[i].Order = i + 1
		}
	}
	sort.SliceStable(d.Solution, func(i, j int) bool {
		return d.Solution[i].Order < d.Solution[j].Order
	})

	for i := range d.Diagnosis {
		step := &d.Diagnosis[i]
		if step.Order <= 0 {
			step.Order = i + 1
		}
		step.Observation = cleanText(step.Observation, 1200)
		step.Hypothesis = cleanText(step.Hypothesis, 1200)
		step.Action = cleanText(step.Action, 1200)
		step.Command = truncateRunes(strings.TrimSpace(step.Command), 4000)
		step.CommandKind = normalizeEnum(step.CommandKind, commandKinds, "unknown")
		step.ExpectedSignal = cleanText(step.ExpectedSignal, 1200)
		step.Result = cleanText(step.Result, 1200)
		step.Conclusion = cleanText(step.Conclusion, 1200)
	}
	sort.SliceStable(d.Diagnosis, func(i, j int) bool {
		return d.Diagnosis[i].Order < d.Diagnosis[j].Order
	})
	if len(d.Diagnosis) > 40 {
		d.Diagnosis = d.Diagnosis[:40]
	}

	for i := range d.Commands {
		command := &d.Commands[i]
		command.Purpose = cleanText(command.Purpose, 400)
		command.Command = truncateRunes(strings.TrimSpace(command.Command), 4000)
		command.Kind = normalizeEnum(command.Kind, commandKinds, "unknown")
		command.ExpectedSignal = cleanText(command.ExpectedSignal, 800)
		command.Risk = cleanText(command.Risk, 400)
	}
	d.Commands = dedupeCommands(d.Commands, 40)

	d.LearningValue = normalizeOptionalEnum(d.LearningValue, learningValues)
	for i := range d.Concepts {
		concept := &d.Concepts[i]
		concept.Name = cleanText(concept.Name, 160)
		concept.Stack = cleanText(concept.Stack, 120)
		concept.WhyRelevant = cleanText(concept.WhyRelevant, 1200)
		concept.Syntax = truncateRunes(strings.TrimSpace(concept.Syntax), 2000)
		concept.Example = truncateRunes(strings.TrimSpace(concept.Example), 4000)
		concept.DocsURL = truncateRunes(strings.TrimSpace(concept.DocsURL), 500)
	}
	d.Concepts = dedupeConcepts(d.Concepts, 30)

	for i := range d.RejectedHypotheses {
		rejected := &d.RejectedHypotheses[i]
		rejected.Hypothesis = cleanText(rejected.Hypothesis, 800)
		rejected.WhyRejected = cleanText(rejected.WhyRejected, 1200)
	}
	d.RejectedHypotheses = dedupeRejectedHypotheses(d.RejectedHypotheses, 20)

	for i := range d.BlindSpots {
		spot := &d.BlindSpots[i]
		spot.Point = cleanText(spot.Point, 800)
		spot.Kind = normalizeOptionalEnum(spot.Kind, blindSpotKinds)
		spot.WhyItMatters = cleanText(spot.WhyItMatters, 1200)
	}
	d.BlindSpots = dedupeBlindSpots(d.BlindSpots, 20)

	d.Uncertain = dedupeUncertain(d.Uncertain)

	if d.Confidence < 0 {
		d.Confidence = 0
	}
	if d.Confidence > 1 {
		d.Confidence = 1
	}

	d.Validation = Validation{Warnings: []string{}}
	if d.Summary == "" {
		d.addValidationWarning("summary is empty")
	}
	if len(d.Solution) == 0 && (d.KnowledgeType == "bug_fix" || d.KnowledgeType == "development") {
		d.addValidationWarning("solution is empty for a bug_fix/development document")
	}

	prov.SessionID = seg.SessionID
	prov.SegmentID = seg.ID
	prov.SegmentHash = seg.Hash
	prov.SourceEventCount = len(seg.Events)
	prov.SchemaVersion = SchemaVersion
	prov.SegmenterVersion = version.SegmenterVersion
	prov.RedactionVersion = version.RedactionVersion
	prov.BuildVersion = version.BuildVersion
	if len(seg.Events) > 0 {
		first, last := seg.Events[0].Seq, seg.Events[len(seg.Events)-1].Seq
		if first > last {
			first, last = last, first
		}
		prov.EventRange = [2]int{first, last}
		if prov.LogFile == "" {
			prov.LogFile = seg.Events[0].File
		}
	}
	if prov.ExtractedAt.IsZero() {
		prov.ExtractedAt = time.Now().UTC()
	}
	d.Provenance = prov

	if d.Evidence == nil {
		d.Evidence = []Evidence{}
	}
	if d.Uncertain == nil {
		d.Uncertain = []Uncertainty{}
	}
}

// Validate checks that the model returned a non-empty document. Field-level
// issues are warnings, not hard failures.
func (d *Doc) Validate() error {
	if d == nil {
		return ErrEmptyDoc
	}
	if strings.TrimSpace(d.Title) == "" &&
		strings.TrimSpace(d.Summary) == "" &&
		len(d.Solution) == 0 &&
		len(d.Insights) == 0 &&
		len(d.Evidence) == 0 {
		return ErrEmptyDoc
	}
	return nil
}

// MakeSlug creates a filesystem-safe, human-readable document slug. Unicode
// letters (including CJK) are kept; everything else collapses to '-'.
func MakeSlug(title string, at time.Time) string {
	date := "undated"
	if !at.IsZero() {
		date = at.Local().Format("2006-01-02")
	}
	return date + "-" + slugify(title)
}

func slugify(title string) string {
	var b strings.Builder
	lastDash := false
	for _, r := range strings.ToLower(strings.TrimSpace(title)) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteRune('-')
			lastDash = true
		}
	}
	out := strings.Trim(b.String(), "-")
	runes := []rune(out)
	if len(runes) > 60 {
		out = strings.Trim(string(runes[:60]), "-")
	}
	if out == "" {
		out = "untitled"
	}
	return out
}

func normalizeEnum(value string, allowed map[string]bool, fallback string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if allowed[value] {
		return value
	}
	return fallback
}

// normalizeOptionalEnum canonicalizes an optional enum. Unlike normalizeEnum it
// returns "" for unknown values rather than inventing a category, so a bad
// model value is omitted instead of becoming a fabricated fact.
func normalizeOptionalEnum(value string, allowed map[string]bool) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if allowed[value] {
		return value
	}
	return ""
}

func cleanList(values []string, limit int) []string {
	out := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		key := strings.ToLower(value)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, value)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

func cleanText(value string, limit int) string {
	value = strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	return truncateRunes(value, limit)
}

func dedupeCommands(values []Command, limit int) []Command {
	out := make([]Command, 0, len(values))
	seen := map[string]bool{}
	for _, command := range values {
		command.Command = strings.TrimSpace(command.Command)
		if command.Command == "" || seen[command.Command] {
			continue
		}
		seen[command.Command] = true
		out = append(out, command)
		if len(out) >= limit {
			break
		}
	}
	return out
}

func dedupeConcepts(values []Concept, limit int) []Concept {
	out := make([]Concept, 0, len(values))
	seen := map[string]bool{}
	for _, concept := range values {
		if concept.Name == "" {
			continue
		}
		key := strings.ToLower(concept.Name) + "\x00" + strings.ToLower(concept.Stack)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, concept)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

func dedupeRejectedHypotheses(values []RejectedHypothesis, limit int) []RejectedHypothesis {
	out := make([]RejectedHypothesis, 0, len(values))
	seen := map[string]bool{}
	for _, rejected := range values {
		if rejected.Hypothesis == "" && rejected.WhyRejected == "" {
			continue
		}
		key := strings.ToLower(rejected.Hypothesis) + "\x00" + strings.ToLower(rejected.WhyRejected)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, rejected)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

func dedupeBlindSpots(values []BlindSpot, limit int) []BlindSpot {
	out := make([]BlindSpot, 0, len(values))
	seen := map[string]bool{}
	for _, spot := range values {
		if spot.Point == "" && spot.WhyItMatters == "" {
			continue
		}
		key := strings.ToLower(spot.Point) + "\x00" + strings.ToLower(spot.WhyItMatters)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, spot)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

func dedupeEvidence(values []Evidence) []Evidence {
	out := make([]Evidence, 0, len(values))
	index := map[int]int{}
	for _, ev := range values {
		if ev.Quote == "" && ev.EventSeq <= 0 {
			continue
		}
		if pos, ok := index[ev.EventSeq]; ok {
			if len(ev.Quote) > len(out[pos].Quote) {
				out[pos].Quote = ev.Quote
				out[pos].Kind = ev.Kind
			}
			continue
		}
		index[ev.EventSeq] = len(out)
		out = append(out, ev)
	}
	return out
}

func dedupeUncertain(values []Uncertainty) []Uncertainty {
	out := make([]Uncertainty, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		value.Field = strings.TrimSpace(value.Field)
		value.Reason = strings.TrimSpace(value.Reason)
		if value.Field == "" && value.Reason == "" {
			continue
		}
		key := strings.ToLower(value.Field) + "\x00" + strings.ToLower(value.Reason)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, value)
	}
	return out
}

func truncateRunes(s string, limit int) string {
	runes := []rune(s)
	if limit <= 0 || len(runes) <= limit {
		return s
	}
	return string(runes[:limit])
}

func (d *Doc) addValidationWarning(format string, args ...any) {
	if len(d.Validation.Warnings) >= 50 {
		return
	}
	if len(args) == 0 {
		d.Validation.Warnings = append(d.Validation.Warnings, format)
		return
	}
	d.Validation.Warnings = append(d.Validation.Warnings, fmt.Sprintf(format, args...))
}
