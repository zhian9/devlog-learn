package segment

import (
	"strings"
	"testing"
	"time"

	"devlog-learn/internal/ingest"
)

var baseTime = time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)

func makeSession(events []ingest.Event) *ingest.Session {
	return &ingest.Session{
		ID:      "session-1",
		Source:  "claude-code",
		Project: "demo",
		Cwd:     `D:\work\demo`,
		Events:  events,
	}
}

func user(seq, line int, at time.Time, text string) ingest.Event {
	return ingest.Event{
		Kind: ingest.KindUserText,
		Role: "user",
		Time: at,
		Text: text,
		Line: line,
		Seq:  seq,
	}
}

func assistant(seq, line int, at time.Time, text string) ingest.Event {
	return ingest.Event{
		Kind: ingest.KindAssistantText,
		Role: "assistant",
		Time: at,
		Text: text,
		Line: line,
		Seq:  seq,
	}
}

func toolUse(seq, line int, at time.Time) ingest.Event {
	return ingest.Event{
		Kind:     ingest.KindToolUse,
		Role:     "assistant",
		Time:     at,
		ToolName: "Bash",
		Seq:      seq,
		Line:     line,
	}
}

func TestSplitKeepsTwoEventConversation(t *testing.T) {
	sess := makeSession([]ingest.Event{
		user(1, 1, baseTime, "Fix the failing test"),
		assistant(2, 2, baseTime.Add(time.Minute), "I fixed it."),
	})

	segs, drops := Split(sess, DefaultOptions())
	if len(drops) != 0 {
		t.Fatalf("drops = %+v", drops)
	}
	if len(segs) != 1 {
		t.Fatalf("segments = %d, want 1", len(segs))
	}
	seg := segs[0]
	if seg.EventCount != 2 || seg.UserTextCount != 1 {
		t.Fatalf("counts = %d/%d", seg.EventCount, seg.UserTextCount)
	}
	if seg.Title != "Fix the failing test" {
		t.Fatalf("title = %q", seg.Title)
	}
	if len(seg.ID) != 16 {
		t.Fatalf("id = %q", seg.ID)
	}
	if seg.Boundary != ReasonSessionStart {
		t.Fatalf("boundary = %q", seg.Boundary)
	}
}

func TestSplitMergesFollowup(t *testing.T) {
	sess := makeSession([]ingest.Event{
		user(1, 1, baseTime, "The test fails with a timeout"),
		assistant(2, 2, baseTime.Add(time.Minute), "I will check it."),
		toolUse(3, 3, baseTime.Add(2*time.Minute)),
		user(4, 4, baseTime.Add(3*time.Minute), "还是不行"),
		assistant(5, 5, baseTime.Add(4*time.Minute), "Let me try another fix."),
	})

	segs, drops := Split(sess, DefaultOptions())
	if len(drops) != 0 {
		t.Fatalf("drops = %+v", drops)
	}
	if len(segs) != 1 {
		t.Fatalf("segments = %d, want 1: %+v", len(segs), boundaries(segs))
	}
	if segs[0].UserTextCount != 2 {
		t.Fatalf("user texts = %d, want 2", segs[0].UserTextCount)
	}
	if segs[0].EventCount != 5 {
		t.Fatalf("events = %d, want 5", segs[0].EventCount)
	}
}

func TestSplitNewProblem(t *testing.T) {
	sess := makeSession([]ingest.Event{
		user(1, 1, baseTime, "Tests fail with a timeout"),
		assistant(2, 2, baseTime.Add(time.Minute), "I fixed the timeout."),
		user(3, 3, baseTime.Add(2*time.Minute), "Please add a --verbose flag"),
		assistant(4, 4, baseTime.Add(3*time.Minute), "Added."),
	})

	segs, drops := Split(sess, DefaultOptions())
	if len(drops) != 0 {
		t.Fatalf("drops = %+v", drops)
	}
	if len(segs) != 2 {
		t.Fatalf("segments = %d, want 2: %+v", len(segs), boundaries(segs))
	}
	if segs[1].Boundary != ReasonNewProblem {
		t.Fatalf("second boundary = %q", segs[1].Boundary)
	}
	if !strings.Contains(segs[1].BoundaryDetail, "keyword:") {
		t.Fatalf("detail = %q", segs[1].BoundaryDetail)
	}
	if segs[0].Title == segs[1].Title {
		t.Fatalf("titles should differ: %q", segs[0].Title)
	}
}

