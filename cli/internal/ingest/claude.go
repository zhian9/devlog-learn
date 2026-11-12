package ingest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type claudeSource struct{}

type claudeRecord struct {
	Type          string          `json:"type"`
	Timestamp     string          `json:"timestamp"`
	SessionID     string          `json:"sessionId"`
	Cwd           string          `json:"cwd"`
	GitBranch     string          `json:"gitBranch"`
	Version       string          `json:"version"`
	IsSidechain   bool            `json:"isSidechain"`
	Message       json.RawMessage `json:"message"`
	CustomTitle   string          `json:"customTitle"`
	Title         string          `json:"title"`
	AITitle       string          `json:"aiTitle"`
	Attachment    json.RawMessage `json:"attachment"`
	ToolUseResult json.RawMessage `json:"toolUseResult"`
}

type claudeMessage struct {
	Role    string          `json:"role"`
	Model   string          `json:"model"`
	Content json.RawMessage `json:"content"`
}

type claudeBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	Thinking  string          `json:"thinking"`
	ID        string          `json:"id"`
	ToolUseID string          `json:"tool_use_id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	Content   json.RawMessage `json:"content"`
	IsError   bool            `json:"is_error"`
}

type claudeToolUseResult struct {
	Stdout      string `json:"stdout"`
	Stderr      string `json:"stderr"`
	Interrupted bool   `json:"interrupted"`
	TimedOut    bool   `json:"timedOut"`
	ExitCode    *int   `json:"exitCode"`
	Type        string `json:"type"`
	FilePath    string `json:"filePath"`
	Success     *bool  `json:"success"`
}

type claudeParser struct {
	sess             *Session
	opt              ParseOptions
	ref              FileRef
	toolUses         map[string]toolUseInfo
	titlePriority    int
	currentSidechain bool
	sessionMismatch  bool
}

func (claudeSource) Name() string { return "claude-code" }

func (claudeSource) DefaultRoot() (string, error) {
	return defaultHomeRoot(".claude", "projects")
}

func (claudeSource) Discover(root string, opt DiscoverOptions) ([]FileRef, error) {
	info, err := os.Stat(root)
	if err != nil {
		return nil, fmt.Errorf("access Claude root %q: %w", root, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("Claude root %q is not a directory", root)
	}

	var refs []FileRef
	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.EqualFold(filepath.Ext(path), ".jsonl") {
			return nil
		}
		fi, err := d.Info()
		if err != nil {
			return fmt.Errorf("stat %q: %w", path, err)
		}
		if !opt.Since.IsZero() && fi.ModTime().Before(opt.Since) {
			return nil
		}

		project := ""
		if filepath.Clean(filepath.Dir(path)) != filepath.Clean(root) {
			project = filepath.Base(filepath.Dir(path))
		}
		refs = append(refs, FileRef{
			Source:    "claude-code",
			Path:      path,
			Project:   project,
			SessionID: strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)),
			ModTime:   fi.ModTime(),
			Size:      fi.Size(),
		})
		return nil
	})
	if walkErr != nil {
		return nil, fmt.Errorf("discover Claude sessions under %q: %w", root, walkErr)
	}

	sort.Slice(refs, func(i, j int) bool { return refs[i].Path < refs[j].Path })
	return refs, nil
}

func (claudeSource) Parse(ref FileRef, opt ParseOptions) (*Session, error) {
	f, err := os.Open(ref.Path)
	if err != nil {
		return nil, fmt.Errorf("open Claude session %q: %w", ref.Path, err)
	}
	defer f.Close()

	sess := &Session{
		ID:      ref.SessionID,
		Source:  "claude-code",
		Project: ref.Project,
		Meta:    map[string]string{},
		Stats:   NewParseStats(),
	}
	p := &claudeParser{
		sess:     sess,
		opt:      opt,
		ref:      ref,
		toolUses: map[string]toolUseInfo{},
	}
	if err := readJSONLines(f, opt.MaxLineBytes, p.handleLine); err != nil {
		return nil, fmt.Errorf("read Claude session %q: %w", ref.Path, err)
	}
	p.finalize()
	return sess, nil
}

func (p *claudeParser) handleLine(jl JSONLine) error {
	s := p.sess
	s.Stats.Lines++

	if jl.TooLong {
		s.Stats.TooLongLines++
		s.warn(jl.No, "line is %d bytes and exceeds the configured limit; skipped", jl.Size)
		return nil
	}

	var rec claudeRecord
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

	p.applyMeta(&rec, jl.No)

	ts, tsErr := parseTimestamp(rec.Timestamp)
	if tsErr != nil {
		s.Stats.MalformedTimestamps++
		s.warn(jl.No, "invalid timestamp: %v", tsErr)
	}

	if rec.IsSidechain && !p.opt.IncludeSubagent {
		s.Stats.SkippedSidechain++
		return nil
	}
	p.currentSidechain = rec.IsSidechain

	// Metadata-only records (titles, attachments) still carry timestamps and
	// must contribute to the session time range.
	if !ts.IsZero() {
		if s.Started.IsZero() || ts.Before(s.Started) {
			s.Started = ts
		}
		if s.Ended.IsZero() || ts.After(s.Ended) {
			s.Ended = ts
		}
	}

	switch rec.Type {
	case "user":
		p.handleUser(&rec, ts, jl.No)
	case "assistant":
		p.handleAssistant(&rec, ts, jl.No)
	case "custom-title":
		title := rec.CustomTitle
		if title == "" {
			title = rec.Title
		}
		p.setTitle(title, 2)
	case "ai-title":
		p.setTitle(rec.AITitle, 1)
	case "attachment":
		p.handleAttachment(&rec)
	case "system", "summary", "queue-operation", "file-history-snapshot",
		"file-history-delta", "api-request", "api-request-shape", "api-request-blob",
		"atis-latch", "cost-state", "mode", "last-prompt", "tools-used":
		s.noteIgnored("type." + rec.Type)
	default:
		s.noteUnknown(jl.No, "type."+rec.Type)
		p.emit(jl.No, Event{
			Kind: KindUnknown,
			Role: "system",
			Time: ts,
			Raw:  append(json.RawMessage(nil), jl.Data...),
		})
	}
	return nil
}

func (p *claudeParser) applyMeta(rec *claudeRecord, line int) {
	s := p.sess
	if rec.SessionID != "" {
		if s.ID == "" || s.ID == p.ref.SessionID {
			s.ID = rec.SessionID
		} else if s.ID != rec.SessionID && !p.sessionMismatch {
			p.sessionMismatch = true
			s.warn(line, "record sessionId %q differs from %q; keeping the first session id", rec.SessionID, s.ID)
		}
	}
	if rec.Cwd != "" {
		s.Cwd = rec.Cwd
		if project := projectFromCwd(rec.Cwd); project != "" {
			s.Project = project
		}
	}
	if rec.Version != "" {
		s.Meta["cli_version"] = rec.Version
	}
	if rec.GitBranch != "" {
		s.Meta["git_branch"] = rec.GitBranch
	}
	if rec.SessionID != "" {
		s.Meta["session_id"] = rec.SessionID
	}
}

func (p *claudeParser) setTitle(title string, priority int) {
	title = strings.TrimSpace(title)
	if title == "" || priority < p.titlePriority {
		return
	}
	p.sess.Title = title
	p.titlePriority = priority
}

func (p *claudeParser) handleAttachment(rec *claudeRecord) {
	var att struct {
		Type string `json:"type"`
	}
	if len(rec.Attachment) > 0 {
		_ = json.Unmarshal(rec.Attachment, &att)
	}
	key := att.Type
	if key == "" {
		key = "unknown"
	}
	p.sess.noteIgnored("attachment." + key)
}

func (p *claudeParser) handleUser(rec *claudeRecord, ts time.Time, line int) {
	s := p.sess
	if len(rec.Message) == 0 {
		s.warn(line, "user record has no message; skipped")
		return
	}
	var msg claudeMessage
	if err := json.Unmarshal(rec.Message, &msg); err != nil {
		s.Stats.UnsupportedBlocks++
		s.warn(line, "cannot decode user message: %v", err)
		return
	}

	role := msg.Role
	if role == "" {
		role = "user"
	}

	text, blocks, err := decodeContent(msg.Content)
	if err != nil {
		s.Stats.UnsupportedBlocks++
		s.warn(line, "cannot decode user content: %v", err)
		return
	}
	if strings.TrimSpace(text) != "" {
		p.emit(line, Event{Kind: KindUserText, Role: role, Time: ts, Text: text})
	}

	tur := p.decodeToolUseResult(rec, line)
	toolResultCount := 0
	for _, raw := range blocks {
		var block claudeBlock
		if err := json.Unmarshal(raw, &block); err == nil && block.Type == "tool_result" {
			toolResultCount++
		}
	}

	for _, raw := range blocks {
		var block claudeBlock
		if err := json.Unmarshal(raw, &block); err != nil {
			s.Stats.UnsupportedBlocks++
			s.warn(line, "cannot decode user content block: %v", err)
			p.emit(line, Event{Kind: KindUnknown, Role: role, Time: ts, Raw: raw})
			continue
		}

		switch block.Type {
		case "text":
			if block.Text != "" {
				p.emit(line, Event{Kind: KindUserText, Role: role, Time: ts, Text: block.Text})
			}
		case "tool_result":
			p.handleToolResult(block, tur, toolResultCount, role, ts, line)
		default:
			s.Stats.UnsupportedBlocks++
			s.warn(line, "unsupported user content block %q; preserved as unknown", block.Type)
			p.emit(line, Event{Kind: KindUnknown, Role: role, Time: ts, Raw: raw})
		}
	}
}

func (p *claudeParser) handleToolResult(block claudeBlock, tur *claudeToolUseResult, count int, role string, ts time.Time, line int) {
	s := p.sess
	resultText, err := contentText(block.Content)
	if err != nil {
		s.Stats.UnsupportedBlocks++
		s.warn(line, "cannot decode tool_result content: %v", err)
	}
	if strings.TrimSpace(resultText) == "" && tur != nil {
		resultText = joinNonEmpty(tur.Stdout, tur.Stderr)
	}

	ev := Event{
		Kind:       KindToolResult,
		Role:       role,
		Time:       ts,
		ToolUseID:  block.ToolUseID,
		ToolResult: resultText,
		IsError:    block.IsError,
	}
	if info, ok := p.toolUses[block.ToolUseID]; ok {
		ev.ToolName = info.Name
		ev.ToolInput = info.Input
		ev.ToolInputText = info.Text
	}
	if count == 1 {
		ev.Exec = execFromClaudeResult(tur, resultText)
	} else if info := execFromText(resultText); info != nil {
		ev.Exec = info
	}
	p.emit(line, ev)
}

func (p *claudeParser) decodeToolUseResult(rec *claudeRecord, line int) *claudeToolUseResult {
	trimmed := bytes.TrimSpace(rec.ToolUseResult)
	// Some tools record toolUseResult as a JSON string or array. The actual
	// result is already carried by message.content, so only object-form
	// metadata is decoded here; non-object values are not an error.
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return nil
	}
	var tur claudeToolUseResult
	if err := json.Unmarshal(rec.ToolUseResult, &tur); err != nil {
		p.sess.warn(line, "cannot decode toolUseResult: %v", err)
		return nil
	}
	return &tur
}

func execFromClaudeResult(tur *claudeToolUseResult, text string) *ExecResult {
	ex := execFromText(text)
	if tur == nil {
		return ex
	}
	if ex == nil {
		ex = &ExecResult{}
	}
	ex.Stdout = joinNonEmpty(ex.Stdout, tur.Stdout)
	ex.Stderr = joinNonEmpty(ex.Stderr, tur.Stderr)
	if tur.ExitCode != nil {
		code := *tur.ExitCode
		ex.ExitCode = &code
		ex.Known = true
	}
	if tur.TimedOut {
		ex.TimedOut = true
		ex.Known = true
	}
	if tur.Interrupted {
		ex.Canceled = true
		ex.Known = true
	}
	if !ex.Known && ex.ExitCode == nil && !ex.TimedOut && !ex.Canceled && ex.Stdout == "" && ex.Stderr == "" {
		return nil
	}
	return ex
}

func (p *claudeParser) handleAssistant(rec *claudeRecord, ts time.Time, line int) {
	s := p.sess
	if len(rec.Message) == 0 {
		s.warn(line, "assistant record has no message; skipped")
		return
	}
	var msg claudeMessage
	if err := json.Unmarshal(rec.Message, &msg); err != nil {
		s.Stats.UnsupportedBlocks++
		s.warn(line, "cannot decode assistant message: %v", err)
		return
	}
	role := msg.Role
	if role == "" {
		role = "assistant"
	}
	if msg.Model != "" {
		s.Meta["model"] = msg.Model
	}

	text, blocks, err := decodeContent(msg.Content)
	if err != nil {
		s.Stats.UnsupportedBlocks++
		s.warn(line, "cannot decode assistant content: %v", err)
		return
	}
	if strings.TrimSpace(text) != "" {
		p.emit(line, Event{Kind: KindAssistantText, Role: role, Time: ts, Text: text})
	}

	for _, raw := range blocks {
		var block claudeBlock
		if err := json.Unmarshal(raw, &block); err != nil {
			s.Stats.UnsupportedBlocks++
			s.warn(line, "cannot decode assistant content block: %v", err)
			p.emit(line, Event{Kind: KindUnknown, Role: role, Time: ts, Raw: raw})
			continue
		}

		switch block.Type {
		case "thinking":
			if block.Thinking != "" {
				p.emit(line, Event{Kind: KindAssistantThink, Role: role, Time: ts, Text: block.Thinking})
			}
		case "text":
			if block.Text != "" {
				p.emit(line, Event{Kind: KindAssistantText, Role: role, Time: ts, Text: block.Text})
			}
		case "tool_use":
			info := toolUseInfo{Name: block.Name, Input: block.Input}
			p.toolUses[block.ID] = info
			p.emit(line, Event{
				Kind:      KindToolUse,
				Role:      role,
				Time:      ts,
				ToolName:  block.Name,
				ToolInput: block.Input,
				ToolUseID: block.ID,
			})
		case "redacted_thinking":
			s.noteIgnored("assistant_block.redacted_thinking")
		default:
			s.Stats.UnsupportedBlocks++
			s.warn(line, "unsupported assistant content block %q; preserved as unknown", block.Type)
			p.emit(line, Event{Kind: KindUnknown, Role: role, Time: ts, Raw: raw})
		}
	}
}

func (p *claudeParser) emit(line int, ev Event) {
	ev.File = p.ref.Path
	ev.Line = line
	ev.Sidechain = p.currentSidechain
	p.sess.appendEvent(ev)
}

func (p *claudeParser) finalize() {
	if p.sess.Title != "" {
		p.sess.Meta["title"] = p.sess.Title
	}
	p.sess.finalize(p.ref, p.sess.Events)
}
