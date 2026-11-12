package redact

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"devlog-learn/internal/ingest"
	"devlog-learn/internal/segment"
)

func mustRedactor(t *testing.T, cfg Config) *Redactor {
	t.Helper()
	r, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return r
}

func TestBuiltinRules(t *testing.T) {
	r := mustRedactor(t, DefaultConfig())
	hex64 := strings.Repeat("a1", 32)

	tests := []struct {
		name        string
		input       string
		contains    string
		notContains string
	}{
		{name: "pem", input: "-----BEGIN RSA PRIVATE KEY-----\nMIIE\n-----END RSA PRIVATE KEY-----", contains: "<REDACTED:pem-private-key>", notContains: "MIIE"},
		{name: "jwt", input: "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.abcdefghijklmnop", contains: "<REDACTED:jwt>"},
		{name: "openai key", input: "sk-abcdefghijklmnopqrstuvwx", contains: "<REDACTED:brand-token>"},
		{name: "anthropic key", input: "sk-ant-abcdefghijklmnopqrstuvwx", contains: "<REDACTED:brand-token>"},
		{name: "github token", input: "ghp_abcdefghijklmnopqrstuvwxyz012345", contains: "<REDACTED:brand-token>"},
		{name: "aws key", input: "AKIAABCDEFGHIJKLMNOP", contains: "<REDACTED:brand-token>"},
		{name: "bearer", input: "Bearer abcdefghijklmnop", contains: "<REDACTED:bearer-token>"},
		{name: "password assignment", input: "password=supersecret123", contains: "password=<REDACTED:credential>"},
		{name: "authorization header", input: "Authorization: Bearer abcdefghijklmnop", contains: "Authorization: <REDACTED:authorization>"},
		{name: "cookie header", input: "Cookie: session=abcdefghijklmnop", contains: "Cookie: <REDACTED:cookie>"},
		{name: "set-cookie header", input: "Set-Cookie: session=abcdefghijklmnop; Path=/", contains: "Set-Cookie: <REDACTED:set-cookie>", notContains: "abcdefghijklmnop"},
		{name: "url credentials", input: "https://user:pass@example.com/path", contains: "https://<REDACTED:url-credentials>@example.com/path"},
		{name: "email", input: "contact user.name+tag@example.co.uk now", contains: "<REDACTED:email>"},
		{name: "phone", input: "call 13800138000 now", contains: "<REDACTED:phone>"},
		{name: "public ip", input: "server 8.8.8.8 responded", contains: "<REDACTED:ip>"},
		{name: "long hex", input: "token " + hex64 + " end", contains: "<REDACTED:long-hex>"},
		{name: "windows home", input: `C:\Users\alice\project\file.go`, contains: `~\project\file.go`, notContains: "alice"},
		{name: "unix home", input: "/home/alice/project/file.go", contains: "~/project/file.go", notContains: "alice"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := r.Text(tt.input)
			if !strings.Contains(got, tt.contains) {
				t.Fatalf("input %q -> %q, want %q", tt.input, got, tt.contains)
			}
			if tt.notContains != "" && strings.Contains(got, tt.notContains) {
				t.Fatalf("input %q -> %q still contains %q", tt.input, got, tt.notContains)
			}
		})
	}
}

func TestFalsePositives(t *testing.T) {
	r := mustRedactor(t, DefaultConfig())
	gitSHA := "da39a3ee5e6b4b0d3255bfef95601890afd80709"
	sha256 := strings.Repeat("b2", 32)

	tests := []struct {
		name  string
		input string
	}{
		{name: "git sha", input: "commit " + gitSHA},
		{name: "uuid", input: "id 550e8400-e29b-41d4-a716-446655440000"},
		{name: "version with v", input: "v1.2.3.4"},
		{name: "version word", input: "version 1.2.3.4"},
		{name: "private ip 10", input: "host 10.0.0.1"},
		{name: "private ip 192", input: "host 192.168.1.10"},
		{name: "loopback", input: "host 127.0.0.1"},
		{name: "unspecified", input: "host 0.0.0.0"},
		{name: "sha256 context", input: "sha256: " + sha256},
		{name: "twelve digit phone-like", input: "order 138001380001"},
		{name: "password prose", input: "reset the password for this account"},
		{name: "token prose", input: "the token bucket is empty"},
		{name: "short hex", input: "id " + strings.Repeat("ab", 16)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := r.Text(tt.input); got != tt.input {
				t.Fatalf("unexpected redaction: %q -> %q", tt.input, got)
			}
		})
	}
}

