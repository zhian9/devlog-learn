package extract

import (
	"errors"
	"strings"
	"testing"
)

func TestParseOutputVariants(t *testing.T) {
	tests := []struct {
		name string
		raw  string
	}{
		{name: "plain", raw: `{"title":"x","summary":"y"}`},
		{name: "fenced", raw: "```json\n{\"title\":\"x\",\"summary\":\"y\"}\n```"},
		{name: "prose", raw: "Here is the result:\n{\"title\":\"x\",\"summary\":\"y\"}\nDone."},
		{name: "bom", raw: string(rune(0xFEFF)) + `{"title":"x","summary":"y"}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, err := ParseOutput(tt.raw)
			if err != nil {
				t.Fatalf("ParseOutput: %v", err)
			}
			if doc.Title != "x" || doc.Summary != "y" {
				t.Fatalf("doc = %+v", doc)
			}
		})
	}
}

func TestParseOutputErrors(t *testing.T) {
	tests := []string{
		"",
		"no json here",
		`{"title":`,
		`{}`,
		`{"title": 42}`,
	}
	for _, raw := range tests {
		if _, err := ParseOutput(raw); !errors.Is(err, ErrInvalidOutput) {
			t.Fatalf("ParseOutput(%q) error = %v, want ErrInvalidOutput", raw, err)
		}
	}
}

func TestParseOutputKeepsUnknownFields(t *testing.T) {
	doc, err := ParseOutput(`{"title":"x","summary":"y","future_field":{"a":1}}`)
	if err != nil {
		t.Fatalf("ParseOutput: %v", err)
	}
	if doc.Title != "x" {
		t.Fatalf("title = %q", doc.Title)
	}
}

func TestParseOutputLastBrace(t *testing.T) {
	raw := `{"title":"contains } brace","summary":"y"} trailing`
	doc, err := ParseOutput(raw)
	if err != nil {
		t.Fatalf("ParseOutput: %v", err)
	}
	if !strings.Contains(doc.Title, "}") {
		t.Fatalf("title = %q", doc.Title)
	}
}
