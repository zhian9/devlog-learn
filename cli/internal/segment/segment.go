// Package segment turns a parsed session into problem-sized units.
//
// SegmentID is business identity; SegmentHash is content identity. They are
// deliberately separate: appending messages keeps the ID and changes the hash,
// while a moved boundary changes the ID.
package segment

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"devlog-learn/internal/ingest"
	"devlog-learn/internal/version"
)

// Reason records why a segment started.
type Reason string

const (
	ReasonSessionStart Reason = "session_start"
	ReasonNewProblem   Reason = "new_problem"
	ReasonGap          Reason = "idle_gap"
	ReasonTopicShift   Reason = "topic_shift"
)

// DropReason records why a produced segment was filtered out.
type DropReason string

const (
	DropTooFewEvents     DropReason = "too_few_events"
	DropNoUserText       DropReason = "no_user_text"
	DropNoAssistantReply DropReason = "no_assistant_reply"
	DropSlashOnly        DropReason = "slash_command_only"
	DropEmptyAfterRedact DropReason = "empty_after_redaction"
)

// Options controls segmentation. Zero values are normalised by Split.
type Options struct {
	// Gap starts a new segment when the idle time between events reaches it.
	Gap time.Duration
	// MergeFollowups keeps same-topic follow-ups inside the current segment.
	MergeFollowups bool
	// TopicFloor is the similarity below which a new user message is treated
	// as a topic shift.
	TopicFloor float64
	// MinEvents is the minimum number of valid events (user/assistant/tool).
	MinEvents int
}

func DefaultOptions() Options {
	return Options{
		Gap:            30 * time.Minute,
		MergeFollowups: true,
		TopicFloor:     0.10,
		MinEvents:      2,
	}
}

func (o Options) normalize() Options {
	def := DefaultOptions()
	if o.Gap < 0 {
		o.Gap = 0
	}
	if o.TopicFloor <= 0 || o.TopicFloor > 1 {
		o.TopicFloor = def.TopicFloor
	}
	if o.MinEvents <= 0 {
		o.MinEvents = def.MinEvents
	}
	return o
}

// Segment is one problem-sized unit of a session.
type Segment struct {
	// ID is stable business identity. It changes when the boundary, first
	// user turn, or segmenter version changes.
	ID string
	// Hash is the content fingerprint, computed after redaction.
	Hash string

	Source    string
	SessionID string
	Project   string
	Cwd       string
	Title     string

	Start time.Time
	End   time.Time

	Boundary       Reason
	BoundaryDetail string

	Events        []ingest.Event
	EventCount    int // valid conversation events
	UserTextCount int
}

// Dropped is a segment removed by filtering. It is never silently discarded:
// DryRun and Run both report these.
type Dropped struct {
	ID        string
	Source    string
	SessionID string
	Project   string
	Start     time.Time
	End       time.Time
	Reasons   []DropReason
	Detail    string

	EventCount    int
	UserTextCount int
}

type segmentBuilder struct {
	events           []ingest.Event
	reason           Reason
	detail           string
	firstUserOrdinal int
	userTexts        []string
	lastUserLine     int
	lastEventTime    time.Time
	started          bool
}

type splitState struct {
	sess        *ingest.Session
	opt         Options
	cur         segmentBuilder
	pending     []ingest.Event
	out         []Segment
	drops       []Dropped
	userOrdinal int
}

