package extract

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// ErrInvalidOutput marks a response that could not be decoded into Doc JSON.
var ErrInvalidOutput = errors.New("invalid model output")

// ParseOutput accepts raw model text, strips markdown fences or surrounding
// prose, and decodes one JSON object.
func ParseOutput(raw string) (*Doc, error) {
	candidate := locateJSON(raw)
	if candidate == "" {
		return nil, fmt.Errorf("%w: no JSON object found", ErrInvalidOutput)
	}

	dec := json.NewDecoder(strings.NewReader(candidate))
	var doc Doc
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidOutput, err)
	}
	if err := doc.Validate(); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidOutput, err)
	}
	return &doc, nil
}

func locateJSON(raw string) string {
	s := strings.TrimSpace(strings.TrimPrefix(raw, string(rune(0xFEFF))))
	if s == "" {
		return ""
	}

	if strings.HasPrefix(s, "```") {
		if nl := strings.IndexByte(s, '\n'); nl >= 0 {
			s = s[nl+1:]
		}
		if end := strings.Index(s, "```"); end >= 0 {
			s = s[:end]
		}
		s = strings.TrimSpace(s)
	}

	start := strings.IndexByte(s, '{')
	end := strings.LastIndexByte(s, '}')
	if start >= 0 && end > start {
		return s[start : end+1]
	}
	return ""
}
