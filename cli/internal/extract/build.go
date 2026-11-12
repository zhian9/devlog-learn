package extract

import (
	"fmt"
	"html"
	"sort"
	"strconv"
	"strings"
	"time"

	"devlog-learn/internal/ingest"
	"devlog-learn/internal/prompts"
	"devlog-learn/internal/segment"
)

// BuildOptions controls transcript construction and prompt budget.
type BuildOptions struct {
	MaxInputBytes      int
	MaxTextRunes       int
	MaxToolInputRunes  int
	MaxToolResultRunes int
	IncludeThinking    bool
	Language           string
}

func DefaultBuildOptions() BuildOptions {
	return BuildOptions{
		MaxInputBytes:      120_000,
		MaxTextRunes:       8_000,
		MaxToolInputRunes:  4_000,
		MaxToolResultRunes: 8_000,
		Language:           "zh",
	}
}

func (o BuildOptions) normalize() BuildOptions {
	def := DefaultBuildOptions()
	if o.MaxInputBytes <= 0 {
		o.MaxInputBytes = def.MaxInputBytes
	}
	if o.MaxTextRunes <= 0 {
		o.MaxTextRunes = def.MaxTextRunes
	}
	if o.MaxToolInputRunes <= 0 {
		o.MaxToolInputRunes = def.MaxToolInputRunes
	}
	if o.MaxToolResultRunes <= 0 {
		o.MaxToolResultRunes = def.MaxToolResultRunes
	}
	if o.Language == "" {
		o.Language = def.Language
	}
	return o
}

// Prompt is the complete model input plus truncation metadata.
type Prompt struct {
	Version      string
	Hash         string
	Text         string
	Transcript   []byte
	Bytes        int
	Truncated    bool
	Omitted      []int
	IncludedSeqs map[int]bool
}

type promptData struct {
	Language   string
	Project    string
	Source     string
	SessionID  string
	SegmentID  string
	Start      string
	End        string
	EventCount int
	Transcript string
	Truncated  bool
	Omitted    string
}

type caps struct {
	text     int
	input    int
	result   int
	thinking bool
}

// Build renders the extraction prompt, shrinking or omitting events to fit the
// byte budget. Seq wrappers are preserved for every included event so evidence
// references remain meaningful.
func Build(seg segment.Segment, opt BuildOptions) (Prompt, error) {
	opt = opt.normalize()

	base, err := renderPrompt(seg, "", false, "", opt.Language)
	if err != nil {
		return Prompt{}, err
	}
	available := opt.MaxInputBytes - len(base) - 1024
	if available < 4096 {
		return Prompt{}, fmt.Errorf("max input bytes %d is too small for the prompt overhead (%d bytes)", opt.MaxInputBytes, len(base))
	}

	attempts := []caps{
		{text: opt.MaxTextRunes, input: opt.MaxToolInputRunes, result: opt.MaxToolResultRunes, thinking: opt.IncludeThinking},
		{text: 4000, input: 2000, result: 4000, thinking: opt.IncludeThinking},
		{text: 2000, input: 1000, result: 2000, thinking: false},
		{text: 1000, input: 500, result: 1000, thinking: false},
		{text: 500, input: 200, result: 500, thinking: false},
		{text: 250, input: 120, result: 250, thinking: false},
	}

	var lastTruncated bool
	for _, c := range attempts {
		text, truncated := renderTranscript(seg.Events, c, nil)
		if len(text) <= available {
			promptText, err := renderPrompt(seg, text, truncated, "", opt.Language)
			if err != nil {
				return Prompt{}, err
			}
			return Prompt{
				Version:      prompts.PromptVersion,
				Hash:         prompts.Hash(),
				Text:         promptText,
				Transcript:   []byte(text),
				Bytes:        len(promptText),
				Truncated:    truncated,
				IncludedSeqs: renderedSeqs(seg.Events, c),
			}, nil
		}
		lastTruncated = truncated
	}

	// Still too large: drop whole events by priority, preserving the most
	// recent user request and assistant conclusion.
	finalCaps := attempts[len(attempts)-1]
	selected, omitted, truncated := selectEvents(seg.Events, available, finalCaps)
	if len(omitted) == 0 {
		return Prompt{}, fmt.Errorf("segment %s exceeds input budget and cannot be reduced", seg.ID)
	}
	text, renderedTruncated := renderTranscript(selected, finalCaps, nil)
	omittedText := compressSeqs(omitted)
	promptText, err := renderPrompt(seg, text, truncated || lastTruncated || renderedTruncated, omittedText, opt.Language)
	if err != nil {
		return Prompt{}, err
	}
	if len(promptText) > opt.MaxInputBytes {
		return Prompt{}, fmt.Errorf("segment %s still exceeds max input bytes after truncation (%d > %d)", seg.ID, len(promptText), opt.MaxInputBytes)
	}
	return Prompt{
		Version:      prompts.PromptVersion,
		Hash:         prompts.Hash(),
		Text:         promptText,
		Transcript:   []byte(text),
		Bytes:        len(promptText),
		Truncated:    true,
		Omitted:      omitted,
		IncludedSeqs: includedSet(selected),
	}, nil
}

