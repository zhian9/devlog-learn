package extract

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"devlog-learn/internal/ingest"
	"devlog-learn/internal/segment"
)

var docTime = time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC)

func testSegment() segment.Segment {
	return segment.Segment{
		ID:        "seg-1234567890ab",
		Hash:      "hash-value",
		Source:    "claude-code",
		SessionID: "sess-1",
		Project:   "demo",
		Title:     "Fix the build",
		Start:     docTime,
		End:       docTime.Add(5 * time.Minute),
		Events: []ingest.Event{
			{Kind: ingest.KindUserText, Role: "user", Text: "Build fails", Seq: 1, File: "session.jsonl"},
			{Kind: ingest.KindToolResult, Role: "tool", ToolResult: "ok", Seq: 2, File: "session.jsonl",
				Exec: &ingest.ExecResult{Known: true, ExitCode: intPtr(0)}},
		},
	}
}

func intPtr(v int) *int { return &v }

func TestNormalizeFillsDeterministicFields(t *testing.T) {
	doc := &Doc{
		Title:         "",
		Summary:       "summary",
		KnowledgeType: "not-a-type",
		Difficulty:    "extreme",
		Status:        "not-a-status",
		ReviewStatus:  "approved",
		Lifecycle:     "stale",
		Tags:          []string{" Go ", "go", "", "CLI"},
		Stack:         []string{"Go", "go"},
		Solution: []Step{
			{Order: 0, Action: "second", Evidence: []int{1}},
			{Order: 0, Action: "first", Evidence: []int{1}},
		},
		Evidence: []Evidence{
			{EventSeq: 1, Kind: "not-a-kind", Quote: "Build fails"},
			{EventSeq: 1, Kind: "user_report", Quote: "Build fails with more text"},
		},
		Confidence: 2.5,
	}

	doc.Normalize(testSegment(), Provenance{})

	if doc.SchemaVersion != SchemaVersion {
		t.Fatalf("schema version = %d", doc.SchemaVersion)
	}
	if doc.ID != "seg-1234567890ab" || doc.Project != "demo" || doc.Source != "claude-code" {
		t.Fatalf("identity = %+v", doc)
	}
	if doc.Title != "Fix the build" {
		t.Fatalf("title = %q", doc.Title)
	}
	if doc.Slug != "2026-05-01-fix-the-build" {
		t.Fatalf("slug = %q", doc.Slug)
	}
	if doc.KnowledgeType != "learning" || doc.Difficulty != "unknown" || doc.Status != "unknown" {
		t.Fatalf("enums = %q/%q/%q", doc.KnowledgeType, doc.Difficulty, doc.Status)
	}
	if doc.ReviewStatus != "pending" || doc.Lifecycle != "active" {
		t.Fatalf("review/lifecycle = %q/%q", doc.ReviewStatus, doc.Lifecycle)
	}
	if doc.Confidence != 1 {
		t.Fatalf("confidence = %v", doc.Confidence)
	}
	if len(doc.Tags) != 2 || doc.Tags[0] != "Go" || doc.Tags[1] != "CLI" {
		t.Fatalf("tags = %v", doc.Tags)
	}
	if len(doc.Solution) != 2 || doc.Solution[0].Order != 1 || doc.Solution[1].Order != 2 {
		t.Fatalf("solution = %+v", doc.Solution)
	}
	if len(doc.Evidence) != 1 || doc.Evidence[0].Quote != "Build fails with more text" {
		t.Fatalf("evidence = %+v", doc.Evidence)
	}
	if doc.Evidence[0].Kind != "user_report" {
		t.Fatalf("evidence kind = %q", doc.Evidence[0].Kind)
	}
	if doc.Provenance.SegmentID != "seg-1234567890ab" || doc.Provenance.SegmentHash != "hash-value" {
		t.Fatalf("provenance = %+v", doc.Provenance)
	}
	if doc.Provenance.EventRange != [2]int{1, 2} || doc.Provenance.SourceEventCount != 2 {
		t.Fatalf("event range = %+v", doc.Provenance)
	}
	if doc.DurationMin != 5 {
		t.Fatalf("duration = %d", doc.DurationMin)
	}
}

