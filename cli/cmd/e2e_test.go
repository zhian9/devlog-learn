package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"devlog-learn/internal/cache"
	"devlog-learn/internal/extract"
	"devlog-learn/internal/index"
	"devlog-learn/internal/render"
)

// e2eDocResponse is intentionally one line: fake CLIs echo it verbatim.
const e2eDocResponse = `{"title":"Fix build","summary":"Build failure resolved.","knowledge_type":"bug_fix","status":"unknown","tags":["build","go"],"stack":["go"],"confidence":0.8}`

func writeClaudeE2EFixture(t *testing.T, root string) {
	t.Helper()
	dir := filepath.Join(root, "demo-project")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir claude fixture: %v", err)
	}
	lines := []string{
		`{"type":"user","timestamp":"2026-05-01T09:00:00Z","sessionId":"claude-e2e","cwd":"D:\\work\\demo","message":{"role":"user","content":"Build fails with error X"}}`,
		`{"type":"assistant","timestamp":"2026-05-01T09:00:10Z","sessionId":"claude-e2e","cwd":"D:\\work\\demo","message":{"role":"assistant","content":[{"type":"text","text":"Fixed the import."}]}}`,
	}
	if err := os.WriteFile(filepath.Join(dir, "session.jsonl"), []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("write claude fixture: %v", err)
	}
}

func writeCodexE2EFixture(t *testing.T, root string) {
	t.Helper()
	dir := filepath.Join(root, "2026", "05", "01")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir codex fixture: %v", err)
	}
	name := "rollout-2026-05-01T09-00-00-33333333-3333-3333-3333-333333333333.jsonl"
	lines := []string{
		`{"timestamp":"2026-05-01T09:00:00Z","type":"session_meta","payload":{"session_id":"codex-e2e","id":"codex-e2e","cwd":"D:\\work\\codex-e2e","originator":"Codex Desktop"}}`,
		`{"timestamp":"2026-05-01T09:00:02Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"Build fails with error Y"}]}}`,
		`{"timestamp":"2026-05-01T09:00:03Z","type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"Fixed the configuration."}]}}`,
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("write codex fixture: %v", err)
	}
}

type e2ePaths struct {
	root      string
	claudeDir string
	codexDir  string
	cacheDir  string
	cursor    string
	docOut    string
	renderOut string
	claudeBin string
	codexBin  string
}

func newE2EPaths(t *testing.T) e2ePaths {
	t.Helper()
	root := t.TempDir()
	p := e2ePaths{
		root:      root,
		claudeDir: filepath.Join(root, "claude"),
		codexDir:  filepath.Join(root, "codex"),
		cacheDir:  filepath.Join(root, "cache"),
		cursor:    filepath.Join(root, "cursor.json"),
		docOut:    filepath.Join(root, "doc-json"),
		renderOut: filepath.Join(root, "rendered"),
	}
	writeClaudeE2EFixture(t, p.claudeDir)
	writeCodexE2EFixture(t, p.codexDir)
	binDir := filepath.Join(root, "bin")
	p.claudeBin = writeFakeCLI(t, binDir, "fake-claude", "fake-claude 1.0", e2eDocResponse)
	p.codexBin = writeFakeCLI(t, binDir, "fake-codex", "fake-codex 1.0", e2eDocResponse)
	return p
}

func (p e2ePaths) extractArgs() []string {
	return []string{
		"extract",
		"--provider", "all",
		"--claude-dir", p.claudeDir,
		"--codex-dir", p.codexDir,
		"--since", "all",
		"--segments", "0",
		"--claude-bin", p.claudeBin,
		"--codex-bin", p.codexBin,
		"--out", p.docOut,
		"--cache-dir", p.cacheDir,
		"--cursor", p.cursor,
		"--timeout", "30s",
	}
}