func TestSplitIdleGap(t *testing.T) {
	sess := makeSession([]ingest.Event{
		user(1, 1, baseTime, "Fix the failing test"),
		assistant(2, 2, baseTime.Add(time.Minute), "Done."),
		user(3, 3, baseTime.Add(32*time.Minute), "Let us look at the logs"),
		assistant(4, 4, baseTime.Add(33*time.Minute), "Here."),
	})

	segs, _ := Split(sess, DefaultOptions())
	if len(segs) != 2 {
		t.Fatalf("segments = %d, want 2: %+v", len(segs), boundaries(segs))
	}
	if segs[1].Boundary != ReasonGap {
		t.Fatalf("second boundary = %q", segs[1].Boundary)
	}
}

func TestSplitTopicShift(t *testing.T) {
	sess := makeSession([]ingest.Event{
		user(1, 1, baseTime, "Fix the failing test"),
		assistant(2, 2, baseTime.Add(time.Minute), "Done."),
		user(3, 3, baseTime.Add(2*time.Minute), "Deploy production now"),
		assistant(4, 4, baseTime.Add(3*time.Minute), "Deployed."),
	})

	segs, _ := Split(sess, DefaultOptions())
	if len(segs) != 2 {
		t.Fatalf("segments = %d, want 2: %+v", len(segs), boundaries(segs))
	}
	if segs[1].Boundary != ReasonTopicShift {
		t.Fatalf("second boundary = %q, detail=%q", segs[1].Boundary, segs[1].BoundaryDetail)
	}
}

func TestSplitMergesClarificationBeforeAssistant(t *testing.T) {
	sess := makeSession([]ingest.Event{
		user(1, 1, baseTime, "Tests fail"),
		user(2, 2, baseTime.Add(10*time.Second), "It is a timeout in CI"),
		assistant(3, 3, baseTime.Add(time.Minute), "I see."),
	})

	segs, drops := Split(sess, DefaultOptions())
	if len(drops) != 0 {
		t.Fatalf("drops = %+v", drops)
	}
	if len(segs) != 1 || segs[0].UserTextCount != 2 {
		t.Fatalf("segments = %+v", boundaries(segs))
	}
}

func TestSplitPreservesLeadingToolEvents(t *testing.T) {
	sess := makeSession([]ingest.Event{
		{Kind: ingest.KindToolResult, Role: "tool", Time: baseTime, ToolResult: "earlier output", Seq: 1, Line: 1},
		user(2, 2, baseTime.Add(time.Minute), "Fix the failing test"),
		assistant(3, 3, baseTime.Add(2*time.Minute), "Done."),
	})

	segs, drops := Split(sess, DefaultOptions())
	if len(drops) != 0 {
		t.Fatalf("drops = %+v", drops)
	}
	if len(segs) != 1 {
		t.Fatalf("segments = %d, want 1", len(segs))
	}
	if len(segs[0].Events) != 3 || segs[0].Events[0].Kind != ingest.KindToolResult {
		t.Fatalf("events = %d, first = %q", len(segs[0].Events), segs[0].Events[0].Kind)
	}
	if segs[0].EventCount != 3 {
		t.Fatalf("valid events = %d, want 3", segs[0].EventCount)
	}
}

func TestSplitSameLineUserBlocks(t *testing.T) {
	sess := makeSession([]ingest.Event{
		user(1, 7, baseTime, "First block"),
		user(2, 7, baseTime, "Second block"),
		assistant(3, 8, baseTime.Add(time.Minute), "Done."),
	})

	segs, _ := Split(sess, DefaultOptions())
	if len(segs) != 1 {
		t.Fatalf("segments = %d, want 1", len(segs))
	}
	if segs[0].UserTextCount != 2 {
		t.Fatalf("user texts = %d, want 2", segs[0].UserTextCount)
	}
}

