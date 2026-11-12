package ingest

import (
	"encoding/json"
	"fmt"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Kind is the provider-independent classification of one event.
type Kind string

const (
	KindUserText       Kind = "user_text"
	KindToolResult     Kind = "tool_result"
	KindAssistantText  Kind = "assistant_text"
	KindAssistantThink Kind = "assistant_thinking"
	KindToolUse        Kind = "tool_use"
	// KindMeta carries session/system material that is preserved for
	// traceability but is not part of the user/assistant conversation.
	KindMeta Kind = "meta"
	// KindUnknown preserves a record or content block we could not interpret.
	// Raw contains the original JSON so data is not silently lost.
	KindUnknown Kind = "unknown"
)

// ExecResult captures only what the log actually tells us about a tool
// execution. It deliberately does not use IsError=false as proof of success:
//
//	Known && ExitCode == 0  -> command completed successfully
//	Known && ExitCode != 0  -> command failed
//	Known && TimedOut       -> execution timed out
//	Known && Canceled       -> execution was interrupted/canceled
//	!Known                  -> outcome cannot be proven from the log
//
// The verification layer (phase 3) maps these values to
// verified/failed/unknown/not_run.
type ExecResult struct {
	Known    bool   `json:"known"`
	ExitCode *int   `json:"exit_code,omitempty"`
	TimedOut bool   `json:"timed_out,omitempty"`
	Canceled bool   `json:"canceled,omitempty"`
	Stdout   string `json:"stdout,omitempty"`
	Stderr   string `json:"stderr,omitempty"`
}

// Event is the unified representation produced by both parsers.
type Event struct {
	Kind Kind      `json:"kind"`
	Time time.Time `json:"time,omitempty"`
	Role string    `json:"role,omitempty"`
	Text string    `json:"text,omitempty"`

	ToolName      string          `json:"tool_name,omitempty"`
	ToolInput     json.RawMessage `json:"tool_input,omitempty"`
	ToolInputText string          `json:"tool_input_text,omitempty"`
	ToolResult    string          `json:"tool_result,omitempty"`
	ToolUseID     string          `json:"tool_use_id,omitempty"`

	// IsError is only set when the source record explicitly marks an error.
	// It is not a success signal.
	IsError bool `json:"is_error,omitempty"`
	// Synthetic marks injected context (environment/permissions/etc.) that
	// is preserved but should not be treated as a user request.
	Synthetic bool        `json:"synthetic,omitempty"`
	Truncated bool        `json:"truncated,omitempty"`
	Exec      *ExecResult `json:"exec,omitempty"`

	// Raw preserves an uninterpreted record/block.
	Raw json.RawMessage `json:"raw,omitempty"`

	Source    string `json:"source,omitempty"`
	SessionID string `json:"session_id,omitempty"`
	Project   string `json:"project,omitempty"`
	Cwd       string `json:"cwd,omitempty"`
	File      string `json:"file,omitempty"`
	Line      int    `json:"line,omitempty"`
	Seq       int    `json:"seq"`
	Sidechain bool   `json:"sidechain,omitempty"`
}

// Warning is a diagnosable parse problem. It never contains raw message text.
type Warning struct {
	File    string `json:"file"`
	Line    int    `json:"line"`
	Message string `json:"message"`
}

// ParseStats makes every skip visible. UnknownTypes and IgnoredTypes are
// intentionally separate: ignored means "recognised but not conversation",
// unknown means "we do not know what this is".
type ParseStats struct {
	Lines               int            `json:"lines"`
	Records             int            `json:"records"`
	Events              int            `json:"events"`
	BadJSON             int            `json:"bad_json"`
	TooLongLines        int            `json:"too_long_lines"`
	SkippedSidechain    int            `json:"skipped_sidechain"`
	UnsupportedBlocks   int            `json:"unsupported_blocks"`
	MalformedTimestamps int            `json:"malformed_timestamps"`
	WarningsDropped     int            `json:"warnings_dropped"`
	Duplicates          int            `json:"duplicates"`
	UnknownTypes        map[string]int `json:"unknown_types,omitempty"`
	IgnoredTypes        map[string]int `json:"ignored_types,omitempty"`
}

func NewParseStats() ParseStats {
	return ParseStats{
		UnknownTypes: map[string]int{},
		IgnoredTypes: map[string]int{},
	}
}

// Session is the parse result for one source file.
type Session struct {
	ID       string            `json:"id"`
	Source   string            `json:"source"`
	Project  string            `json:"project"`
	File     string            `json:"file,omitempty"`
	Cwd      string            `json:"cwd,omitempty"`
	Title    string            `json:"title,omitempty"`
	Started  time.Time         `json:"started,omitempty"`
	Ended    time.Time         `json:"ended,omitempty"`
	Events   []Event           `json:"events"`
	Warnings []Warning         `json:"warnings,omitempty"`
	Stats    ParseStats        `json:"stats"`
	Meta     map[string]string `json:"meta,omitempty"`

	Excluded      bool   `json:"excluded,omitempty"`
	ExcludeReason string `json:"exclude_reason,omitempty"`

	seenUnknown map[string]bool
}

const maxWarnings = 100

func (s *Session) warn(line int, format string, args ...any) {
	if len(s.Warnings) >= maxWarnings {
		s.Stats.WarningsDropped++
		return
	}
	s.Warnings = append(s.Warnings, Warning{
		File:    "",
		Line:    line,
		Message: fmt.Sprintf(format, args...),
	})
}

func (s *Session) noteIgnored(key string) {
	if s.Stats.IgnoredTypes == nil {
		s.Stats.IgnoredTypes = map[string]int{}
	}
	s.Stats.IgnoredTypes[key]++
}

func (s *Session) noteUnknown(line int, key string) {
	if s.Stats.UnknownTypes == nil {
		s.Stats.UnknownTypes = map[string]int{}
	}
	s.Stats.UnknownTypes[key]++
	if s.seenUnknown == nil {
		s.seenUnknown = map[string]bool{}
	}
	if !s.seenUnknown[key] {
		s.seenUnknown[key] = true
		s.warn(line, "unknown record/event type %q; counted and preserved as a diagnostic", key)
	}
}

func (s *Session) appendEvent(e Event) {
	e.Seq = len(s.Events) + 1
	if e.Source == "" {
		e.Source = s.Source
	}
	if e.SessionID == "" {
		e.SessionID = s.ID
	}
	if e.Project == "" {
		e.Project = s.Project
	}
	if e.Cwd == "" {
		e.Cwd = s.Cwd
	}
	if !e.Time.IsZero() {
		if s.Started.IsZero() || e.Time.Before(s.Started) {
			s.Started = e.Time
		}
		if s.Ended.IsZero() || e.Time.After(s.Ended) {
			s.Ended = e.Time
		}
	}
	s.Events = append(s.Events, e)
}

// finalize applies authoritative session metadata to every event and assigns
// contiguous sequence numbers. events may be nil for excluded sessions.
func (s *Session) finalize(ref FileRef, events []Event) {
	if s.Source == "" {
		s.Source = ref.Source
	}
	s.File = ref.Path
	if s.ID == "" {
		s.ID = ref.SessionID
	}
	if s.Project == "" {
		s.Project = ref.Project
	}
	if s.Project == "" {
		s.Project = "unknown-project"
	}
	if s.Meta == nil {
		s.Meta = map[string]string{}
	}

	for i := range s.Warnings {
		if s.Warnings[i].File == "" {
			s.Warnings[i].File = ref.Path
		}
	}

	s.Events = events
	for i := range s.Events {
		e := &s.Events[i]
		e.Seq = i + 1
		if e.Source == "" {
			e.Source = s.Source
		}
		if s.ID != "" {
			e.SessionID = s.ID
		}
		if s.Project != "" {
			e.Project = s.Project
		}
		if s.Cwd != "" {
			e.Cwd = s.Cwd
		}
		if e.File == "" {
			e.File = ref.Path
		}
		if !e.Time.IsZero() {
			if s.Started.IsZero() || e.Time.Before(s.Started) {
				s.Started = e.Time
			}
			if s.Ended.IsZero() || e.Time.After(s.Ended) {
				s.Ended = e.Time
			}
		}
	}
	s.Stats.Events = len(s.Events)
}

type toolUseInfo struct {
	Name  string
	Input json.RawMessage
	Text  string
}

// projectFromCwd returns the last path component on either Windows or POSIX.
func projectFromCwd(cwd string) string {
	cwd = strings.TrimSpace(cwd)
	if cwd == "" {
		return ""
	}
	cwd = strings.TrimRight(strings.ReplaceAll(cwd, "\\", "/"), "/")
	if cwd == "" {
		return ""
	}
	// Windows drive roots ("D:") have no useful project component.
	if len(cwd) == 2 && cwd[1] == ':' {
		return ""
	}
	base := path.Base(cwd)
	if base == "." || base == "/" || base == "" {
		return ""
	}
	return base
}

func parseTimestamp(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, nil
	}
	if t, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return t, nil
	}
	if t, err := time.Parse(time.RFC3339, value); err == nil {
		return t, nil
	}
	return time.Time{}, fmt.Errorf("invalid timestamp %q", value)
}

