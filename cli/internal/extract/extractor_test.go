package extract

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"devlog-learn/internal/ingest"
	"devlog-learn/internal/redact"
	"devlog-learn/internal/segment"
)

var extractNow = time.Date(2026, 5, 2, 8, 0, 0, 0, time.UTC)

type fakeClient struct {
	name      string
	version   string
	model     string
	responses []string
	usages    []Usage
	errs      []error
	calls     []Request
	callCount int
}

func (f *fakeClient) Name() string { return f.name }

func (f *fakeClient) Version(context.Context) (string, error) { return f.version, nil }

func (f *fakeClient) Capabilities(context.Context) (map[string]bool, error) {
	return map[string]bool{"--output-format": true}, nil
}

func (f *fakeClient) Model() string { return f.model }

func (f *fakeClient) Complete(_ context.Context, req Request) (Response, error) {
	index := f.callCount
	f.callCount++
	f.calls = append(f.calls, req)
	if index < len(f.errs) && f.errs[index] != nil {
		return Response{Stderr: "client failure"}, f.errs[index]
	}
	usage := Usage{}
	if index < len(f.usages) {
		usage = f.usages[index]
	}
	if index < len(f.responses) {
		return Response{Text: f.responses[index], Usage: usage}, nil
	}
	return Response{}, errors.New("no fake response configured")
}

func extractorSegment() segment.Segment {
	return segment.Segment{
		ID: "seg-extract", Hash: "seg-hash", Source: "claude-code", SessionID: "sess-1",
		Project: "demo", Title: "Fix build", Start: docTime, End: docTime.Add(2 * time.Minute),
		Events: []ingest.Event{
			{Kind: ingest.KindUserText, Role: "user", Text: "Build fails with error X", Seq: 1, File: "session.jsonl"},
			{Kind: ingest.KindToolResult, Role: "tool", ToolResult: "ok", Seq: 2, File: "session.jsonl",
				Exec: &ingest.ExecResult{Known: true, ExitCode: intPtr(0)}},
			{Kind: ingest.KindAssistantText, Role: "assistant", Text: "Fixed", Seq: 3, File: "session.jsonl"},
		},
	}
}

func validExtractionJSON() string {
	return `{
	  "title": "Fix build",
	  "summary": "The build failure was fixed.",
	  "knowledge_type": "bug_fix",
	  "status": "resolved",
	  "problem": {"statement": "build failed", "evidence": [1]},
	  "root_cause": {"text": "bad import", "known": true, "confidence": "high", "evidence": [1]},
	  "diagnosis": [
	    {"order": 1, "observation": "Build fails", "hypothesis": "bad import", "action": "run build",
	     "command": "go build ./...", "command_kind": "build", "expected_signal": "undefined: foo",
	     "result": "compiler points at main.go", "conclusion": "confirmed", "evidence": [1]}
	  ],
	  "commands": [
	    {"purpose": "reproduce", "command": "go build ./...", "kind": "build",
	     "expected_signal": "exit 1", "risk": "read-only", "evidence": [2]}
	  ],
	  "solution": [{"order": 1, "action": "fix import", "evidence": [1]}],
	  "verification": [{"check": "go build", "status": "passed", "evidence": [2]}],
	  "evidence": [
	    {"event_seq": 1, "kind": "user_report", "quote": "Build fails"},
	    {"event_seq": 2, "kind": "tool_output", "quote": "ok"}
	  ],
	  "confidence": 0.9
	}`
}

func testRedactor(t *testing.T) *redact.Redactor {
	t.Helper()
	r, err := redact.New(redact.DefaultConfig())
	if err != nil {
		t.Fatalf("redact.New: %v", err)
	}
	return r
}