func TestSplitFilteringRecordsReasons(t *testing.T) {
	t.Run("short but valid conversation is kept", func(t *testing.T) {
		sess := makeSession([]ingest.Event{
			user(1, 1, baseTime, "hi"),
			assistant(2, 2, baseTime.Add(time.Minute), "hello"),
		})
		segs, drops := Split(sess, DefaultOptions())
		if len(segs) != 1 || len(drops) != 0 {
			t.Fatalf("segments=%d drops=%+v", len(segs), drops)
		}
	})

	t.Run("no assistant reply", func(t *testing.T) {
		sess := makeSession([]ingest.Event{
			user(1, 1, baseTime, "Fix the failing test"),
		})
		segs, drops := Split(sess, DefaultOptions())
		if len(segs) != 0 || len(drops) != 1 {
			t.Fatalf("segments=%d drops=%+v", len(segs), drops)
		}
		if !hasReason(drops[0].Reasons, DropTooFewEvents) || !hasReason(drops[0].Reasons, DropNoAssistantReply) {
			t.Fatalf("reasons = %v", drops[0].Reasons)
		}
		if drops[0].Detail == "" {
			t.Fatal("drop detail must not be empty")
		}
	})

	t.Run("slash command only", func(t *testing.T) {
		sess := makeSession([]ingest.Event{
			user(1, 1, baseTime, "/clear"),
			assistant(2, 2, baseTime.Add(time.Minute), "Cleared."),
		})
		segs, drops := Split(sess, DefaultOptions())
		if len(segs) != 0 || len(drops) != 1 {
			t.Fatalf("segments=%d drops=%+v", len(segs), drops)
		}
		if !hasReason(drops[0].Reasons, DropSlashOnly) {
			t.Fatalf("reasons = %v", drops[0].Reasons)
		}
	})

	t.Run("no user text", func(t *testing.T) {
		sess := makeSession([]ingest.Event{
			{Kind: ingest.KindToolResult, Role: "tool", Time: baseTime, ToolResult: "output", Seq: 1, Line: 1},
			assistant(2, 2, baseTime.Add(time.Minute), "Done."),
		})
		segs, drops := Split(sess, DefaultOptions())
		if len(segs) != 0 || len(drops) != 1 {
			t.Fatalf("segments=%d drops=%+v", len(segs), drops)
		}
		if !hasReason(drops[0].Reasons, DropNoUserText) {
			t.Fatalf("reasons = %v", drops[0].Reasons)
		}
	})

	t.Run("empty session", func(t *testing.T) {
		segs, drops := Split(makeSession(nil), DefaultOptions())
		if len(segs) != 0 || len(drops) != 0 {
			t.Fatalf("segments=%d drops=%+v", len(segs), drops)
		}
	})
}

func TestSplitUsesSessionTitleForSingleSegment(t *testing.T) {
	sess := makeSession([]ingest.Event{
		user(1, 1, baseTime, "Fix it"),
		assistant(2, 2, baseTime.Add(time.Minute), "Done."),
	})
	sess.Title = "Custom session title"

	segs, _ := Split(sess, DefaultOptions())
	if len(segs) != 1 || segs[0].Title != "Custom session title" {
		t.Fatalf("title = %q", segs[0].Title)
	}
}

func TestSegmentIDStableWhenContentAppended(t *testing.T) {
	events := []ingest.Event{
		user(1, 1, baseTime, "Fix the failing test"),
		assistant(2, 2, baseTime.Add(time.Minute), "Working on it."),
	}
	sess := makeSession(events)
	first, _ := Split(sess, DefaultOptions())
	if len(first) != 1 {
		t.Fatalf("first segments = %d", len(first))
	}
	firstHash := ComputeHash(first[0])

	sess.Events = append(sess.Events, assistant(3, 3, baseTime.Add(2*time.Minute), "Fixed."))
	second, _ := Split(sess, DefaultOptions())
	if len(second) != 1 {
		t.Fatalf("second segments = %d", len(second))
	}
	if second[0].ID != first[0].ID {
		t.Fatalf("id changed after append: %q -> %q", first[0].ID, second[0].ID)
	}
	if ComputeHash(second[0]) == firstHash {
		t.Fatal("hash must change when content changes")
	}
}

func TestSegmentIDIsSessionScoped(t *testing.T) {
	events := []ingest.Event{
		user(1, 1, baseTime, "Fix the failing test"),
		assistant(2, 2, baseTime.Add(time.Minute), "Done."),
	}
	a, _ := Split(makeSession(events), DefaultOptions())
	bSess := makeSession(events)
	bSess.ID = "session-2"
	b, _ := Split(bSess, DefaultOptions())
	if a[0].ID == b[0].ID {
		t.Fatalf("ids collided across sessions: %q", a[0].ID)
	}
}

func TestBoundaryChangeChangesIdentity(t *testing.T) {
	first := user(1, 1, baseTime, "Fix the failing test")
	reply := assistant(2, 2, baseTime.Add(time.Minute), "Done.")

	splitSession := makeSession([]ingest.Event{
		first,
		reply,
		user(3, 3, baseTime.Add(2*time.Minute), "Please add logging"),
		assistant(4, 4, baseTime.Add(3*time.Minute), "Added."),
	})
	mergedSession := makeSession([]ingest.Event{
		first,
		reply,
		user(3, 3, baseTime.Add(2*time.Minute), "The test fix looks good"),
		assistant(4, 4, baseTime.Add(3*time.Minute), "Added."),
	})

	splitSegs, _ := Split(splitSession, DefaultOptions())
	mergedSegs, _ := Split(mergedSession, DefaultOptions())
	if len(splitSegs) != 2 || len(mergedSegs) != 1 {
		t.Fatalf("split=%d merged=%d", len(splitSegs), len(mergedSegs))
	}
	// The unchanged first boundary keeps its identity.
	if splitSegs[0].ID != mergedSegs[0].ID {
		t.Fatalf("first segment id changed: %q vs %q", splitSegs[0].ID, mergedSegs[0].ID)
	}

	// A boundary decision change must change the derived identity.
	base := Segment{Source: "claude-code", SessionID: "session-1", Start: baseTime}
	base.Boundary = ReasonNewProblem
	newProblemID := makeID(base, 2, []string{"Please add logging"})
	base.Boundary = ReasonTopicShift
	topicShiftID := makeID(base, 2, []string{"Please add logging"})
	if newProblemID == topicShiftID {
		t.Fatalf("ids must differ by boundary reason: %q", newProblemID)
	}
}

