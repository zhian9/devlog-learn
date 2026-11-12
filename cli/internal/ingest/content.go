package ingest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// textBlockTypes are the content block types whose only payload we need is
// their text. Providers use slightly different names for the same concept.
var textBlockTypes = map[string]bool{
	"text":           true,
	"input_text":     true,
	"output_text":    true,
	"summary_text":   true,
	"reasoning_text": true,
}

// decodeContent splits a provider content field into either a plain string or
// a list of raw blocks. Raw blocks are preserved so unknown content can be
// emitted as KindUnknown without losing fields.
func decodeContent(raw json.RawMessage) (string, []json.RawMessage, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return "", nil, nil
	}

	switch trimmed[0] {
	case '"':
		var s string
		if err := json.Unmarshal(trimmed, &s); err != nil {
			return "", nil, fmt.Errorf("decode string content: %w", err)
		}
		return s, nil, nil
	case '[':
		var blocks []json.RawMessage
		if err := json.Unmarshal(trimmed, &blocks); err != nil {
			return "", nil, fmt.Errorf("decode content array: %w", err)
		}
		return "", blocks, nil
	case '{':
		return "", []json.RawMessage{append(json.RawMessage(nil), trimmed...)}, nil
	default:
		return "", nil, fmt.Errorf("unsupported content shape starting with %q", string(trimmed[:1]))
	}
}

// contentText renders a tool result/output value as text while preserving
// structured data and marking non-text blocks. It never silently drops a
// non-empty block.
func contentText(raw json.RawMessage) (string, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return "", nil
	}

	if trimmed[0] == '"' {
		var s string
		if err := json.Unmarshal(trimmed, &s); err != nil {
			return "", fmt.Errorf("decode string output: %w", err)
		}
		return s, nil
	}

	var blocks []json.RawMessage
	switch trimmed[0] {
	case '[':
		if err := json.Unmarshal(trimmed, &blocks); err != nil {
			return "", fmt.Errorf("decode output array: %w", err)
		}
	case '{':
		blocks = []json.RawMessage{append(json.RawMessage(nil), trimmed...)}
	default:
		return "", fmt.Errorf("unsupported output shape starting with %q", string(trimmed[:1]))
	}

	parts := make([]string, 0, len(blocks))
	for _, block := range blocks {
		var head struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if err := json.Unmarshal(block, &head); err != nil {
			// Preserve the raw block; a malformed block should not erase data.
			parts = append(parts, string(bytes.TrimSpace(block)))
			continue
		}

		switch {
		case textBlockTypes[head.Type]:
			if head.Text != "" {
				parts = append(parts, head.Text)
			}
		case head.Type == "image" || head.Type == "input_image":
			parts = append(parts, "[image]")
		case head.Text != "":
			parts = append(parts, head.Text)
		case head.Type != "":
			parts = append(parts, "["+head.Type+"]")
		default:
			parts = append(parts, string(bytes.TrimSpace(block)))
		}
	}
	return strings.Join(parts, "\n"), nil
}

func joinNonEmpty(parts ...string) string {
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if strings.TrimSpace(part) != "" {
			out = append(out, strings.TrimRight(part, "\r\n"))
		}
	}
	return strings.Join(out, "\n")
}