// Split segments one parsed session. Events with KindMeta/KindUnknown are
// considered non-conversational and are not copied into segments; they remain
// available on the original Session for diagnostics.
func Split(sess *ingest.Session, opt Options) ([]Segment, []Dropped) {
	if sess == nil {
		return nil, nil
	}
	opt = opt.normalize()
	st := &splitState{sess: sess, opt: opt}

	for _, ev := range sess.Events {
		if ev.Kind == ingest.KindMeta || ev.Kind == ingest.KindUnknown {
			continue
		}

		if ev.Kind == ingest.KindUserText {
			st.userOrdinal++
			st.handleUser(ev)
			continue
		}

		if !st.cur.started {
			// Leading assistant/tool events have no user request yet. Preserve
			// them and attach them once the first user message arrives.
			st.pending = append(st.pending, ev)
			continue
		}
		st.append(ev)
	}
	st.flush()

	// A session with tool/assistant activity but no user request cannot form
	// a problem segment. Record the skip instead of silently dropping it.
	if len(st.out) == 0 && len(st.drops) == 0 && st.userOrdinal == 0 {
		valid, userTexts, _, _ := countEvents(sess.Events)
		if valid > 0 {
			st.drops = append(st.drops, Dropped{
				Source:        sess.Source,
				SessionID:     sess.ID,
				Project:       sess.Project,
				Start:         sess.Started,
				End:           sess.Ended,
				Reasons:       []DropReason{DropNoUserText},
				Detail:        "session contains no user text",
				EventCount:    valid,
				UserTextCount: userTexts,
			})
		}
	}

	if len(st.out) == 1 && len(st.drops) == 0 && sess.Title != "" {
		st.out[0].Title = sess.Title
	}
	return st.out, st.drops
}

// SplitAll segments all non-excluded sessions.
func SplitAll(sessions []*ingest.Session, opt Options) ([]Segment, []Dropped) {
	var out []Segment
	var drops []Dropped
	for _, sess := range sessions {
		if sess == nil || sess.Excluded {
			continue
		}
		segs, ds := Split(sess, opt)
		out = append(out, segs...)
		drops = append(drops, ds...)
	}
	return out, drops
}

func (s *splitState) handleUser(ev ingest.Event) {
	text := strings.TrimSpace(ev.Text)

	if !s.cur.started {
		s.start(ReasonSessionStart, "first user message", ev)
		s.absorbPending()
		s.appendUser(ev, text)
		return
	}

	// Multiple text blocks from one provider message share Line; they belong
	// to the same user turn.
	if ev.Line != 0 && ev.Line == s.cur.lastUserLine && len(s.cur.userTexts) > 0 {
		s.appendUser(ev, text)
		return
	}

	// A second user message before any assistant/tool response is normally a
	// clarification of the same request, unless the idle gap is large.
	if !hasAssistantResponse(s.cur.events) {
		if s.gapExceeded(ev) {
			s.flush()
			s.start(ReasonGap, "idle gap before assistant response", ev)
		}
		s.appendUser(ev, text)
		return
	}

	if !s.opt.MergeFollowups {
		s.flush()
		s.start(ReasonNewProblem, "follow-up merge disabled", ev)
		s.appendUser(ev, text)
		return
	}

	if IsContinuation(text) {
		s.appendUser(ev, text)
		return
	}

	if signal := newProblemSignal(text); signal != "" {
		s.flush()
		s.start(ReasonNewProblem, "keyword: "+signal, ev)
		s.appendUser(ev, text)
		return
	}

	if s.gapExceeded(ev) {
		s.flush()
		s.start(ReasonGap, "idle gap", ev)
		s.appendUser(ev, text)
		return
	}

	prev := ""
	if len(s.cur.userTexts) > 0 {
		prev = s.cur.userTexts[len(s.cur.userTexts)-1]
	}
	if sim := Similarity(prev, text); sim < s.opt.TopicFloor {
		s.flush()
		s.start(ReasonTopicShift, fmt.Sprintf("similarity=%.2f", sim), ev)
		s.appendUser(ev, text)
		return
	}

	s.appendUser(ev, text)
}

func (s *splitState) start(reason Reason, detail string, ev ingest.Event) {
	s.cur = segmentBuilder{
		reason:           reason,
		detail:           detail,
		firstUserOrdinal: s.userOrdinal,
		lastUserLine:     ev.Line,
		started:          true,
	}
}

func (s *splitState) absorbPending() {
	if len(s.pending) == 0 {
		return
	}
	s.cur.events = append(s.cur.events, s.pending...)
	for _, ev := range s.pending {
		if !ev.Time.IsZero() {
			s.cur.lastEventTime = ev.Time
		}
	}
	s.pending = nil
}

func (s *splitState) append(ev ingest.Event) {
	s.cur.events = append(s.cur.events, ev)
	if !ev.Time.IsZero() {
		s.cur.lastEventTime = ev.Time
	}
}

