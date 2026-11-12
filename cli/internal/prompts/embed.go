// Package prompts embeds the extraction and JSON-repair templates.
package prompts

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"text/template"
)

//go:embed *.tmpl
var FS embed.FS

// PromptVersion changes when prompt semantics change.
const PromptVersion = "extract-v1"

var promptHash string

func init() {
	entries, err := FS.ReadDir(".")
	if err != nil {
		panic(fmt.Sprintf("prompts: read embedded directory: %v", err))
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".tmpl") {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)

	h := sha256.New()
	h.Write([]byte(PromptVersion))
	for _, name := range names {
		data, err := FS.ReadFile(name)
		if err != nil {
			panic(fmt.Sprintf("prompts: read %s: %v", name, err))
		}
		h.Write([]byte{0})
		h.Write([]byte(name))
		h.Write([]byte{0})
		h.Write(data)
	}
	promptHash = hex.EncodeToString(h.Sum(nil))
}

// Hash identifies the embedded prompt set and PromptVersion.
func Hash() string { return promptHash }

// Render executes one embedded template with strict missing-key checking.
func Render(name string, data any) (string, error) {
	raw, err := FS.ReadFile(name)
	if err != nil {
		return "", fmt.Errorf("read prompt template %q: %w", name, err)
	}
	tmpl, err := template.New(name).Option("missingkey=error").Parse(string(raw))
	if err != nil {
		return "", fmt.Errorf("parse prompt template %q: %w", name, err)
	}
	var b strings.Builder
	if err := tmpl.Execute(&b, data); err != nil {
		return "", fmt.Errorf("execute prompt template %q: %w", name, err)
	}
	return b.String(), nil
}
