package ingest

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type codexSource struct{}

type codexRecord struct {
	Timestamp string          `json:"timestamp"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
}

type codexPayload struct {
	Type         string          `json:"type"`
	Role         string          `json:"role"`
	Content      json.RawMessage `json:"content"`
	Summary      json.RawMessage `json:"summary"`
	Name         string          `json:"name"`
	Arguments    string          `json:"arguments"`
	Input        string          `json:"input"`
	CallID       string          `json:"call_id"`
	Output       json.RawMessage `json:"output"`
	Status       string          `json:"status"`
	Cwd          string          `json:"cwd"`
	SessionID    string          `json:"session_id"`
	ID           string          `json:"id"`
	Originator   string          `json:"originator"`
	CLIVersion   string          `json:"cli_version"`
	ThreadSource string          `json:"thread_source"`
	Source       json.RawMessage `json:"source"`
	Message      string          `json:"message"`
	Reason       string          `json:"reason"`
}

type codexParser struct {
	sess             *Session
	opt              ParseOptions
	ref              FileRef
	toolUses         map[string]toolUseInfo
	resp             []Event
	fallback         []Event
	respConversation int
	metaSeen         bool
}

func (codexSource) Name() string { return "codex" }

func (codexSource) DefaultRoot() (string, error) {
	return defaultHomeRoot(".codex", "sessions")
}

func (codexSource) Discover(root string, opt DiscoverOptions) ([]FileRef, error) {
	info, err := os.Stat(root)
	if err != nil {
		return nil, fmt.Errorf("access Codex root %q: %w", root, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("Codex root %q is not a directory", root)
	}

	var refs []FileRef
	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		base := filepath.Base(path)
		if !strings.EqualFold(filepath.Ext(path), ".jsonl") || !strings.HasPrefix(base, "rollout-") {
			return nil
		}
		fi, err := d.Info()
		if err != nil {
			return fmt.Errorf("stat %q: %w", path, err)
		}
		if !opt.Since.IsZero() && fi.ModTime().Before(opt.Since) {
			return nil
		}
		refs = append(refs, FileRef{
			Source:    "codex",
			Path:      path,
			SessionID: strings.TrimSuffix(base, filepath.Ext(base)),
			ModTime:   fi.ModTime(),
			Size:      fi.Size(),
		})
		return nil
	})
	if walkErr != nil {
		return nil, fmt.Errorf("discover Codex sessions under %q: %w", root, walkErr)
	}

	sort.Slice(refs, func(i, j int) bool { return refs[i].Path < refs[j].Path })
	return refs, nil
}

func (codexSource) Parse(ref FileRef, opt ParseOptions) (*Session, error) {
	f, err := os.Open(ref.Path)
	if err != nil {
		return nil, fmt.Errorf("open Codex session %q: %w", ref.Path, err)
	}
	defer f.Close()

	sess := &Session{
		ID:      ref.SessionID,
		Source:  "codex",
		Project: ref.Project,
		Meta:    map[string]string{},
		Stats:   NewParseStats(),
	}
	p := &codexParser{
		sess:     sess,
		opt:      opt,
		ref:      ref,
		toolUses: map[string]toolUseInfo{},
	}
	if err := readJSONLines(f, opt.MaxLineBytes, p.handleLine); err != nil {
		return nil, fmt.Errorf("read Codex session %q: %w", ref.Path, err)
	}
	p.finalize()
	return sess, nil
}

func (p *codexParser) handleLine(jl JSONLine) error {
	s := p.sess
	s.Stats.Lines++

	if jl.TooLong {
		s.Stats.TooLongLines++
		s.warn(jl.No, "line is %d bytes and exceeds the configured limit; skipped", jl.Size)
		return nil
	}

	var rec codexRecord
	if err := json.Unmarshal(jl.Data, &rec); err != nil {
		s.Stats.BadJSON++
		s.warn(jl.No, "invalid JSON: %v", err)
		return nil
	}
	s.Stats.Records++

	if rec.Type == "" {
		s.Stats.BadJSON++
		s.warn(jl.No, "record has no type field; skipped")
		return nil
	}

	ts, tsErr := parseTimestamp(rec.Timestamp)
	if tsErr != nil {
		s.Stats.MalformedTimestamps++
		s.warn(jl.No, "invalid timestamp: %v", tsErr)
	}

	switch rec.Type {
	case "session_meta":
		p.handleSessionMeta(rec.Payload, ts, jl.No)
	case "turn_context":
		p.handleTurnContext(rec.Payload)
	case "response_item":
		p.handleResponseItem(rec.Payload, ts, jl.No)
	case "event_msg":
		p.handleEventMsg(rec.Payload, ts, jl.No)
	case "token_usage_record", "world_state":
		s.noteIgnored("type." + rec.Type)
	default:
		s.noteUnknown(jl.No, "type."+rec.Type)
	}
	return nil
}

func (p *codexParser) handleSessionMeta(raw json.RawMessage, ts time.Time, line int) {
	if p.metaSeen {
		return
	}
	p.metaSeen = true

	s := p.sess
	var pl codexPayload
	if err := json.Unmarshal(raw, &pl); err != nil {
		s.Stats.UnsupportedBlocks++
		s.warn(line, "cannot decode session_meta payload: %v", err)
		return
	}

	if pl.Cwd != "" {
		s.Cwd = pl.Cwd
		if project := projectFromCwd(pl.Cwd); project != "" {
			s.Project = project
		}
	}
	switch {
	case pl.SessionID != "":
		s.ID = pl.SessionID
	case pl.ID != "":
		s.ID = pl.ID
	}
	if pl.Originator != "" {
		s.Meta["originator"] = pl.Originator
	}
	if pl.CLIVersion != "" {
		s.Meta["cli_version"] = pl.CLIVersion
	}
	if pl.ThreadSource != "" {
		s.Meta["thread_source"] = pl.ThreadSource
	}
	if isSubagentPayload(&pl) && !p.opt.IncludeSubagent {
		s.Excluded = true
		s.ExcludeReason = "subagent session (thread_source/source)"
	}
	if !ts.IsZero() {
		s.Started = ts
	}
}

func (p *codexParser) handleTurnContext(raw json.RawMessage) {
	var pl codexPayload
	if err := json.Unmarshal(raw, &pl); err != nil {
		p.sess.Stats.UnsupportedBlocks++
		p.sess.warn(0, "cannot decode turn_context payload: %v", err)
		return
	}
	if pl.Cwd != "" {
		if p.sess.Cwd == "" {
			p.sess.Cwd = pl.Cwd
		}
		if p.sess.Project == "" || p.sess.Project == "unknown-project" {
			if project := projectFromCwd(pl.Cwd); project != "" {
				p.sess.Project = project
			}
		}
	}
}

func (p *codexParser) handleResponseItem(raw json.RawMessage, ts time.Time, line int) {
	s := p.sess
	var pl codexPayload
	if err := json.Unmarshal(raw, &pl); err != nil {
		s.Stats.UnsupportedBlocks++
		s.warn(line, "cannot decode response_item payload: %v", err)
		p.emitResp(Event{Kind: KindUnknown, Role: "system", Time: ts, Raw: raw})
		return
	}

	switch pl.Type {
	case "message":
		p.handleMessage(&pl, ts, line)
	case "reasoning":
		text, err := contentText(pl.Summary)
		if err != nil {
			s.Stats.UnsupportedBlocks++
			s.warn(line, "cannot decode reasoning summary: %v", err)
		}
		if strings.TrimSpace(text) == "" {
			text, err = contentText(pl.Content)
			if err != nil {
				s.Stats.UnsupportedBlocks++
				s.warn(line, "cannot decode reasoning content: %v", err)
			}
		}
		if strings.TrimSpace(text) != "" {
			p.emitResp(Event{Kind: KindAssistantThink, Role: "assistant", Time: ts, Text: text})
			p.respConversation++
		}
	case "function_call":
		p.handleFunctionCall(&pl, ts, line)
	case "custom_tool_call":
		p.handleCustomToolCall(&pl, ts)
	case "function_call_output", "custom_tool_call_output":
		p.handleToolOutput(&pl, ts, line)
	default:
		s.noteUnknown(line, "response_item."+pl.Type)
		p.emitResp(Event{Kind: KindUnknown, Role: "system", Time: ts, Raw: raw})
	}
}

func (p *codexParser) handleMessage(pl *codexPayload, ts time.Time, line int) {
	s := p.sess
	text, err := contentText(pl.Content)
	if err != nil {
		s.Stats.UnsupportedBlocks++
		s.warn(line, "cannot decode message content: %v", err)
	}
	if strings.TrimSpace(text) == "" {
		return
	}

	kind := KindMeta
	synthetic := true
	switch pl.Role {
	case "user":
		if isSyntheticUserText(text) {
			kind = KindMeta
		} else {
			kind = KindUserText
			synthetic = false
		}
	case "assistant":
		kind = KindAssistantText
		synthetic = false
	case "developer", "system":
		kind = KindMeta
	default:
		s.warn(line, "unknown message role %q; preserved as meta", pl.Role)
	}

	p.emitResp(Event{
		Kind:      kind,
		Role:      pl.Role,
		Time:      ts,
		Text:      text,
		Synthetic: synthetic,
	})
	p.respConversation++
}

func (p *codexParser) handleFunctionCall(pl *codexPayload, ts time.Time, line int) {
	info := toolUseInfo{Name: pl.Name}
	if pl.Arguments != "" {
		if json.Valid([]byte(pl.Arguments)) {
			info.Input = json.RawMessage(pl.Arguments)
		} else {
			info.Text = pl.Arguments
			p.sess.warn(line, "function_call %q has non-JSON arguments; preserved as text", pl.Name)
		}
	}
	p.toolUses[pl.CallID] = info
	p.emitResp(Event{
		Kind:          KindToolUse,
		Role:          "assistant",
		Time:          ts,
		ToolName:      info.Name,
		ToolInput:     info.Input,
		ToolInputText: info.Text,
		ToolUseID:     pl.CallID,
	})
	p.respConversation++
}

func (p *codexParser) handleCustomToolCall(pl *codexPayload, ts time.Time) {
	info := toolUseInfo{Name: pl.Name, Text: pl.Input}
	p.toolUses[pl.CallID] = info
	p.emitResp(Event{
		Kind:          KindToolUse,
		Role:          "assistant",
		Time:          ts,
		ToolName:      info.Name,
		ToolInputText: info.Text,
		ToolUseID:     pl.CallID,
	})
	p.respConversation++
}

func (p *codexParser) handleToolOutput(pl *codexPayload, ts time.Time, line int) {
	s := p.sess
	text, err := contentText(pl.Output)
	if err != nil {
		s.Stats.UnsupportedBlocks++
		s.warn(line, "cannot decode tool output: %v", err)
		text = string(pl.Output)
	}

	info, ok := p.toolUses[pl.CallID]
	if !ok {
		s.warn(line, "tool output for call_id %q has no matching tool call", pl.CallID)
	}

	ev := Event{
		Kind:          KindToolResult,
		Role:          "tool",
		Time:          ts,
		ToolUseID:     pl.CallID,
		ToolResult:    text,
		ToolName:      info.Name,
		ToolInput:     info.Input,
		ToolInputText: info.Text,
		Exec:          execFromCodexOutput(pl.Output, text),
	}
	p.emitResp(ev)
	p.respConversation++
}

func (p *codexParser) handleEventMsg(raw json.RawMessage, ts time.Time, line int) {
	s := p.sess
	var pl codexPayload
	if err := json.Unmarshal(raw, &pl); err != nil {
		s.Stats.UnsupportedBlocks++
		s.warn(line, "cannot decode event_msg payload: %v", err)
		p.emitFallback(Event{Kind: KindUnknown, Role: "system", Time: ts, Raw: raw})
		return
	}

	switch pl.Type {
	case "user_message":
		if strings.TrimSpace(pl.Message) == "" {
			return
		}
		kind := KindUserText
		synthetic := false
		if isSyntheticUserText(pl.Message) {
			kind = KindMeta
			synthetic = true
		}
		p.emitFallback(Event{Kind: kind, Role: "user", Time: ts, Text: pl.Message, Synthetic: synthetic})
	case "agent_message":
		if strings.TrimSpace(pl.Message) == "" {
			return
		}
		p.emitFallback(Event{Kind: KindAssistantText, Role: "assistant", Time: ts, Text: pl.Message})
	case "agent_reasoning":
		if strings.TrimSpace(pl.Message) == "" {
			return
		}
		p.emitFallback(Event{Kind: KindAssistantThink, Role: "assistant", Time: ts, Text: pl.Message})
	case "turn_aborted":
		text := "turn_aborted"
		if pl.Reason != "" {
			text += ": " + pl.Reason
		}
		p.emitFallback(Event{
			Kind:      KindMeta,
			Role:      "system",
			Time:      ts,
			Text:      text,
			Synthetic: true,
			Raw:       raw,
		})
	case "token_count", "task_started", "task_complete", "thread_settings_applied", "item_completed":
		s.noteIgnored("event_msg." + pl.Type)
	default:
		s.noteUnknown(line, "event_msg."+pl.Type)
		p.emitFallback(Event{Kind: KindUnknown, Role: "system", Time: ts, Raw: raw})
	}
}

func (p *codexParser) emitResp(ev Event) {
	p.resp = append(p.resp, ev)
}

func (p *codexParser) emitFallback(ev Event) {
	p.fallback = append(p.fallback, ev)
}

func (p *codexParser) finalize() {
	s := p.sess

	chosen := p.resp
	if p.respConversation == 0 {
		chosen = p.fallback
	} else if len(p.fallback) > 0 {
		// event_msg duplicates response_item in normal Codex logs. Preserve
		// only entries that are genuinely absent from the primary stream.
		seen := map[string]bool{}
		for _, ev := range chosen {
			seen[eventKey(ev)] = true
		}
		for _, ev := range p.fallback {
			key := eventKey(ev)
			if seen[key] {
				s.Stats.Duplicates++
				continue
			}
			seen[key] = true
			chosen = append(chosen, ev)
		}
		sort.SliceStable(chosen, func(i, j int) bool {
			ti, tj := chosen[i].Time, chosen[j].Time
			if ti.Equal(tj) {
				return false
			}
			if ti.IsZero() {
				return false
			}
			if tj.IsZero() {
				return true
			}
			return ti.Before(tj)
		})
	}

	if s.Excluded {
		chosen = nil
	}
	if s.Cwd == "" && s.Stats.Records > 0 {
		s.warn(0, "session metadata has no cwd; project falls back to %q", s.Project)
	}
	s.finalize(p.ref, chosen)
}

func eventKey(ev Event) string {
	if len(ev.Raw) > 0 {
		return string(ev.Kind) + "\x00" + string(ev.Raw)
	}
	return string(ev.Kind) + "\x00" + ev.Text
}

func execFromCodexOutput(raw json.RawMessage, text string) *ExecResult {
	ex := execFromText(text)

	var obj struct {
		ExitCode  *int `json:"exit_code"`
		ExitCode2 *int `json:"exitCode"`
		TimedOut  bool `json:"timed_out"`
		Canceled  bool `json:"canceled"`
	}
	if len(raw) > 0 && json.Unmarshal(raw, &obj) == nil {
		if ex == nil {
			ex = &ExecResult{}
		}
		switch {
		case obj.ExitCode != nil:
			ex.ExitCode = obj.ExitCode
			ex.Known = true
		case obj.ExitCode2 != nil:
			ex.ExitCode = obj.ExitCode2
			ex.Known = true
		}
		if obj.TimedOut {
			ex.TimedOut = true
			ex.Known = true
		}
		if obj.Canceled {
			ex.Canceled = true
			ex.Known = true
		}
	}
	return ex
}

func isSubagentPayload(pl *codexPayload) bool {
	thread := strings.ToLower(pl.ThreadSource)
	if strings.Contains(thread, "guardian") || strings.Contains(thread, "subagent") {
		return true
	}
	if len(pl.Source) > 0 {
		var src map[string]json.RawMessage
		if err := json.Unmarshal(pl.Source, &src); err == nil {
			if _, ok := src["subagent"]; ok {
				return true
			}
		}
	}
	return false
}