func (s *splitState) appendUser(ev ingest.Event, text string) {
	s.append(ev)
	s.cur.userTexts = append(s.cur.userTexts, text)
	s.cur.lastUserLine = ev.Line
}

func (s *splitState) gapExceeded(ev ingest.Event) bool {
	if s.opt.Gap <= 0 || ev.Time.IsZero() || s.cur.lastEventTime.IsZero() {
		return false
	}
	return ev.Time.Sub(s.cur.lastEventTime) >= s.opt.Gap
}

func (s *splitState) flush() {
	b := s.cur
	if len(b.events) == 0 {
		return
	}

	seg := Segment{
		Source:         s.sess.Source,
		SessionID:      s.sess.ID,
		Project:        s.sess.Project,
		Cwd:            s.sess.Cwd,
		Boundary:       b.reason,
		BoundaryDetail: b.detail,
		Events:         b.events,
	}
	if len(b.userTexts) > 0 {
		seg.Title = titleFromText(b.userTexts[0])
	}
	seg.Start, seg.End = eventRange(b.events)
	seg.EventCount, seg.UserTextCount, _, _ = countEvents(b.events)
	seg.ID = makeID(seg, b.firstUserOrdinal, b.userTexts)

	keep, reasons := filter(seg, s.opt)
	if keep {
		s.out = append(s.out, seg)
	} else {
		detail := strings.Join(reasonStrings(reasons), ",")
		s.drops = append(s.drops, Dropped{
			ID:            seg.ID,
			Source:        seg.Source,
			SessionID:     seg.SessionID,
			Project:       seg.Project,
			Start:         seg.Start,
			End:           seg.End,
			Reasons:       reasons,
			Detail:        detail,
			EventCount:    seg.EventCount,
			UserTextCount: seg.UserTextCount,
		})
	}
	s.cur = segmentBuilder{}
}

func titleFromText(text string) string {
	s := strings.TrimSpace(text)
	s = strings.ReplaceAll(s, "```", " ")
	s = strings.Join(strings.Fields(s), " ")
	s = strings.Trim(s, "#-*`> \t\r\n")
	if s == "" {
		return "Untitled"
	}
	runes := []rune(s)
	if len(runes) > 80 {
		return string(runes[:80]) + "..."
	}
	return s
}

func eventRange(events []ingest.Event) (time.Time, time.Time) {
	var start, end time.Time
	for _, ev := range events {
		if ev.Time.IsZero() {
			continue
		}
		if start.IsZero() || ev.Time.Before(start) {
			start = ev.Time
		}
		if end.IsZero() || ev.Time.After(end) {
			end = ev.Time
		}
	}
	return start, end
}

// countEvents returns valid conversation events, user text count, whether an
// assistant/tool response exists, and the user texts.
func countEvents(events []ingest.Event) (valid, userTexts int, assistant bool, texts []string) {
	for _, ev := range events {
		switch ev.Kind {
		case ingest.KindUserText:
			valid++
			userTexts++
			texts = append(texts, strings.TrimSpace(ev.Text))
		case ingest.KindAssistantText, ingest.KindToolUse:
			valid++
			assistant = true
		case ingest.KindToolResult:
			valid++
		}
	}
	return valid, userTexts, assistant, texts
}

func hasAssistantResponse(events []ingest.Event) bool {
	for _, ev := range events {
		if ev.Kind == ingest.KindAssistantText || ev.Kind == ingest.KindToolUse {
			return true
		}
	}
	return false
}

func filter(seg Segment, opt Options) (bool, []DropReason) {
	valid, userTexts, assistant, texts := countEvents(seg.Events)
	var reasons []DropReason

	if valid < opt.MinEvents {
		reasons = append(reasons, DropTooFewEvents)
	}
	if userTexts == 0 || allEmpty(texts) {
		reasons = append(reasons, DropNoUserText)
	}
	if !assistant {
		reasons = append(reasons, DropNoAssistantReply)
	}
	if userTexts > 0 && allSlashCommands(texts) {
		reasons = append(reasons, DropSlashOnly)
	}
	return len(reasons) == 0, reasons
}

func allEmpty(values []string) bool {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return false
		}
	}
	return true
}