func TestMergeFollowupsDisabled(t *testing.T) {
	sess := makeSession([]ingest.Event{
		user(1, 1, baseTime, "Fix the failing test"),
		assistant(2, 2, baseTime.Add(time.Minute), "Done."),
		user(3, 3, baseTime.Add(2*time.Minute), "Also add a note"),
		assistant(4, 4, baseTime.Add(3*time.Minute), "Added."),
	})
	opts := DefaultOptions()
	opts.MergeFollowups = false
	segs, _ := Split(sess, opts)
	if len(segs) != 2 {
		t.Fatalf("segments = %d, want 2", len(segs))
	}
	if segs[1].Boundary != ReasonNewProblem || !strings.Contains(segs[1].BoundaryDetail, "disabled") {
		t.Fatalf("second boundary = %q / %q", segs[1].Boundary, segs[1].BoundaryDetail)
	}
}

func TestSplitAllSkipsExcludedSessions(t *testing.T) {
	included := makeSession([]ingest.Event{
		user(1, 1, baseTime, "Fix the failing test"),
		assistant(2, 2, baseTime.Add(time.Minute), "Done."),
	})
	excluded := makeSession([]ingest.Event{
		user(1, 1, baseTime, "Subagent task"),
		assistant(2, 2, baseTime.Add(time.Minute), "Done."),
	})
	excluded.Excluded = true

	segs, drops := SplitAll([]*ingest.Session{included, excluded}, DefaultOptions())
	if len(segs) != 1 || len(drops) != 0 {
		t.Fatalf("segments=%d drops=%+v", len(segs), drops)
	}
}

func TestTextHelpers(t *testing.T) {
	if !IsContinuation("还是不行") || !IsContinuation("Still failing") || !IsContinuation("ok") {
		t.Fatal("expected continuation phrases")
	}
	if IsContinuation("Please implement a new flag") {
		t.Fatal("new request must not be a continuation")
	}
	if !IsNewProblem("Please add a --verbose flag") {
		t.Fatal("expected new-problem signal")
	}
	if IsNewProblem("谢谢") {
		t.Fatal("plain acknowledgement is not a new problem")
	}
	if got := Similarity("fix failing test", "fix failing test"); got != 1 {
		t.Fatalf("similarity = %v, want 1", got)
	}
	if got := Similarity("fix failing test", "deploy production"); got != 0 {
		t.Fatalf("similarity = %v, want 0", got)
	}
}

func TestComputeHashStable(t *testing.T) {
	seg := Segment{Events: []ingest.Event{
		user(1, 1, baseTime, "Fix the failing test"),
		assistant(2, 2, baseTime.Add(time.Minute), "Done."),
	}}
	a := ComputeHash(seg)
	b := ComputeHash(seg)
	if a != b {
		t.Fatalf("hash not stable: %q != %q", a, b)
	}
	seg.Events[1].Text = "Changed."
	if ComputeHash(seg) == a {
		t.Fatal("hash must change when content changes")
	}
}

func TestByteSizeAndHasContent(t *testing.T) {
	seg := Segment{Events: []ingest.Event{
		user(1, 1, baseTime, "hello"),
	}}
	if ByteSize(seg) <= len("hello") {
		t.Fatalf("byte size = %d", ByteSize(seg))
	}
	if !HasContent(seg) {
		t.Fatal("expected content")
	}
	seg.Events[0].Text = "  "
	if HasContent(seg) {
		t.Fatal("blank content must not count")
	}
}

func boundaries(segs []Segment) []Reason {
	out := make([]Reason, len(segs))
	for i, seg := range segs {
		out[i] = seg.Boundary
	}
	return out
}

func hasReason(reasons []DropReason, want DropReason) bool {
	for _, reason := range reasons {
		if reason == want {
			return true
		}
	}
	return false
}