func TestNormalizeDiagnosisAndCommands(t *testing.T) {
	doc := &Doc{
		Title:   "Learning case",
		Summary: "summary",
		Diagnosis: []DiagnosisStep{
			{Order: 0, Observation: "  auto   order ", CommandKind: "BUILD", Command: " go build ./... "},
			{Order: 2, Observation: "explicit order", CommandKind: "not-a-kind"},
		},
		Commands: []Command{
			{Purpose: " reproduce ", Command: " go build ./... ", Kind: "BUILD"},
			{Purpose: "duplicate", Command: "go build ./...", Kind: "read"},
			{Purpose: "second", Command: " grep -rn foo . ", Kind: "shell"},
			{Purpose: "empty", Command: "   "},
		},
	}
	doc.Normalize(testSegment(), Provenance{})

	if len(doc.Diagnosis) != 2 {
		t.Fatalf("diagnosis = %+v", doc.Diagnosis)
	}
	if doc.Diagnosis[0].Order != 1 || doc.Diagnosis[1].Order != 2 {
		t.Fatalf("diagnosis order = %+v", doc.Diagnosis)
	}
	if doc.Diagnosis[0].Observation != "auto order" {
		t.Fatalf("diagnosis sort = %+v", doc.Diagnosis)
	}
	if doc.Diagnosis[0].CommandKind != "build" || doc.Diagnosis[0].Command != "go build ./..." {
		t.Fatalf("diagnosis command = %+v", doc.Diagnosis[0])
	}
	if doc.Diagnosis[1].CommandKind != "unknown" {
		t.Fatalf("invalid command kind = %q", doc.Diagnosis[1].CommandKind)
	}

	if len(doc.Commands) != 2 {
		t.Fatalf("commands = %+v", doc.Commands)
	}
	if doc.Commands[0].Command != "go build ./..." || doc.Commands[0].Purpose != "reproduce" || doc.Commands[0].Kind != "build" {
		t.Fatalf("first command = %+v", doc.Commands[0])
	}
	if doc.Commands[1].Command != "grep -rn foo ." || doc.Commands[1].Kind != "unknown" {
		t.Fatalf("second command = %+v", doc.Commands[1])
	}
}

func TestValidateEvidenceFiltersDiagnosisAndCommands(t *testing.T) {
	doc := &Doc{
		Title:   "Learning case",
		Summary: "summary",
		Diagnosis: []DiagnosisStep{
			{Observation: "kept", Evidence: []int{1, 99}},
		},
		Commands: []Command{
			{Purpose: "kept", Command: "go build ./...", Evidence: []int{2, 99}},
		},
		Evidence: []Evidence{
			{EventSeq: 1, Kind: "user_report", Quote: "Build fails"},
			{EventSeq: 2, Kind: "tool_output", Quote: "ok"},
		},
	}
	doc.Normalize(testSegment(), Provenance{})
	ValidateEvidence(doc, testSegment(), map[int]bool{1: true, 2: true})

	if len(doc.Diagnosis) != 1 || len(doc.Diagnosis[0].Evidence) != 1 || doc.Diagnosis[0].Evidence[0] != 1 {
		t.Fatalf("diagnosis evidence = %+v", doc.Diagnosis)
	}
	if len(doc.Commands) != 1 || len(doc.Commands[0].Evidence) != 1 || doc.Commands[0].Evidence[0] != 2 {
		t.Fatalf("command evidence = %+v", doc.Commands)
	}
}

func TestNormalizeLearningFields(t *testing.T) {
	doc := &Doc{
		Title:         "Learning case",
		Summary:       "summary",
		LearningValue: "HIGH",
		Concepts: []Concept{
			{
				Name:        "  kafka-consumer-groups ",
				Stack:       " Kafka ",
				WhyRelevant: "  shows   consumer   lag ",
				Syntax:      " kafka-consumer-groups --describe ",
				Example:     " kafka-consumer-groups --bootstrap-server localhost:9092 --describe --group payment ",
				DocsURL:     " https://kafka.apache.org/documentation ",
			},
			{Name: "   ", Example: "orphan without a name"},
			{Name: "Gin routes", Stack: "Gin"},
			{Name: "gin routes", Stack: "gin"},
		},
		RejectedHypotheses: []RejectedHypothesis{
			{Hypothesis: "  stale cache ", WhyRejected: "  offsets   were current "},
			{Hypothesis: "", WhyRejected: ""},
		},
		BlindSpots: []BlindSpot{
			{Point: "  consumer group offsets ", Kind: "FRAMEWORK_KNOWLEDGE", WhyItMatters: "  lag diagnosis "},
			{Point: "unknown kind", Kind: "not-a-kind"},
		},
	}
	doc.Normalize(testSegment(), Provenance{})

	if doc.LearningValue != "high" {
		t.Fatalf("learning value = %q", doc.LearningValue)
	}
	if len(doc.Concepts) != 2 {
		t.Fatalf("concepts = %+v", doc.Concepts)
	}
	first := doc.Concepts[0]
	if first.Name != "kafka-consumer-groups" || first.Stack != "Kafka" || first.WhyRelevant != "shows consumer lag" {
		t.Fatalf("concept text = %+v", first)
	}
	if first.Syntax != "kafka-consumer-groups --describe" || first.Example == "" || first.DocsURL == "" {
		t.Fatalf("concept code fields = %+v", first)
	}
	if len(doc.RejectedHypotheses) != 1 || doc.RejectedHypotheses[0].Hypothesis != "stale cache" {
		t.Fatalf("rejected hypotheses = %+v", doc.RejectedHypotheses)
	}
	if len(doc.BlindSpots) != 2 || doc.BlindSpots[0].Kind != "framework_knowledge" || doc.BlindSpots[1].Kind != "" {
		t.Fatalf("blind spots = %+v", doc.BlindSpots)
	}
}