var slashOnlyPattern = regexp.MustCompile(`^/[A-Za-z][A-Za-z0-9_-]*\s*$`)

func allSlashCommands(texts []string) bool {
	found := false
	for _, text := range texts {
		text = strings.TrimSpace(text)
		if text == "" {
			continue
		}
		found = true
		if !slashOnlyPattern.MatchString(text) {
			return false
		}
	}
	return found
}

func reasonStrings(reasons []DropReason) []string {
	out := make([]string, len(reasons))
	for i, reason := range reasons {
		out[i] = string(reason)
	}
	return out
}

// makeID is intentionally based on boundary identity, not segment content.
func makeID(seg Segment, firstUserOrdinal int, userTexts []string) string {
	start := "seq:" + strconv.Itoa(firstEventSeq(seg.Events))
	if !seg.Start.IsZero() {
		start = seg.Start.UTC().Format(time.RFC3339Nano)
	}
	firstText := ""
	if len(userTexts) > 0 {
		firstText = strings.Join(strings.Fields(userTexts[0]), " ")
	}
	h := sha256.New()
	writeField(h, "segment-id-v1")
	writeField(h, "segmenter="+strconv.Itoa(version.SegmenterVersion))
	writeField(h, seg.Source)
	writeField(h, seg.SessionID)
	writeField(h, start)
	writeField(h, string(seg.Boundary))
	writeField(h, strconv.Itoa(firstUserOrdinal))
	writeField(h, firstText)
	return hex.EncodeToString(h.Sum(nil))[:16]
}

func firstEventSeq(events []ingest.Event) int {
	if len(events) == 0 {
		return 0
	}
	return events[0].Seq
}

