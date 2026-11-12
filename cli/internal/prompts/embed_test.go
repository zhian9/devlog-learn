package prompts

import (
	"strings"
	"testing"
)

func TestHashIsStable(t *testing.T) {
	first := Hash()
	second := Hash()
	if first == "" || len(first) != 64 {
		t.Fatalf("hash = %q", first)
	}
	if first != second {
		t.Fatalf("hash changed: %q vs %q", first, second)
	}
}

func TestRenderExtract(t *testing.T) {
	data := map[string]any{
		"Language":   "zh",
		"Project":    "demo",
		"Source":     "claude-code",
		"SessionID":  "s1",
		"SegmentID":  "seg1",
		"Start":      "2026-01-01T00:00:00Z",
		"End":        "2026-01-01T00:10:00Z",
		"EventCount": 2,
		"Transcript": `<event seq="1">hello</event>`,
		"Truncated":  false,
		"Omitted":    "",
	}
	out, err := Render("extract.md.tmpl", data)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	for _, want := range []string{
		"Simplified Chinese", "EVIDENCE RULES", "segment_id: seg1", "hello",
		"\"concepts\"", "\"rejected_hypotheses\"", "\"blind_spots\"", "\"learning_value\"",
		"minimal, runnable example",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q:\n%s", want, out)
		}
	}
}

func TestRenderRequiresAllKeys(t *testing.T) {
	if _, err := Render("extract.md.tmpl", map[string]any{"Language": "zh"}); err == nil {
		t.Fatal("expected missing-key error")
	}
	if _, err := Render("missing.tmpl", map[string]any{}); err == nil {
		t.Fatal("expected missing-template error")
	}
}

func TestRenderRepair(t *testing.T) {
	out, err := Render("repair.md.tmpl", map[string]any{
		"Previous": "not json",
		"Error":    "invalid",
		"Language": "en",
	})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	for _, want := range []string{"not json", "English", "concepts", "rejected_hypotheses", "blind_spots", "learning_value"} {
		if !strings.Contains(out, want) {
			t.Fatalf("repair output missing %q:\n%s", want, out)
		}
	}
}
