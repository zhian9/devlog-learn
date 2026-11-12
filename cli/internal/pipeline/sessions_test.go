package pipeline

import (
	"testing"
	"time"

	"devlog-learn/internal/segment"
)

func TestSelectableSessionsGroupsAndCounts(t *testing.T) {
	base := time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC)
	prepared := &Prepared{
		Warnings: []string{"one warning"},
		Drops:    []Drop{{SegmentID: "drop"}},
		Segments: []PreparedSegment{
			{
				Segment: segment.Segment{
					ID: "seg-a1", Source: "claude-code", SessionID: "s1", Project: "demo",
					Title: "First problem", Start: base, End: base.Add(10 * time.Minute),
					EventCount: 4, UserTextCount: 2,
				},
				Bytes: 100,
			},
			{
				Segment: segment.Segment{
					ID: "seg-a2", Source: "claude-code", SessionID: "s1", Project: "demo",
					Title: "Second problem", Start: base.Add(time.Hour), End: base.Add(70 * time.Minute),
					EventCount: 3, UserTextCount: 1,
				},
				Bytes: 200,
			},
			{
				Segment: segment.Segment{
					ID: "seg-b1", Source: "codex", SessionID: "s2", Project: "other",
					Title: "Codex problem", Start: base.Add(2 * time.Hour), End: base.Add(130 * time.Minute),
					EventCount: 5, UserTextCount: 2,
				},
				Bytes: 300,
			},
		},
	}

	list := BuildSessionList(prepared, map[string]bool{"seg-a1": true, "seg-b1": true}, base, "7d")
	if len(list.Sessions) != 2 {
		t.Fatalf("sessions = %d, want 2", len(list.Sessions))
	}
	// Newest session first.
	if list.Sessions[0].Selector != "codex:s2" || list.Sessions[1].Selector != "claude-code:s1" {
		t.Fatalf("order = %q, %q", list.Sessions[0].Selector, list.Sessions[1].Selector)
	}
	claude := list.Sessions[1]
	if claude.Project != "demo" || claude.SegmentCount != 2 || claude.Extracted != 1 {
		t.Fatalf("claude session = %+v", claude)
	}
	if claude.EventCount != 7 || claude.UserTextCount != 3 || claude.Bytes != 300 {
		t.Fatalf("claude totals = %+v", claude)
	}
	if claude.Start != base || !claude.End.Equal(base.Add(70*time.Minute)) {
		t.Fatalf("claude range = %s..%s", claude.Start, claude.End)
	}
	if claude.Segments[0].ID != "seg-a1" || claude.Segments[1].ID != "seg-a2" {
		t.Fatalf("segment order = %+v", claude.Segments)
	}
	if !claude.Segments[0].Extracted || claude.Segments[1].Extracted {
		t.Fatalf("extracted flags = %+v", claude.Segments)
	}
	if list.SegmentCount != 3 || list.DropCount != 1 || len(list.Warnings) != 1 {
		t.Fatalf("list metadata = %+v", list)
	}
	if list.Since != "7d" || list.GeneratedAt.IsZero() {
		t.Fatalf("list header = %+v", list)
	}
}

func TestSelectableSessionsEmptyAndNilExtracted(t *testing.T) {
	if got := SelectableSessions(nil, nil); got != nil {
		t.Fatalf("nil prepared = %+v", got)
	}
	prepared := &Prepared{Segments: []PreparedSegment{{
		Segment: segment.Segment{
			ID: "seg", Source: "claude-code", SessionID: "s", Project: "p", Title: "t",
			Start: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		},
	}}}
	sessions := SelectableSessions(prepared, nil)
	if len(sessions) != 1 || sessions[0].Extracted != 0 {
		t.Fatalf("sessions = %+v", sessions)
	}
}