func renderPrompt(seg segment.Segment, transcript string, truncated bool, omitted, language string) (string, error) {
	data := promptData{
		Language:   language,
		Project:    seg.Project,
		Source:     seg.Source,
		SessionID:  seg.SessionID,
		SegmentID:  seg.ID,
		Start:      formatTime(seg.Start),
		End:        formatTime(seg.End),
		EventCount: len(seg.Events),
		Transcript: transcript,
		Truncated:  truncated,
		Omitted:    omitted,
	}
	return prompts.Render("extract.md.tmpl", data)
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.UTC().Format(time.RFC3339)
}

func renderTranscript(events []ingest.Event, c caps, keep map[int]bool) (string, bool) {
	var b strings.Builder
	truncated := false
	for _, ev := range events {
		if keep != nil && !keep[ev.Seq] {
			continue
		}
		if ev.Kind == ingest.KindAssistantThink && !c.thinking {
			continue
		}
		chunk, wasTruncated := renderEvent(ev, c)
		if wasTruncated {
			truncated = true
		}
		b.WriteString(chunk)
		b.WriteByte('\n')
	}
	return b.String(), truncated
}

func renderEvent(ev ingest.Event, c caps) (string, bool) {
	text, truncated := eventBody(ev, c)
	var b strings.Builder
	b.WriteString(`<event seq="`)
	b.WriteString(strconv.Itoa(ev.Seq))
	b.WriteString(`" kind="`)
	b.WriteString(html.EscapeString(string(ev.Kind)))
	b.WriteString(`" role="`)
	b.WriteString(html.EscapeString(ev.Role))
	b.WriteString(`" time="`)
	b.WriteString(formatTime(ev.Time))
	b.WriteString(`"`)
	if ev.ToolName != "" {
		b.WriteString(` tool="`)
		b.WriteString(html.EscapeString(ev.ToolName))
		b.WriteString(`"`)
	}
	if outcome := execOutcome(ev); outcome != "" {
		b.WriteString(` outcome="`)
		b.WriteString(outcome)
		b.WriteString(`"`)
		if ev.Exec != nil && ev.Exec.ExitCode != nil {
			b.WriteString(` exit_code="`)
			b.WriteString(strconv.Itoa(*ev.Exec.ExitCode))
			b.WriteString(`"`)
		}
	}
	if truncated {
		b.WriteString(` truncated="true"`)
	}
	b.WriteString(">\n")
	b.WriteString(text)
	if !strings.HasSuffix(text, "\n") {
		b.WriteByte('\n')
	}
	b.WriteString("</event>")
	return b.String(), truncated
}

func eventBody(ev ingest.Event, c caps) (string, bool) {
	switch ev.Kind {
	case ingest.KindToolUse:
		return truncateMiddle(firstNonEmpty(ev.ToolInputText, string(ev.ToolInput)), c.input)
	case ingest.KindToolResult:
		text := ev.ToolResult
		if strings.TrimSpace(text) == "" && ev.Exec != nil {
			text = strings.TrimSpace(ev.Exec.Stdout)
			if s := strings.TrimSpace(ev.Exec.Stderr); s != "" {
				if text != "" {
					text += "\n"
				}
				text += s
			}
		}
		return truncateMiddle(text, c.result)
	case ingest.KindAssistantThink:
		return truncateMiddle(ev.Text, c.text)
	default:
		return truncateMiddle(ev.Text, c.text)
	}
}