func TestExtractorSuccess(t *testing.T) {
	client := &fakeClient{
		name: "claude-code", version: "fake-cli 1.0", model: "fake-model",
		responses: []string{"```json\n" + validExtractionJSON() + "\n```"},
		usages:    []Usage{{InputTokens: 100, OutputTokens: 20, Known: true}},
	}
	extractor := NewExtractor(client, ExtractorOptions{
		Redactor: testRedactor(t),
		Now:      func() time.Time { return extractNow },
	})

	result, err := extractor.Extract(context.Background(), extractorSegment())
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if result.Doc.Title != "Fix build" || result.Doc.Status != "resolved" {
		t.Fatalf("doc = %+v", result.Doc)
	}
	if result.Doc.ID != "seg-extract" || result.Doc.Provenance.SegmentHash != "seg-hash" {
		t.Fatalf("identity = %+v", result.Doc.Provenance)
	}
	if result.Doc.ReviewStatus != "pending" || result.Doc.Lifecycle != "active" {
		t.Fatalf("review/lifecycle = %q/%q", result.Doc.ReviewStatus, result.Doc.Lifecycle)
	}
	if result.Doc.Validation.EvidenceChecked != 2 || result.Doc.Validation.EvidenceVerified != 2 {
		t.Fatalf("validation = %+v", result.Doc.Validation)
	}
	if result.Doc.Provenance.Provider != "claude-code" || result.Doc.Provenance.Model != "fake-model" {
		t.Fatalf("provenance = %+v", result.Doc.Provenance)
	}
	if result.Doc.Provenance.CLIVersion != "fake-cli 1.0" {
		t.Fatalf("cli version = %q", result.Doc.Provenance.CLIVersion)
	}
	if result.Doc.Provenance.EventRange != [2]int{1, 3} {
		t.Fatalf("event range = %v", result.Doc.Provenance.EventRange)
	}
	if result.Doc.Provenance.PromptHash == "" || result.Doc.Provenance.PromptVersion == "" {
		t.Fatalf("prompt provenance = %+v", result.Doc.Provenance)
	}
	if result.Repairs != 0 || client.callCount != 1 {
		t.Fatalf("repairs/calls = %d/%d", result.Repairs, client.callCount)
	}
	if result.Prompt.Bytes == 0 || result.Prompt.Text == "" {
		t.Fatalf("prompt = %+v", result.Prompt)
	}
	if !result.Usage.Known || result.Usage.TotalTokens() != 120 {
		t.Fatalf("usage = %+v", result.Usage)
	}
}

func TestExtractorRepairsInvalidJSONOnce(t *testing.T) {
	client := &fakeClient{
		name: "codex", version: "codex-fake", model: "codex-model",
		responses: []string{"not json", validExtractionJSON()},
		usages: []Usage{
			{InputTokens: 5, OutputTokens: 1, Known: true},
			{InputTokens: 10, OutputTokens: 2, Known: true},
		},
	}
	extractor := NewExtractor(client, ExtractorOptions{Redactor: testRedactor(t)})

	result, err := extractor.Extract(context.Background(), extractorSegment())
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if result.Repairs != 1 || client.callCount != 2 {
		t.Fatalf("repairs/calls = %d/%d", result.Repairs, client.callCount)
	}
	repairPrompt := client.calls[1].Prompt
	for _, want := range []string{"could not be parsed", "previous_response", "not json"} {
		if !strings.Contains(repairPrompt, want) {
			t.Fatalf("repair prompt missing %q:\n%s", want, repairPrompt)
		}
	}
	if result.Usage.InputTokens != 15 || result.Usage.OutputTokens != 3 {
		t.Fatalf("repair usage = %+v", result.Usage)
	}
}

func TestExtractorEmptyDocumentIsRepaired(t *testing.T) {
	client := &fakeClient{
		name: "claude-code", version: "v", model: "m",
		responses: []string{"{}", validExtractionJSON()},
	}
	extractor := NewExtractor(client, ExtractorOptions{Redactor: testRedactor(t)})
	result, err := extractor.Extract(context.Background(), extractorSegment())
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if result.Repairs != 1 {
		t.Fatalf("repairs = %d", result.Repairs)
	}
}

