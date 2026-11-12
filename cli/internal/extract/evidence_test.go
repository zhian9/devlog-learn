package extract

import (
	"strings"
	"testing"

	"devlog-learn/internal/ingest"
	"devlog-learn/internal/segment"
)

func evidenceSegment() segment.Segment {
	return segment.Segment{
		ID: "seg-evidence", Source: "codex", SessionID: "s1", Project: "demo",
		Events: []ingest.Event{
			{Kind: ingest.KindUserText, Role: "user", Text: "Connection refused error", Seq: 1},
			{Kind: ingest.KindToolResult, Role: "tool", ToolResult: "permission denied", Seq: 2,
				Exec: &ingest.ExecResult{Known: true, ExitCode: intPtr(1)}},
			{Kind: ingest.KindToolResult, Role: "tool", ToolResult: "all tests passed", Seq: 3,
				Exec: &ingest.ExecResult{Known: true, ExitCode: intPtr(0)}},
			{Kind: ingest.KindAssistantText, Role: "assistant", Text: "Fixed the problem", Seq: 4},
		},
	}
}

func newEvidenceDoc() *Doc {
	return &Doc{
		Title:         "Fix connection",
		Summary:       "summary",
		KnowledgeType: "bug_fix",
		Status:        "resolved",
		Problem:       Problem{Statement: "connection error", Evidence: []int{1, 99}},
		RootCause:     RootCause{Text: "port mismatch", Known: true, Confidence: "high", Evidence: []int{1}},
		Solution:      []Step{{Order: 1, Action: "change port", Evidence: []int{1}}},
		Verification: []Verification{
			{Check: "bad evidence", Status: "passed", Evidence: []int{2}},
			{Check: "good evidence", Status: "passed", Evidence: []int{3}},
		},
		Evidence: []Evidence{
			{EventSeq: 1, Kind: "user_report", Quote: "Connection refused"},
			{EventSeq: 2, Kind: "tool_output", Quote: "permission denied"},
			{EventSeq: 3, Kind: "tool_output", Quote: "all tests passed"},
			{EventSeq: 99, Kind: "user_report", Quote: "fabricated"},
		},
		Confidence: 0.9,
	}
}

func prepareEvidenceDoc(t *testing.T, included map[int]bool) (*Doc, segment.Segment) {
	t.Helper()
	seg := evidenceSegment()
	doc := newEvidenceDoc()
	doc.Normalize(seg, Provenance{})
	ValidateEvidence(doc, seg, included)
	return doc, seg
}

func TestValidateEvidenceFiltersAndVerifies(t *testing.T) {
	doc, _ := prepareEvidenceDoc(t, nil)

	if len(doc.Evidence) != 3 {
		t.Fatalf("evidence = %+v", doc.Evidence)
	}
	for _, ev := range doc.Evidence {
		if !ev.Verified {
			t.Fatalf("evidence verification = %+v", doc.Evidence)
		}
	}
	if doc.Validation.EvidenceChecked != 3 || doc.Validation.EvidenceVerified != 3 {
		t.Fatalf("validation = %+v", doc.Validation)
	}
	if len(doc.Problem.Evidence) != 1 || doc.Problem.Evidence[0] != 1 {
		t.Fatalf("problem evidence = %v", doc.Problem.Evidence)
	}
	if doc.RootCause.Known != true {
		t.Fatalf("root cause = %+v", doc.RootCause)
	}
	if doc.Verification[0].Status != "unknown" {
		t.Fatalf("exit-1 verification = %+v", doc.Verification[0])
	}
	if doc.Verification[1].Status != "passed" {
		t.Fatalf("exit-0 verification = %+v", doc.Verification[1])
	}
	if doc.Status != "resolved" {
		t.Fatalf("status = %q", doc.Status)
	}
}

func TestValidateEvidenceRejectsOmittedEvents(t *testing.T) {
	doc, _ := prepareEvidenceDoc(t, map[int]bool{1: true, 2: true})

	for _, ev := range doc.Evidence {
		if ev.EventSeq == 3 {
			t.Fatalf("omitted seq 3 survived: %+v", doc.Evidence)
		}
	}
	if doc.Verification[1].Status != "unknown" {
		t.Fatalf("verification should be downgraded: %+v", doc.Verification[1])
	}
	if doc.Status != "unknown" {
		t.Fatalf("status = %q, want unknown", doc.Status)
	}
}