func TestValidateEvidenceFiltersLearningFields(t *testing.T) {
	doc := &Doc{
		Title:   "Learning case",
		Summary: "summary",
		Concepts: []Concept{
			{Name: "kept", Evidence: []int{1, 99}},
			{Name: "no evidence"},
		},
		RejectedHypotheses: []RejectedHypothesis{
			{Hypothesis: "kept", WhyRejected: "checked", Evidence: []int{2, 99}},
		},
		BlindSpots: []BlindSpot{
			{Point: "kept", Evidence: []int{99}},
		},
		Evidence: []Evidence{
			{EventSeq: 1, Kind: "user_report", Quote: "Build fails"},
			{EventSeq: 2, Kind: "tool_output", Quote: "ok"},
		},
	}
	doc.Normalize(testSegment(), Provenance{})
	ValidateEvidence(doc, testSegment(), map[int]bool{1: true, 2: true})

	if len(doc.Concepts) != 2 || len(doc.Concepts[0].Evidence) != 1 || doc.Concepts[0].Evidence[0] != 1 {
		t.Fatalf("concept evidence = %+v", doc.Concepts)
	}
	if len(doc.Concepts[1].Evidence) != 0 {
		t.Fatalf("concept without evidence should only degrade: %+v", doc.Concepts[1])
	}
	if len(doc.RejectedHypotheses[0].Evidence) != 1 || doc.RejectedHypotheses[0].Evidence[0] != 2 {
		t.Fatalf("rejected hypothesis evidence = %+v", doc.RejectedHypotheses)
	}
	if len(doc.BlindSpots[0].Evidence) != 0 {
		t.Fatalf("blind spot evidence should be filtered: %+v", doc.BlindSpots)
	}
	if doc.Confidence > 0.6 {
		t.Fatalf("confidence should be downgraded, got %v", doc.Confidence)
	}
	warnings := strings.Join(doc.Validation.Warnings, "\n")
	for _, want := range []string{"concepts lost", "rejected_hypotheses lost", "blind_spots lost", "concept 2", "blind spot 1"} {
		if !strings.Contains(warnings, want) {
			t.Fatalf("warnings missing %q:\n%s", want, warnings)
		}
	}
}

func TestOldDocJSONWithoutLearningFieldsStillParses(t *testing.T) {
	doc, err := ParseOutput(`{"title":"Legacy doc","summary":"old shape","solution":[{"action":"fix"}]}`)
	if err != nil {
		t.Fatalf("ParseOutput legacy: %v", err)
	}
	doc.Normalize(testSegment(), Provenance{})
	if doc.LearningValue != "" || len(doc.Concepts) != 0 || len(doc.RejectedHypotheses) != 0 || len(doc.BlindSpots) != 0 {
		t.Fatalf("legacy doc gained learning fields: %+v", doc)
	}
	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal legacy doc: %v", err)
	}
	for _, field := range []string{"concepts", "rejected_hypotheses", "blind_spots", "learning_value"} {
		if strings.Contains(string(data), `"`+field+`"`) {
			t.Fatalf("empty optional field %q should stay omitted: %s", field, data)
		}
	}
}

func TestMakeSlug(t *testing.T) {
	tests := []struct {
		title string
		at    time.Time
		want  string
	}{
		{title: "Fix: Windows path / issue?", at: docTime, want: "2026-05-01-fix-windows-path-issue"},
		{title: "修复 并发 bug", at: docTime, want: "2026-05-01-修复-并发-bug"},
		{title: "", at: time.Time{}, want: "undated-untitled"},
	}
	for _, tt := range tests {
		if got := MakeSlug(tt.title, tt.at); got != tt.want {
			t.Fatalf("MakeSlug(%q) = %q, want %q", tt.title, got, tt.want)
		}
	}
}

func TestValidateEmptyDocument(t *testing.T) {
	if err := (&Doc{}).Validate(); !errors.Is(err, ErrEmptyDoc) {
		t.Fatalf("error = %v, want ErrEmptyDoc", err)
	}
	if err := (&Doc{Title: "x"}).Validate(); err != nil {
		t.Fatalf("non-empty document error: %v", err)
	}
}

func TestNormalizeAddsWarnings(t *testing.T) {
	doc := &Doc{Title: "Only a title", KnowledgeType: "bug_fix"}
	doc.Normalize(testSegment(), Provenance{})
	if len(doc.Validation.Warnings) == 0 {
		t.Fatal("expected warnings for missing summary/solution")
	}
	if !strings.Contains(strings.Join(doc.Validation.Warnings, " "), "solution is empty") {
		t.Fatalf("warnings = %v", doc.Validation.Warnings)
	}
}
