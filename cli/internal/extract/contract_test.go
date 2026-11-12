package extract

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"devlog-learn/internal/ingest"
	"devlog-learn/internal/segment"
	"devlog-learn/internal/version"
)

// requiredDocFields is the RAG-facing contract. schema_version may only grow
// additively: adding a field is fine, removing or renaming one of these breaks
// downstream consumers. Update this list deliberately when the schema changes.
var requiredDocFields = []string{
	"schema_version", "id", "slug", "title", "summary", "project", "source",
	"knowledge_type", "occurred_at", "ended_at", "duration_minutes",
	"stack", "tags", "difficulty", "status", "review_status", "lifecycle",
	"problem", "root_cause", "solution", "insights", "pitfalls", "verification",
	"lessons", "open_questions", "artifacts", "evidence", "uncertain", "confidence",
	"validation", "provenance",
}

var requiredEvidenceFields = []string{"event_seq", "kind", "quote", "verified"}

var requiredProvenanceFields = []string{
	"session_id", "segment_id", "segment_hash", "event_range", "source_event_count",
	"provider", "model", "cli_version", "prompt_version", "prompt_hash",
	"schema_version", "segmenter_version", "redaction_version", "build_version",
	"extracted_at",
}

func contractProvenance() Provenance {
	return Provenance{
		Provider:      "claude-code",
		Model:         "contract-model",
		CLIVersion:    "contract-cli 1.0",
		PromptVersion: "extract-v1",
		PromptHash:    strings.Repeat("a", 64),
		RedactionHash: strings.Repeat("b", 64),
		ExtractedAt:   time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC),
	}
}

func TestDocJSONContractIsAdditiveAndSelfContained(t *testing.T) {
	seg := extractorSegment()
	doc, err := ParseOutput(validExtractionJSON())
	if err != nil {
		t.Fatalf("ParseOutput: %v", err)
	}
	doc.Normalize(seg, contractProvenance())
	ValidateEvidence(doc, seg, map[int]bool{1: true, 2: true, 3: true})

	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal doc: %v", err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("decode doc object: %v", err)
	}
	for _, field := range requiredDocFields {
		if _, ok := raw[field]; !ok {
			t.Fatalf("schema contract: missing top-level field %q", field)
		}
	}

	if doc.SchemaVersion != version.SchemaVersion {
		t.Fatalf("schema_version = %d, want %d", doc.SchemaVersion, version.SchemaVersion)
	}
	if doc.ID != seg.ID || doc.Provenance.SegmentID != seg.ID || doc.Provenance.SegmentHash != seg.Hash {
		t.Fatalf("identity contract: doc=%q prov=%q/%q seg=%q/%q",
			doc.ID, doc.Provenance.SegmentID, doc.Provenance.SegmentHash, seg.ID, seg.Hash)
	}

	var knowledgeType string
	if err := json.Unmarshal(raw["knowledge_type"], &knowledgeType); err != nil || knowledgeType == "" {
		t.Fatalf("knowledge_type = %q, err = %v", knowledgeType, err)
	}
	if doc.KnowledgeType != knowledgeType {
		t.Fatalf("knowledge_type mismatch: doc=%q json=%q", doc.KnowledgeType, knowledgeType)
	}

	var tags, stack []string
	if err := json.Unmarshal(raw["tags"], &tags); err != nil {
		t.Fatalf("tags must be an array: %v", err)
	}
	if err := json.Unmarshal(raw["stack"], &stack); err != nil {
		t.Fatalf("stack must be an array: %v", err)
	}

	var evidence []map[string]json.RawMessage
	if err := json.Unmarshal(raw["evidence"], &evidence); err != nil {
		t.Fatalf("evidence must be an array: %v", err)
	}
	if len(evidence) == 0 {
		t.Fatal("contract doc must carry evidence")
	}
	for _, item := range evidence {
		for _, field := range requiredEvidenceFields {
			if _, ok := item[field]; !ok {
				t.Fatalf("evidence contract: missing field %q in %v", field, item)
			}
		}
	}

	var provenance map[string]json.RawMessage
	if err := json.Unmarshal(raw["provenance"], &provenance); err != nil {
		t.Fatalf("provenance must be an object: %v", err)
	}
	for _, field := range requiredProvenanceFields {
		if _, ok := provenance[field]; !ok {
			t.Fatalf("provenance contract: missing field %q", field)
		}
	}
	var provider, model, promptVersion string
	if err := json.Unmarshal(provenance["provider"], &provider); err != nil || provider != "claude-code" {
		t.Fatalf("provenance provider = %q, err = %v", provider, err)
	}
	if err := json.Unmarshal(provenance["model"], &model); err != nil || model != "contract-model" {
		t.Fatalf("provenance model = %q, err = %v", model, err)
	}
	if err := json.Unmarshal(provenance["prompt_version"], &promptVersion); err != nil || promptVersion == "" {
		t.Fatalf("provenance prompt_version = %q, err = %v", promptVersion, err)
	}
}

func TestDocIDStableWhenOnlySegmentContentChanges(t *testing.T) {
	seg := extractorSegment()
	first, err := ParseOutput(validExtractionJSON())
	if err != nil {
		t.Fatalf("ParseOutput: %v", err)
	}
	first.Normalize(seg, contractProvenance())

	appended := seg
	appended.Events = append(append([]ingest.Event(nil), seg.Events...), ingest.Event{
		Kind: ingest.KindAssistantText, Role: "assistant", Text: "one more follow-up", Seq: 4,
	})
	segment.SetHash(&appended)
	second, err := ParseOutput(validExtractionJSON())
	if err != nil {
		t.Fatalf("ParseOutput second: %v", err)
	}
	second.Normalize(appended, contractProvenance())

	if first.ID != second.ID || first.ID != seg.ID {
		t.Fatalf("id changed with content: first=%q second=%q segment=%q", first.ID, second.ID, seg.ID)
	}
	if first.Provenance.SegmentHash == second.Provenance.SegmentHash {
		t.Fatal("segment hash should change when events are appended")
	}
	if first.Slug != second.Slug {
		t.Fatalf("slug should remain stable for the same segment identity: %q vs %q", first.Slug, second.Slug)
	}
}
