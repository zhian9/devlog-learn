package config

import (
	"strings"
	"testing"
)

func TestValidateProvider(t *testing.T) {
	cases := map[string]string{
		"claude-code": "claude-code",
		"Codex":       "codex",
		"  ALL  ":     "all",
	}
	for input, want := range cases {
		got, err := ValidateProvider(input)
		if err != nil {
			t.Fatalf("ValidateProvider(%q): %v", input, err)
		}
		if got != want {
			t.Fatalf("ValidateProvider(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestValidateProviderRejectsCursorAndUnknown(t *testing.T) {
	for _, input := range []string{"cursor", "", "claude", "chatgpt"} {
		if _, err := ValidateProvider(input); err == nil {
			t.Fatalf("ValidateProvider(%q) succeeded", input)
		} else if !strings.Contains(err.Error(), "Cursor is not supported") {
			t.Fatalf("ValidateProvider(%q) error = %v", input, err)
		}
	}
}