// TestCommandEndToEndExtractRenderIndexReviewRelated walks the complete
// pipeline with two providers and fake CLIs: extraction, Doc output, Markdown
// rendering, review editing, related lookup, index generation, and incremental
// re-run behavior.
func TestCommandEndToEndExtractRenderIndexReviewRelated(t *testing.T) {
	p := newE2EPaths(t)

	stdout, stderr, err := runCommand(p.extractArgs()...)
	if err != nil {
		t.Fatalf("extract: %v\nstderr:\n%s", err, stderr)
	}
	if !strings.Contains(stderr, "extracted=2") || !strings.Contains(stderr, "failed_segments=0") {
		t.Fatalf("extract summary =\n%s", stderr)
	}
	entries, err := os.ReadDir(p.docOut)
	if err != nil || len(entries) != 2 {
		t.Fatalf("doc output dir = %v, err = %v", entries, err)
	}
	if len(strings.Fields(stdout)) != 2 {
		t.Fatalf("extract stdout paths = %q", stdout)
	}

	store, err := cache.Open(p.cacheDir)
	if err != nil {
		t.Fatalf("cache.Open: %v", err)
	}
	infos, warnings := store.ListActiveDocs()
	if len(warnings) != 0 || len(infos) != 2 {
		t.Fatalf("active docs = %d, warnings = %v", len(infos), warnings)
	}
	bySource := map[string]*extract.Doc{}
	docs := make([]*extract.Doc, 0, len(infos))
	for _, info := range infos {
		bySource[info.Doc.Source] = info.Doc
		docs = append(docs, info.Doc)
	}
	claudeDoc, codexDoc := bySource["claude-code"], bySource["codex"]
	if claudeDoc == nil || codexDoc == nil {
		t.Fatalf("missing provider docs: %v", bySource)
	}
	if claudeDoc.Provenance.Provider != "claude-code" || claudeDoc.Provenance.CLIVersion != "fake-claude 1.0" {
		t.Fatalf("claude provenance = %+v", claudeDoc.Provenance)
	}
	if codexDoc.Provenance.Provider != "codex" || codexDoc.Provenance.CLIVersion != "fake-codex 1.0" {
		t.Fatalf("codex provenance = %+v", codexDoc.Provenance)
	}
	if claudeDoc.Project != "demo" || codexDoc.Project != "codex-e2e" {
		t.Fatalf("projects = %q / %q", claudeDoc.Project, codexDoc.Project)
	}

	// render: <out>/<project>/<date>-<slug>.md for both docs.
	if _, stderr, err = runCommand("render", "--cache-dir", p.cacheDir, "--out", p.renderOut); err != nil {
		t.Fatalf("render: %v\nstderr:\n%s", err, stderr)
	}
	planByID := map[string]string{}
	for _, plan := range render.PlanDocs(docs) {
		planByID[plan.Doc.ID] = plan.Path
		path := filepath.Join(p.renderOut, filepath.FromSlash(plan.Path))
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatalf("rendered markdown %s: %v", path, readErr)
		}
		if !strings.Contains(string(data), "# Fix build") {
			t.Fatalf("markdown %s lacks title:\n%s", path, data)
		}
	}
	if planByID[claudeDoc.ID] != render.DocPath(claudeDoc) {
		t.Fatalf("claude path = %q, want %q", planByID[claudeDoc.ID], render.DocPath(claudeDoc))
	}
	if !strings.HasPrefix(planByID[claudeDoc.ID], "demo/") || !strings.HasSuffix(planByID[claudeDoc.ID], "-fix-build.md") {
		t.Fatalf("claude path = %q", planByID[claudeDoc.ID])
	}

	// review: update one doc, then verify render and index see the new state.
	if _, stderr, err = runCommand("review", claudeDoc.ID, "--cache-dir", p.cacheDir, "--status", "approved", "--note", "verified e2e"); err != nil {
		t.Fatalf("review: %v\nstderr:\n%s", err, stderr)
	}
	if _, stderr, err = runCommand("render", "--cache-dir", p.cacheDir, "--out", p.renderOut); err != nil {
		t.Fatalf("render after review: %v\nstderr:\n%s", err, stderr)
	}
	mdPath := filepath.Join(p.renderOut, filepath.FromSlash(planByID[claudeDoc.ID]))
	data, err := os.ReadFile(mdPath)
	if err != nil {
		t.Fatalf("read reviewed markdown: %v", err)
	}
	for _, want := range []string{`review_status: "approved"`, `review_note: "verified e2e"`} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("markdown missing %q:\n%s", want, data)
		}
	}

	// related: both docs share tags/stack/knowledge_type, so one must find the other.
	stdout, _, err = runCommand("related", claudeDoc.ID, "--cache-dir", p.cacheDir, "--json")
	if err != nil {
		t.Fatalf("related: %v", err)
	}
	var related []index.Related
	if err := json.Unmarshal([]byte(stdout), &related); err != nil {
		t.Fatalf("related JSON: %v\n%s", err, stdout)
	}
	if len(related) != 1 || related[0].ID != codexDoc.ID {
		t.Fatalf("related = %+v", related)
	}

	// index: every entry path must resolve to the Markdown rendered above.
	stdout, stderr, err = runCommand("index", "--cache-dir", p.cacheDir, "--out", p.renderOut)
	if err != nil {
		t.Fatalf("index: %v\nstderr:\n%s", err, stderr)
	}
	indexJSON := filepath.Join(p.renderOut, "index.json")
	raw, err := os.ReadFile(indexJSON)
	if err != nil {
		t.Fatalf("read index.json: %v", err)
	}
	var global index.Document
	if err := json.Unmarshal(raw, &global); err != nil {
		t.Fatalf("decode index.json: %v", err)
	}
	if global.Total != 2 || len(global.Docs) != 2 {
		t.Fatalf("index total/docs = %d/%d", global.Total, len(global.Docs))
	}
	var approvedSeen bool
	for _, entry := range global.Docs {
		if _, statErr := os.Stat(filepath.Join(p.renderOut, filepath.FromSlash(entry.Path))); statErr != nil {
			t.Fatalf("index path %q does not resolve: %v", entry.Path, statErr)
		}
		if entry.ID == claudeDoc.ID {
			approvedSeen = entry.ReviewStatus == extract.ReviewApproved
		}
	}
	if !approvedSeen {
		t.Fatalf("index did not reflect approved review: %+v", global.Docs)
	}
	if !strings.Contains(stdout, indexJSON) {
		t.Fatalf("index stdout does not list index.json: %q", stdout)
	}

	// A second incremental extract must skip both files and call no model.
	_, stderr, err = runCommand(p.extractArgs()...)
	if err != nil {
		t.Fatalf("second extract: %v\nstderr:\n%s", err, stderr)
	}
	if !strings.Contains(stderr, "skipped=2") || !strings.Contains(stderr, "selected=0") {
		t.Fatalf("incremental summary =\n%s", stderr)
	}
}

