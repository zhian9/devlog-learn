package pipeline

import (
	"context"
	"time"

	"devlog-learn/internal/ingest"
	"devlog-learn/internal/redact"
	"devlog-learn/internal/segment"
	"devlog-learn/internal/version"
)

// Options controls a dry run. No model is called.
type Options struct {
	Load          LoadOptions
	SinceLabel    string
	Segment       segment.Options
	Redact        redact.Config
	IncludeEvents bool
	Now           func() time.Time
}

// DryRunItem is one segment that would be sent for extraction.
type DryRunItem struct {
	Source    string `json:"source"`
	Project   string `json:"project"`
	SessionID string `json:"session_id"`
	SegmentID string `json:"segment_id"`
	Hash      string `json:"hash"`
	Title     string `json:"title"`
	Boundary  string `json:"boundary"`

	Start time.Time `json:"start"`
	End   time.Time `json:"end"`

	EventCount    int            `json:"event_count"`
	UserTextCount int            `json:"user_text_count"`
	Bytes         int            `json:"bytes"`
	Redactions    map[string]int `json:"redactions,omitempty"`
	Events        []EventPreview `json:"events,omitempty"`
}

// EventPreview is a redacted, human-oriented view used by dry-run.
type EventPreview struct {
	Seq  int       `json:"seq"`
	Kind string    `json:"kind"`
	Role string    `json:"role"`
	Tool string    `json:"tool,omitempty"`
	Time time.Time `json:"time"`
	Text string    `json:"text,omitempty"`
}

// DryRunReport is a complete, model-free preview.
type DryRunReport struct {
	GeneratedAt time.Time `json:"generated_at"`
	Since       string    `json:"since"`

	ParserVersion    int `json:"parser_version"`
	SegmenterVersion int `json:"segmenter_version"`
	RedactionVersion int `json:"redaction_version"`

	Files        int `json:"files"`
	Sessions     int `json:"sessions"`
	Excluded     int `json:"excluded"`
	SegmentCount int `json:"segment_count"`
	DropCount    int `json:"drop_count"`
	EventCount   int `json:"event_count"`
	Bytes        int `json:"bytes"`

	Segments   []DryRunItem   `json:"segments"`
	Drops      []Drop         `json:"drops"`
	Redactions map[string]int `json:"redactions"`
	Warnings   []string       `json:"warnings,omitempty"`
}

// DryRun performs discovery, parsing, segmentation, redaction, and size
// estimation. It never invokes an LLM.
func DryRun(ctx context.Context, opt Options) (*DryRunReport, error) {
	prepared, err := Prepare(ctx, PrepareOptions{
		Load:    opt.Load,
		Segment: opt.Segment,
		Redact:  opt.Redact,
	})
	if prepared == nil {
		return nil, err
	}

	now := time.Now
	if opt.Now != nil {
		now = opt.Now
	}
	report := &DryRunReport{
		GeneratedAt:      now(),
		Since:            opt.SinceLabel,
		ParserVersion:    version.ParserVersion,
		SegmenterVersion: version.SegmenterVersion,
		RedactionVersion: version.RedactionVersion,
		Files:            prepared.Files,
		Sessions:         prepared.Sessions,
		Excluded:         prepared.Excluded,
		EventCount:       prepared.EventCount,
		Redactions:       prepared.Redactions,
		Warnings:         prepared.Warnings,
		Segments:         make([]DryRunItem, 0, len(prepared.Segments)),
		Drops:            prepared.Drops,
	}

	for _, ps := range prepared.Segments {
		seg := ps.Segment
		item := DryRunItem{
			Source:        seg.Source,
			Project:       seg.Project,
			SessionID:     seg.SessionID,
			SegmentID:     seg.ID,
			Hash:          seg.Hash,
			Title:         seg.Title,
			Boundary:      string(seg.Boundary),
			Start:         seg.Start,
			End:           seg.End,
			EventCount:    seg.EventCount,
			UserTextCount: seg.UserTextCount,
			Bytes:         ps.Bytes,
			Redactions:    ps.Redactions,
		}
		if opt.IncludeEvents {
			item.Events = previewEvents(seg.Events)
		}
		report.Segments = append(report.Segments, item)
		report.Bytes += item.Bytes
	}

	report.SegmentCount = len(report.Segments)
	report.DropCount = len(report.Drops)
	return report, err
}

func previewEvents(events []ingest.Event) []EventPreview {
	out := make([]EventPreview, 0, len(events))
	for _, ev := range events {
		text := ev.Text
		switch ev.Kind {
		case ingest.KindToolResult:
			text = ev.ToolResult
		case ingest.KindToolUse:
			if len(ev.ToolInput) > 0 {
				text = string(ev.ToolInput)
			} else {
				text = ev.ToolInputText
			}
		}
		out = append(out, EventPreview{
			Seq:  ev.Seq,
			Kind: string(ev.Kind),
			Role: ev.Role,
			Tool: ev.ToolName,
			Time: ev.Time,
			Text: truncatePreview(text, 300),
		})
	}
	return out
}

func truncatePreview(s string, limit int) string {
	runes := []rune(s)
	if len(runes) <= limit {
		return s
	}
	return string(runes[:limit]) + "..."
}