// Tool inputs arrive as raw JSON, where newlines are escaped as literal \n.
// Models quote the decoded code, so matching must consider decoded strings or
// every multi-line file_change quote is wrongly marked unverified.
func TestValidateEvidenceMatchesDecodedToolInput(t *testing.T) {
	raw := []byte(`{"file_path":"index.html","new_string":"    .stats {\n      display: flex;\n      flex-wrap: wrap;\n    }"}`)
	seg := segment.Segment{
		ID: "seg-tool-input",
		Events: []ingest.Event{
			{Kind: ingest.KindToolUse, Role: "assistant", ToolName: "Edit", ToolInput: raw, Seq: 1},
		},
	}
	doc := &Doc{
		Title:   "x",
		Summary: "y",
		Evidence: []Evidence{
			{EventSeq: 1, Kind: "file_change", Quote: "display: flex;\n      flex-wrap: wrap;"},
		},
		Confidence: 0.5,
	}
	doc.Normalize(seg, Provenance{})
	ValidateEvidence(doc, seg, nil)

	if len(doc.Evidence) != 1 || !doc.Evidence[0].Verified {
		t.Fatalf("decoded tool input quote not verified: %+v", doc.Evidence)
	}
	if doc.Validation.EvidenceVerified != 1 {
		t.Fatalf("validation = %+v", doc.Validation)
	}
}

// Decoding must not join separate JSON fields into one searchable string:
// otherwise a quote that spans unrelated fields could be "verified".
func TestValidateEvidenceDoesNotMatchAcrossJSONFields(t *testing.T) {
	raw := []byte(`{"old_string":"alpha","new_string":"beta"}`)
	seg := segment.Segment{
		ID: "seg-tool-input-cross",
		Events: []ingest.Event{
			{Kind: ingest.KindToolUse, Role: "assistant", ToolName: "Edit", ToolInput: raw, Seq: 1},
		},
	}
	doc := &Doc{
		Title:   "x",
		Summary: "y",
		Evidence: []Evidence{
			{EventSeq: 1, Kind: "file_change", Quote: "alpha\nbeta"},
		},
		Confidence: 0.5,
	}
	doc.Normalize(seg, Provenance{})
	ValidateEvidence(doc, seg, nil)

	if len(doc.Evidence) != 1 || doc.Evidence[0].Verified {
		t.Fatalf("cross-field quote must stay unverified: %+v", doc.Evidence)
	}
}

func TestValidateEvidenceDowngradesFabricatedClaims(t *testing.T) {
	seg := evidenceSegment()
	doc := &Doc{
		Title:     "x",
		Summary:   "y",
		Status:    "resolved",
		RootCause: RootCause{Text: "cause", Known: true, Confidence: "high", Evidence: []int{99}},
		Verification: []Verification{
			{Check: "claimed", Status: "passed", Evidence: []int{99}},
		},
		Evidence: []Evidence{
			{EventSeq: 1, Kind: "user_report", Quote: "Connection refused"},
		},
		Confidence: 0.9,
	}
	doc.Normalize(seg, Provenance{})
	ValidateEvidence(doc, seg, nil)

	if doc.RootCause.Known {
		t.Fatalf("root cause = %+v", doc.RootCause)
	}
	if doc.Verification[0].Status != "unknown" {
		t.Fatalf("verification = %+v", doc.Verification[0])
	}
	if doc.Status != "unknown" {
		t.Fatalf("status = %q", doc.Status)
	}
	if doc.Confidence > 0.4 {
		t.Fatalf("confidence = %v, want capped", doc.Confidence)
	}
	if len(doc.Uncertain) == 0 {
		t.Fatal("expected uncertainty entries")
	}
	if !strings.Contains(strings.Join(doc.Validation.Warnings, " "), "root cause") {
		t.Fatalf("warnings = %v", doc.Validation.Warnings)
	}
}