func TestCommandExtractSurvivesOneProviderFailure(t *testing.T) {
	p := newE2EPaths(t)
	p.codexBin = filepath.Join(p.root, "missing-codex")

	_, stderr, err := runCommand(p.extractArgs()...)
	if err == nil {
		t.Fatal("expected the codex provider failure to be reported")
	}
	if !strings.Contains(err.Error(), "codex") {
		t.Fatalf("error = %v", err)
	}
	if !strings.Contains(stderr, "extracted=1") {
		t.Fatalf("claude should still be extracted:\n%s", stderr)
	}

	store, openErr := cache.Open(p.cacheDir)
	if openErr != nil {
		t.Fatalf("cache.Open: %v", openErr)
	}
	infos, _ := store.ListActiveDocs()
	if len(infos) != 1 || infos[0].Doc.Source != "claude-code" {
		t.Fatalf("active docs = %+v", infos)
	}
}

func TestCommandExtractRepairsInvalidJSON(t *testing.T) {
	p := newE2EPaths(t)
	binDir := filepath.Join(p.root, "repair-bin")
	p.claudeBin = writeSequenceFakeCLI(t, binDir, "repair-claude", "repair-claude 1.0", "not json", e2eDocResponse)

	args := []string{
		"extract",
		"--provider", "claude-code",
		"--claude-dir", p.claudeDir,
		"--since", "all",
		"--segments", "0",
		"--claude-bin", p.claudeBin,
		"--out", p.docOut,
		"--cache-dir", p.cacheDir,
		"--cursor", p.cursor,
		"--timeout", "30s",
	}
	_, stderr, err := runCommand(args...)
	if err != nil {
		t.Fatalf("extract: %v\nstderr:\n%s", err, stderr)
	}
	if !strings.Contains(stderr, "repairs=1") {
		t.Fatalf("repair was not reported:\n%s", stderr)
	}
	store, openErr := cache.Open(p.cacheDir)
	if openErr != nil {
		t.Fatalf("cache.Open: %v", openErr)
	}
	infos, warnings := store.ListActiveDocs()
	if len(warnings) != 0 || len(infos) != 1 {
		t.Fatalf("active docs = %d, warnings = %v", len(infos), warnings)
	}
	if infos[0].Doc.Title != "Fix build" {
		t.Fatalf("doc title = %q", infos[0].Doc.Title)
	}
}