var (
	exitCodePattern = regexp.MustCompile(`(?im)(?:Process exited with code|Exit code:)\s*(-?\d+)`)
	timeoutPattern  = regexp.MustCompile(`(?im)(?:command|process|request)\s+timed out|timed out after\s+\d+`)
)

// execFromText extracts only explicit execution outcome signals from tool
// output text. Absence of a signal returns nil, never a success result.
func execFromText(text string) *ExecResult {
	if text == "" {
		return nil
	}
	if m := exitCodePattern.FindStringSubmatch(text); len(m) == 2 {
		if code, err := strconv.Atoi(m[1]); err == nil {
			return &ExecResult{Known: true, ExitCode: &code}
		}
	}
	if timeoutPattern.MatchString(text) {
		return &ExecResult{Known: true, TimedOut: true}
	}
	return nil
}

func mergeExec(dst, src *ExecResult) *ExecResult {
	if dst == nil {
		dst = &ExecResult{}
	}
	if src == nil {
		return dst
	}
	if src.Known {
		dst.Known = true
	}
	if src.ExitCode != nil {
		code := *src.ExitCode
		dst.ExitCode = &code
	}
	if src.TimedOut {
		dst.TimedOut = true
	}
	if src.Canceled {
		dst.Canceled = true
	}
	if src.Stdout != "" {
		dst.Stdout = src.Stdout
	}
	if src.Stderr != "" {
		dst.Stderr = src.Stderr
	}
	return dst
}

// isSyntheticUserText recognises context injected by the agent runtime rather
// than a request typed by the user.
func isSyntheticUserText(text string) bool {
	trimmed := strings.TrimSpace(text)
	prefixes := []string{
		"<environment_context>",
		"<user_instructions>",
		"<permissions instructions>",
		"<app-context>",
	}
	for _, prefix := range prefixes {
		if strings.HasPrefix(trimmed, prefix) {
			return true
		}
	}
	return false
}
