package pipeline

import (
	"context"
	"time"

	"devlog-learn/internal/redact"
	"devlog-learn/internal/segment"
)

// PrepareOptions controls local parsing, segmentation, and redaction.
type PrepareOptions struct {
	Load    LoadOptions
	Segment segment.Options
	Redact  redact.Config
}

// PreparedSegment is a redacted, hash-stamped segment ready for extraction.
type PreparedSegment struct {
	Segment    segment.Segment
	Redactions map[string]int
	Bytes      int
}

// Drop is a filtered segment with its reasons. It is used by dry-run and by
// extraction reports.
type Drop struct {
	Source    string               `json:"source"`
	Project   string               `json:"project"`
	SessionID string               `json:"session_id"`
	SegmentID string               `json:"segment_id"`
	Start     time.Time            `json:"start"`
	End       time.Time            `json:"end"`
	Reasons   []segment.DropReason `json:"reasons"`
	Detail    string               `json:"detail,omitempty"`

	EventCount    int `json:"event_count"`
	UserTextCount int `json:"user_text_count"`
}

// Prepared is the shared local stage used by dry-run and extraction.
type Prepared struct {
	Files      int
	Sessions   int
	Excluded   int
	EventCount int
	Segments   []PreparedSegment
	Drops      []Drop
	Redactions map[string]int
	Warnings   []string
	Redactor   *redact.Redactor
}

// Prepare performs discovery, parsing, segmentation, redaction, and content
// hashing. It never calls an LLM.
func Prepare(ctx context.Context, opt PrepareOptions) (*Prepared, error) {
	redactor, err := redact.New(opt.Redact)
	if err != nil {
		return nil, err
	}

	prepared := &Prepared{
		Segments:   []PreparedSegment{},
		Drops:      []Drop{},
		Redactions: map[string]int{},
		Redactor:   redactor,
	}

	loaded, warnings, loadErr := LoadSessions(opt.Load)
	prepared.Warnings = warnings
	prepared.Files = len(loaded)
	prepared.Sessions = len(loaded)

	for _, entry := range loaded {
		if err := ctx.Err(); err != nil {
			return prepared, err
		}

		sess := entry.Session
		prepared.EventCount += len(sess.Events)
		if sess.Excluded {
			prepared.Excluded++
			continue
		}

		segs, drops := segment.Split(sess, opt.Segment)
		for _, drop := range drops {
			prepared.Drops = append(prepared.Drops, makeDrop(drop))
		}

		for _, seg := range segs {
			if err := ctx.Err(); err != nil {
				return prepared, err
			}

			redacted, stats := redactor.Segment(seg)
			mergeRedactions(prepared.Redactions, stats)

			if !segment.HasContent(redacted) {
				prepared.Drops = append(prepared.Drops, Drop{
					Source:        seg.Source,
					Project:       seg.Project,
					SessionID:     seg.SessionID,
					SegmentID:     seg.ID,
					Start:         seg.Start,
					End:           seg.End,
					Reasons:       []segment.DropReason{segment.DropEmptyAfterRedact},
					Detail:        "segment content is empty after redaction",
					EventCount:    seg.EventCount,
					UserTextCount: seg.UserTextCount,
				})
				continue
			}

			segment.SetHash(&redacted)
			prepared.Segments = append(prepared.Segments, PreparedSegment{
				Segment:    redacted,
				Redactions: stats,
				Bytes:      segment.ByteSize(redacted),
			})
		}
	}

	return prepared, loadErr
}

func makeDrop(drop segment.Dropped) Drop {
	return Drop{
		Source:        drop.Source,
		Project:       drop.Project,
		SessionID:     drop.SessionID,
		SegmentID:     drop.ID,
		Start:         drop.Start,
		End:           drop.End,
		Reasons:       drop.Reasons,
		Detail:        drop.Detail,
		EventCount:    drop.EventCount,
		UserTextCount: drop.UserTextCount,
	}
}

func mergeRedactions(dst, src map[string]int) {
	for key, value := range src {
		dst[key] += value
	}
}