func TestConfigOptions(t *testing.T) {
	t.Run("disable emails", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.DisableEmails = true
		r := mustRedactor(t, cfg)
		if got := r.Text("user@example.com"); got != "user@example.com" {
			t.Fatalf("email = %q", got)
		}
	})

	t.Run("disable phones", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.DisablePhones = true
		r := mustRedactor(t, cfg)
		if got := r.Text("call 13800138000"); got != "call 13800138000" {
			t.Fatalf("phone = %q", got)
		}
	})

	t.Run("disable ip", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.DisableIP = true
		r := mustRedactor(t, cfg)
		if got := r.Text("server 8.8.8.8"); got != "server 8.8.8.8" {
			t.Fatalf("ip = %q", got)
		}
	})

	t.Run("redact private ip", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.RedactPrivateIP = true
		r := mustRedactor(t, cfg)
		if got := r.Text("host 10.0.0.1"); !strings.Contains(got, "<REDACTED:ip>") {
			t.Fatalf("ip = %q", got)
		}
	})

	t.Run("disable by name", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.Disable = []string{"email"}
		r := mustRedactor(t, cfg)
		if got := r.Text("user@example.com"); got != "user@example.com" {
			t.Fatalf("email = %q", got)
		}
	})

	t.Run("custom rule", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.Rules = []RuleConfig{{Name: "internal", Pattern: `INTERNAL_[A-Z0-9]{8,}`}}
		r := mustRedactor(t, cfg)
		got := r.Text("value INTERNAL_ABCDEFGH")
		if !strings.Contains(got, "<REDACTED:internal>") {
			t.Fatalf("custom rule = %q", got)
		}
	})

	t.Run("hex threshold", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.HexMinLength = 32
		r := mustRedactor(t, cfg)
		sha := "da39a3ee5e6b4b0d3255bfef95601890afd80709"
		if got := r.Text("value " + sha); !strings.Contains(got, "<REDACTED:long-hex>") {
			t.Fatalf("hex = %q", got)
		}
		if got := r.Text("commit " + sha); got != "commit "+sha {
			t.Fatalf("commit context = %q", got)
		}
	})
}

func TestConfigValidation(t *testing.T) {
	if _, err := New(Config{Disable: []string{"missing-rule"}}); err == nil {
		t.Fatal("unknown disable name must fail")
	}
	if _, err := New(Config{Rules: []RuleConfig{{Name: "bad", Pattern: "("}}}); err == nil {
		t.Fatal("invalid custom regex must fail")
	}
	if _, err := New(Config{HexMinLength: 8}); err == nil {
		t.Fatal("too-small hex threshold must fail")
	}
}

func TestLoadConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "redact.json")
	body := `{
		"disable_emails": true,
		"hex_min_length": 32,
		"rules": [{"name": "internal", "pattern": "INTERNAL_[A-Z]{4,}"}]
	}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if !cfg.DisableEmails || cfg.HexMinLength != 32 || len(cfg.Rules) != 1 {
		t.Fatalf("config = %+v", cfg)
	}

	if _, err := LoadConfig(filepath.Join(dir, "missing.json")); err == nil {
		t.Fatal("missing config must fail")
	}
	if err := os.WriteFile(path, []byte("{"), 0o644); err != nil {
		t.Fatalf("write bad config: %v", err)
	}
	if _, err := LoadConfig(path); err == nil {
		t.Fatal("invalid JSON config must fail")
	}
}

func TestJSONRedaction(t *testing.T) {
	r := mustRedactor(t, DefaultConfig())
	input := []byte(`{"password":"supersecret123","n":1794318826123456789,"nested":["user@example.com"],"path":"C:\\Users\\alice\\x"}`)

	out := r.JSON(input)
	if !json.Valid(out) {
		t.Fatalf("output is not valid JSON: %s", out)
	}

	dec := json.NewDecoder(bytes.NewReader(out))
	dec.UseNumber()
	var value map[string]any
	if err := dec.Decode(&value); err != nil {
		t.Fatalf("decode output: %v", err)
	}
	if value["password"] != "<REDACTED:credential>" {
		t.Fatalf("password = %v", value["password"])
	}
	number, ok := value["n"].(json.Number)
	if !ok || number.String() != "1794318826123456789" {
		t.Fatalf("number = %v (%T)", value["n"], value["n"])
	}
	nested, ok := value["nested"].([]any)
	if !ok || len(nested) != 1 || nested[0] != "<REDACTED:email>" {
		t.Fatalf("nested = %v", value["nested"])
	}
	pathValue, _ := value["path"].(string)
	if strings.Contains(pathValue, "alice") || !strings.Contains(pathValue, "~") {
		t.Fatalf("path = %q", pathValue)
	}
}

func TestEventRedactionIsDeepCopy(t *testing.T) {
	r := mustRedactor(t, DefaultConfig())
	original := ingest.Event{
		Text:          "key sk-abcdefghijklmnopqrstuvwx",
		ToolInput:     json.RawMessage(`{"password":"supersecret123","n":1234567890123456789}`),
		ToolInputText: "contact user@example.com",
		ToolResult:    "call 13800138000",
		Exec:          &ingest.ExecResult{Stdout: "password=hunter2hunter2"},
		File:          `C:\Users\alice\project\session.jsonl`,
		Cwd:           `C:\Users\alice\project`,
	}
	originalInput := string(original.ToolInput)
	originalStdout := original.Exec.Stdout

	redacted, stats := r.Event(original)
	if !strings.Contains(redacted.Text, "<REDACTED:brand-token>") {
		t.Fatalf("text = %q", redacted.Text)
	}
	if !strings.Contains(redacted.ToolInputText, "<REDACTED:email>") {
		t.Fatalf("input text = %q", redacted.ToolInputText)
	}
	if !strings.Contains(redacted.ToolResult, "<REDACTED:phone>") {
		t.Fatalf("tool result = %q", redacted.ToolResult)
	}
	if !strings.Contains(redacted.Exec.Stdout, "<REDACTED:credential>") {
		t.Fatalf("exec stdout = %q", redacted.Exec.Stdout)
	}
	if strings.Contains(redacted.File, "alice") || !strings.Contains(redacted.File, "~") {
		t.Fatalf("file = %q", redacted.File)
	}
	if strings.Contains(redacted.Cwd, "alice") || !strings.Contains(redacted.Cwd, "~") {
		t.Fatalf("cwd = %q", redacted.Cwd)
	}
	if !json.Valid(redacted.ToolInput) || !strings.Contains(string(redacted.ToolInput), "<REDACTED:credential>") {
		t.Fatalf("tool input = %s", redacted.ToolInput)
	}
	if !strings.Contains(string(redacted.ToolInput), "1234567890123456789") {
		t.Fatalf("tool input lost number precision: %s", redacted.ToolInput)
	}

	if string(original.ToolInput) != originalInput || original.Exec.Stdout != originalStdout {
		t.Fatal("Event must not mutate its input")
	}
	if redacted.Exec == original.Exec {
		t.Fatal("Exec must be copied")
	}
	if stats["brand-token"] != 1 || stats["email"] != 1 || stats["phone-cn"] != 1 {
		t.Fatalf("stats = %+v", stats)
	}
}

func TestSegmentRedaction(t *testing.T) {
	r := mustRedactor(t, DefaultConfig())
	seg := segment.Segment{Events: []ingest.Event{
		{Kind: ingest.KindUserText, Text: "key sk-abcdefghijklmnopqrstuvwx"},
		{Kind: ingest.KindToolResult, ToolResult: "user@example.com"},
	}}

	out, stats := r.Segment(seg)
	if !strings.Contains(out.Events[0].Text, "<REDACTED:brand-token>") {
		t.Fatalf("text = %q", out.Events[0].Text)
	}
	if !strings.Contains(out.Events[1].ToolResult, "<REDACTED:email>") {
		t.Fatalf("result = %q", out.Events[1].ToolResult)
	}
	if strings.Contains(seg.Events[0].Text, "REDACTED") || strings.Contains(seg.Events[1].ToolResult, "REDACTED") {
		t.Fatal("Segment must not mutate its input")
	}
	if stats["brand-token"] != 1 || stats["email"] != 1 {
		t.Fatalf("stats = %+v", stats)
	}
	if out.Hash != "" {
		t.Fatal("redact.Segment must leave hash computation to the caller")
	}
}

func TestSegmentHashChangesWithRedactionRules(t *testing.T) {
	seg := segment.Segment{Events: []ingest.Event{
		{Kind: ingest.KindUserText, Text: "value INTERNAL_ABCDEFGH"},
	}}

	plain := mustRedactor(t, DefaultConfig())
	outPlain, _ := plain.Segment(seg)
	segment.SetHash(&outPlain)

	cfg := DefaultConfig()
	cfg.Rules = []RuleConfig{{Name: "internal", Pattern: `INTERNAL_[A-Z0-9]{8,}`}}
	custom := mustRedactor(t, cfg)
	outCustom, _ := custom.Segment(seg)
	segment.SetHash(&outCustom)

	if outPlain.Hash == outCustom.Hash {
		t.Fatalf("hash must change after redaction: %q", outPlain.Hash)
	}
	if strings.Contains(outCustom.Events[0].Text, "INTERNAL_") {
		t.Fatalf("custom rule did not redact: %q", outCustom.Events[0].Text)
	}
}

func TestIdempotent(t *testing.T) {
	r := mustRedactor(t, DefaultConfig())
	input := "password=supersecret123 sk-abcdefghijklmnopqrstuvwx user@example.com"
	once := r.Text(input)
	twice := r.Text(once)
	if once != twice {
		t.Fatalf("not idempotent:\n%q\n%q", once, twice)
	}
}

func TestDetect(t *testing.T) {
	r := mustRedactor(t, DefaultConfig())
	stats := r.Detect("sk-abcdefghijklmnopqrstuvwx user@example.com")
	if stats["brand-token"] != 1 || stats["email"] != 1 {
		t.Fatalf("detect = %+v", stats)
	}
}

func TestWalkStrings(t *testing.T) {
	type payload struct {
		Title   string
		Items   []string
		When    time.Time
		private string
	}
	p := &payload{
		Title:   "key sk-abcdefghijklmnopqrstuvwx",
		Items:   []string{"user@example.com"},
		When:    time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		private: "untouched",
	}
	r := mustRedactor(t, DefaultConfig())
	r.WalkStrings(p, r.Text)

	if strings.Contains(p.Title, "sk-") {
		t.Fatalf("title = %q", p.Title)
	}
	if strings.Contains(p.Items[0], "@") {
		t.Fatalf("item = %q", p.Items[0])
	}
	if p.private != "untouched" {
		t.Fatalf("unexported field changed: %q", p.private)
	}
	if p.When.IsZero() {
		t.Fatal("time value was damaged")
	}
}

func TestHashChangesWithConfig(t *testing.T) {
	a := mustRedactor(t, DefaultConfig()).Hash()
	b := mustRedactor(t, DefaultConfig()).Hash()
	if a != b {
		t.Fatalf("hash is not stable: %q vs %q", a, b)
	}

	cfg := DefaultConfig()
	cfg.DisableEmails = true
	if c := mustRedactor(t, cfg).Hash(); c == a {
		t.Fatal("hash must change when rules change")
	}

	cfg = DefaultConfig()
	cfg.Rules = []RuleConfig{{Name: "internal", Pattern: `INTERNAL_[A-Z]{4,}`}}
	if c := mustRedactor(t, cfg).Hash(); c == a {
		t.Fatal("hash must change for custom rules")
	}
}