func TestExtractorFailureSavesRedactedRaw(t *testing.T) {
	rawDir := t.TempDir()
	secret := "sk-abcdefghijklmnopqrstuvwx"
	client := &fakeClient{
		name: "claude-code", version: "v", model: "m",
		responses: []string{"not json " + secret, "still not json " + secret},
	}
	extractor := NewExtractor(client, ExtractorOptions{
		Redactor: testRedactor(t),
		KeepRaw:  true,
		RawDir:   rawDir,
		Now:      func() time.Time { return extractNow },
	})

	_, err := extractor.Extract(context.Background(), extractorSegment())
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "raw response") {
		t.Fatalf("error = %v", err)
	}

	entries, readErr := os.ReadDir(rawDir)
	if readErr != nil || len(entries) != 1 {
		t.Fatalf("raw dir = %v, err = %v", entries, readErr)
	}
	data, readErr := os.ReadFile(filepath.Join(rawDir, entries[0].Name()))
	if readErr != nil {
		t.Fatalf("read raw: %v", readErr)
	}
	content := string(data)
	if strings.Contains(content, secret) {
		t.Fatalf("raw file leaked secret: %s", content)
	}
	if !strings.Contains(content, "<REDACTED:brand-token>") {
		t.Fatalf("raw file was not redacted: %s", content)
	}
}

func TestExtractorClientError(t *testing.T) {
	client := &fakeClient{
		name: "claude-code", version: "v", model: "m",
		errs: []error{errors.New("network down")},
	}
	extractor := NewExtractor(client, ExtractorOptions{Redactor: testRedactor(t)})
	if _, err := extractor.Extract(context.Background(), extractorSegment()); err == nil {
		t.Fatal("expected an error")
	}
}

func TestExtractorPreservesProgramProvenanceHashes(t *testing.T) {
	seg := extractorSegment()
	seg.Hash = strings.Repeat("ab12", 16) // 64 hex chars, like a real SegmentHash
	redactor := testRedactor(t)
	client := &fakeClient{name: "claude-code", version: "v", model: "m", responses: []string{validExtractionJSON()}}
	extractor := NewExtractor(client, ExtractorOptions{Redactor: redactor})

	result, err := extractor.Extract(context.Background(), seg)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	prov := result.Doc.Provenance
	if prov.SegmentHash != seg.Hash {
		t.Fatalf("segment hash = %q, want %q", prov.SegmentHash, seg.Hash)
	}
	if prov.RedactionHash != redactor.Hash() {
		t.Fatalf("redaction hash = %q, want %q", prov.RedactionHash, redactor.Hash())
	}
	if len(prov.PromptHash) != 64 || strings.Contains(prov.PromptHash, "REDACTED") {
		t.Fatalf("prompt hash was altered: %q", prov.PromptHash)
	}
}

func TestExtractorRedactsModelEcho(t *testing.T) {
	secret := "sk-abcdefghijklmnopqrstuvwx"
	doc := strings.Replace(validExtractionJSON(), `"title": "Fix build"`, `"title": "Fix `+secret+`"`, 1)
	client := &fakeClient{name: "claude-code", version: "v", model: "m", responses: []string{doc}}
	extractor := NewExtractor(client, ExtractorOptions{Redactor: testRedactor(t)})

	result, err := extractor.Extract(context.Background(), extractorSegment())
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if strings.Contains(result.Doc.Title, secret) {
		t.Fatalf("title leaked secret: %q", result.Doc.Title)
	}
	if !strings.Contains(result.Doc.Title, "<REDACTED:brand-token>") {
		t.Fatalf("title = %q", result.Doc.Title)
	}
}

func TestExtractorDowngradesUnprovenVerification(t *testing.T) {
	seg := extractorSegment()
	seg.Events[1].Exec = &ingest.ExecResult{Known: true, ExitCode: intPtr(1)}
	client := &fakeClient{name: "claude-code", version: "v", model: "m", responses: []string{validExtractionJSON()}}
	extractor := NewExtractor(client, ExtractorOptions{Redactor: testRedactor(t)})

	result, err := extractor.Extract(context.Background(), seg)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if result.Doc.Verification[0].Status != "unknown" {
		t.Fatalf("verification = %+v", result.Doc.Verification[0])
	}
	if result.Doc.Status != "unknown" {
		t.Fatalf("status = %q", result.Doc.Status)
	}
	if len(result.Doc.Uncertain) == 0 {
		t.Fatal("expected uncertainty")
	}
}