func execOutcome(ev ingest.Event) string {
	if ev.Exec != nil {
		if ev.Exec.Canceled {
			return "canceled"
		}
		if ev.Exec.TimedOut {
			return "timed_out"
		}
		if ev.Exec.Known && ev.Exec.ExitCode != nil {
			if *ev.Exec.ExitCode == 0 {
				return "success"
			}
			return "failed"
		}
	}
	if ev.IsError {
		return "failed"
	}
	if ev.Kind == ingest.KindToolResult {
		return "unknown"
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

// truncateMiddle keeps both ends of a long field so error and result context
// survive while respecting the prompt budget.
func truncateMiddle(s string, limit int) (string, bool) {
	runes := []rune(s)
	if limit <= 0 || len(runes) <= limit {
		return s, false
	}
	head := limit * 2 / 3
	tail := limit - head
	removed := len(runes) - limit
	out := string(runes[:head]) +
		fmt.Sprintf("\n...[truncated %d chars]...\n", removed) +
		string(runes[len(runes)-tail:])
	return out, true
}

func allSeqs(events []ingest.Event) map[int]bool {
	out := make(map[int]bool, len(events))
	for _, ev := range events {
		out[ev.Seq] = true
	}
	return out
}

func includedSet(events []ingest.Event) map[int]bool {
	return allSeqs(events)
}

func renderedSeqs(events []ingest.Event, c caps) map[int]bool {
	out := make(map[int]bool, len(events))
	for _, ev := range events {
		if ev.Kind == ingest.KindAssistantThink && !c.thinking {
			continue
		}
		out[ev.Seq] = true
	}
	return out
}

// selectEvents removes lowest-priority events until the transcript fits.
func selectEvents(events []ingest.Event, available int, c caps) ([]ingest.Event, []int, bool) {
	type item struct {
		index    int
		priority int
		size     int
	}
	items := make([]item, 0, len(events))
	truncated := false
	total := 0
	for i, ev := range events {
		if ev.Kind == ingest.KindAssistantThink && !c.thinking {
			continue
		}
		chunk, wasTruncated := renderEvent(ev, c)
		if wasTruncated {
			truncated = true
		}
		items = append(items, item{index: i, priority: eventPriority(ev), size: len(chunk) + 1})
		total += len(chunk) + 1
	}

	total += 512 // omitted marker reserve
	keep := make([]bool, len(events))
	for _, it := range items {
		keep[it.index] = true
	}

	// Never drop the last user request or the last assistant conclusion.
	lastUser, lastAssistant := -1, -1
	for i := len(events) - 1; i >= 0; i-- {
		if lastUser < 0 && events[i].Kind == ingest.KindUserText {
			lastUser = i
		}
		if lastAssistant < 0 && events[i].Kind == ingest.KindAssistantText {
			lastAssistant = i
		}
	}

	var candidates []item
	for _, it := range items {
		if it.index == lastUser || it.index == lastAssistant {
			continue
		}
		candidates = append(candidates, it)
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].priority != candidates[j].priority {
			return candidates[i].priority < candidates[j].priority
		}
		return candidates[i].index < candidates[j].index
	})

	for _, it := range candidates {
		if total <= available {
			break
		}
		keep[it.index] = false
		total -= it.size
	}

	var selected []ingest.Event
	var omitted []int
	for i, ev := range events {
		if keep[i] {
			selected = append(selected, ev)
		} else {
			omitted = append(omitted, ev.Seq)
		}
	}
	return selected, omitted, truncated
}

func eventPriority(ev ingest.Event) int {
	switch ev.Kind {
	case ingest.KindUserText:
		return 100
	case ingest.KindAssistantText:
		return 90
	case ingest.KindToolUse:
		return 70
	case ingest.KindToolResult:
		if ev.IsError || (ev.Exec != nil && (ev.Exec.TimedOut || ev.Exec.Canceled)) {
			return 80
		}
		return 40
	case ingest.KindAssistantThink:
		return 10
	default:
		return 50
	}
}

func compressSeqs(seqs []int) string {
	if len(seqs) == 0 {
		return ""
	}
	sorted := append([]int(nil), seqs...)
	sort.Ints(sorted)
	var parts []string
	start, prev := sorted[0], sorted[0]
	flush := func() {
		if start == prev {
			parts = append(parts, strconv.Itoa(start))
		} else {
			parts = append(parts, fmt.Sprintf("%d-%d", start, prev))
		}
	}
	for _, seq := range sorted[1:] {
		if seq == prev+1 {
			prev = seq
			continue
		}
		flush()
		start, prev = seq, seq
	}
	flush()
	return strings.Join(parts, ",")
}

// BuildRepair renders the one-shot JSON repair prompt.
func BuildRepair(previous, parseErr, language string) (string, error) {
	if runes := []rune(previous); len(runes) > 30_000 {
		previous = string(runes[:30_000]) + "\n...[truncated]..."
	}
	return prompts.Render("repair.md.tmpl", struct {
		Previous string
		Error    string
		Language string
	}{
		Previous: previous,
		Error:    parseErr,
		Language: language,
	})
}
