package pipeline

import (
	"sort"
	"time"
)

// SelectableSegment is one prepared segment shown to a human or a UI picker.
type SelectableSegment struct {
	ID            string    `json:"id"`
	Title         string    `json:"title"`
	Start         time.Time `json:"start"`
	End           time.Time `json:"end"`
	EventCount    int       `json:"event_count"`
	UserTextCount int       `json:"user_text_count"`
	Bytes         int       `json:"bytes"`
	Extracted     bool      `json:"extracted"`
}

// SelectableSession groups prepared segments by source+session. Selector is the
// value accepted by --session.
type SelectableSession struct {
	Selector      string              `json:"selector"`
	Source        string              `json:"source"`
	SessionID     string              `json:"session_id"`
	Project       string              `json:"project"`
	Title         string              `json:"title"`
	Start         time.Time           `json:"start"`
	End           time.Time           `json:"end"`
	EventCount    int                 `json:"event_count"`
	UserTextCount int                 `json:"user_text_count"`
	Bytes         int                 `json:"bytes"`
	SegmentCount  int                 `json:"segment_count"`
	Extracted     int                 `json:"extracted"`
	Segments      []SelectableSegment `json:"segments"`
}

// SessionList is the model-free listing used by the sessions command and UI.
type SessionList struct {
	GeneratedAt  time.Time           `json:"generated_at"`
	Since        string              `json:"since,omitempty"`
	Sessions     []SelectableSession `json:"sessions"`
	Drops        []Drop              `json:"drops,omitempty"`
	Warnings     []string            `json:"warnings,omitempty"`
	SegmentCount int                 `json:"segment_count"`
	DropCount    int                 `json:"drop_count"`
}

// SelectableSessions groups prepared segments by session. extracted is keyed by
// SegmentID and marks segments whose canonical Doc already exists; it is a
// cheap "already extracted" signal, not a cache-key prediction.
func SelectableSessions(prepared *Prepared, extracted map[string]bool) []SelectableSession {
	if prepared == nil {
		return nil
	}
	bySelector := make(map[string]*SelectableSession)
	for _, ps := range prepared.Segments {
		seg := ps.Segment
		selector := seg.Source + ":" + seg.SessionID
		session, ok := bySelector[selector]
		if !ok {
			session = &SelectableSession{
				Selector:  selector,
				Source:    seg.Source,
				SessionID: seg.SessionID,
				Project:   seg.Project,
				Title:     seg.Title,
				Start:     seg.Start,
				End:       seg.End,
			}
			bySelector[selector] = session
		}
		if session.Project == "" {
			session.Project = seg.Project
		}
		if session.Title == "" {
			session.Title = seg.Title
		}
		if earlier(seg.Start, session.Start) {
			session.Start = seg.Start
		}
		if later(seg.End, session.End) {
			session.End = seg.End
		}
		session.EventCount += seg.EventCount
		session.UserTextCount += seg.UserTextCount
		session.Bytes += ps.Bytes
		session.SegmentCount++

		item := SelectableSegment{
			ID:            seg.ID,
			Title:         seg.Title,
			Start:         seg.Start,
			End:           seg.End,
			EventCount:    seg.EventCount,
			UserTextCount: seg.UserTextCount,
			Bytes:         ps.Bytes,
			Extracted:     extracted[seg.ID],
		}
		if item.Extracted {
			session.Extracted++
		}
		session.Segments = append(session.Segments, item)
	}

	out := make([]SelectableSession, 0, len(bySelector))
	for _, session := range bySelector {
		sort.SliceStable(session.Segments, func(i, j int) bool {
			a, b := session.Segments[i], session.Segments[j]
			if !a.Start.Equal(b.Start) {
				return a.Start.Before(b.Start)
			}
			return a.ID < b.ID
		})
		out = append(out, *session)
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if !a.Start.Equal(b.Start) {
			if a.Start.IsZero() {
				return false
			}
			if b.Start.IsZero() {
				return true
			}
			return a.Start.After(b.Start)
		}
		return a.Selector < b.Selector
	})
	return out
}

// BuildSessionList wraps SelectableSessions with run metadata for JSON output.
func BuildSessionList(prepared *Prepared, extracted map[string]bool, now time.Time, since string) *SessionList {
	list := &SessionList{
		GeneratedAt: now.UTC(),
		Since:       since,
		Sessions:    SelectableSessions(prepared, extracted),
	}
	if prepared != nil {
		list.Drops = prepared.Drops
		list.Warnings = prepared.Warnings
		list.SegmentCount = len(prepared.Segments)
		list.DropCount = len(prepared.Drops)
	}
	return list
}

func earlier(candidate, current time.Time) bool {
	if current.IsZero() {
		return !candidate.IsZero()
	}
	return candidate.Before(current)
}

func later(candidate, current time.Time) bool {
	if current.IsZero() {
		return !candidate.IsZero()
	}
	return candidate.After(current)
}