// ComputeHash fingerprints the current content. Call it after redaction.
func ComputeHash(seg Segment) string {
	h := sha256.New()
	writeField(h, "segment-hash-v1")
	for _, ev := range seg.Events {
		writeField(h, string(ev.Kind))
		writeField(h, ev.Role)
		if ev.Time.IsZero() {
			writeField(h, "")
		} else {
			writeField(h, ev.Time.UTC().Format(time.RFC3339Nano))
		}
		writeField(h, ev.Text)
		writeField(h, ev.ToolName)
		writeField(h, string(ev.ToolInput))
		writeField(h, ev.ToolInputText)
		writeField(h, ev.ToolResult)
		writeField(h, ev.ToolUseID)
		writeField(h, strconv.FormatBool(ev.IsError))
		writeField(h, strconv.FormatBool(ev.Synthetic))
		writeField(h, strconv.FormatBool(ev.Sidechain))
		writeField(h, string(ev.Raw))
		if ev.Exec == nil {
			writeField(h, "")
		} else {
			writeField(h, strconv.FormatBool(ev.Exec.Known))
			if ev.Exec.ExitCode == nil {
				writeField(h, "")
			} else {
				writeField(h, strconv.Itoa(*ev.Exec.ExitCode))
			}
			writeField(h, strconv.FormatBool(ev.Exec.TimedOut))
			writeField(h, strconv.FormatBool(ev.Exec.Canceled))
			writeField(h, ev.Exec.Stdout)
			writeField(h, ev.Exec.Stderr)
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}

// SetHash updates seg.Hash in place.
func SetHash(seg *Segment) {
	if seg != nil {
		seg.Hash = ComputeHash(*seg)
	}
}

func writeField(h interface{ Write([]byte) (int, error) }, value string) {
	_, _ = h.Write([]byte(strconv.Itoa(len(value))))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(value))
	_, _ = h.Write([]byte{0})
}

// ByteSize estimates the prompt payload size of the segment.
func ByteSize(seg Segment) int {
	total := 0
	for _, ev := range seg.Events {
		total += len(ev.Text) + len(ev.ToolName) + len(ev.ToolInput) + len(ev.ToolInputText) + len(ev.ToolResult) + len(ev.ToolUseID)
		if ev.Exec != nil {
			total += len(ev.Exec.Stdout) + len(ev.Exec.Stderr)
		}
		total += 32 // per-event structural overhead estimate
	}
	return total
}

// HasContent reports whether redaction left anything worth extracting.
func HasContent(seg Segment) bool {
	for _, ev := range seg.Events {
		if strings.TrimSpace(ev.Text) != "" ||
			strings.TrimSpace(ev.ToolInputText) != "" ||
			strings.TrimSpace(ev.ToolResult) != "" ||
			len(ev.ToolInput) > 0 ||
			strings.TrimSpace(ev.ToolName) != "" {
			return true
		}
		if ev.Exec != nil && (strings.TrimSpace(ev.Exec.Stdout) != "" || strings.TrimSpace(ev.Exec.Stderr) != "") {
			return true
		}
	}
	return false
}

var newProblemPattern = regexp.MustCompile(`(?i)(\berror\b|\bexception\b|\bpanic\b|\btraceback\b|\bstack trace\b|\bfail(?:ed|ure|ing)?\b|\bbug\b|\bcrash\b|\btimeout\b|\btimed out\b|\bcannot\b|\bcan't\b|\bunable\b|\bnot working\b|\bdoesn't work\b|\bbroken\b|\bimplement\b|\badd\b|\bcreate\b|\brefactor\b|\brename\b|\bmigrate\b|\bupgrade\b|\bchange\b|\bupdate\b|\bplease\b|\bcan you\b|\bcould you\b|\bhelp me\b|报错|错误|异常|失败|崩溃|超时|不工作|无法|不能|有问题|实现|新增|添加|创建|重构|迁移|升级|修改|改成|帮我|请)`)

func newProblemSignal(text string) string {
	return newProblemPattern.FindString(text)
}

// IsNewProblem reports whether the text looks like a new bug report or
// requirement rather than a follow-up.
func IsNewProblem(text string) bool {
	return strings.TrimSpace(newProblemSignal(text)) != ""
}

var continuationPhrases = []string{
	"继续", "接着", "还是", "仍然", "依然", "又报错", "还是不行", "没解决", "未解决",
	"同样", "再试", "重试", "仍然失败", "还是失败",
	"continue", "go on", "keep going", "still", "again", "same error", "not fixed",
	"doesn't work", "does not work", "still failing", "retry", "re-run", "rerun",
}

var acknowledgement = map[string]bool{
	"ok": true, "okay": true, "k": true, "thanks": true, "thank you": true,
	"好": true, "好的": true, "嗯": true, "可以": true, "行": true, "继续": true,
	"收到": true, "明白": true, "是的": true, "对": true,
}

// IsContinuation recognises same-topic follow-ups such as "还是不行",
// "继续", "still failing", and short acknowledgements.
func IsContinuation(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	if lower == "" {
		return false
	}
	cleaned := strings.Trim(lower, "。.!！,，;； ")
	if acknowledgement[cleaned] {
		return true
	}
	for _, phrase := range continuationPhrases {
		if strings.Contains(lower, phrase) {
			return true
		}
	}
	return false
}

// Similarity is a lexical Jaccard score over ASCII words and CJK bigrams.
func Similarity(a, b string) float64 {
	ta, tb := tokenSet(a), tokenSet(b)
	if len(ta) == 0 && len(tb) == 0 {
		return 1
	}
	if len(ta) == 0 || len(tb) == 0 {
		return 0
	}
	inter := 0
	for token := range ta {
		if _, ok := tb[token]; ok {
			inter++
		}
	}
	union := len(ta) + len(tb) - inter
	if union == 0 {
		return 1
	}
	return float64(inter) / float64(union)
}

func tokenSet(s string) map[string]struct{} {
	out := map[string]struct{}{}
	var word []rune

	flush := func() {
		if len(word) == 0 {
			return
		}
		ascii := true
		for _, r := range word {
			if r > unicode.MaxASCII {
				ascii = false
				break
			}
		}
		if ascii {
			if len(word) >= 2 {
				out[string(word)] = struct{}{}
			}
		} else if len(word) == 1 {
			out[string(word)] = struct{}{}
		} else {
			for i := 0; i+1 < len(word); i++ {
				out[string(word[i:i+2])] = struct{}{}
			}
		}
		word = word[:0]
	}

	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			word = append(word, r)
		} else {
			flush()
		}
	}
	flush()
	return out
}
