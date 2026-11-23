package experience

import (
	"context"
	"strings"
	"testing"

	"devlog-learn/internal/extract"
)

type fakeSynthClient struct {
	text  string
	err   error
	calls []extract.Request
	model string
	usage extract.Usage
}

func (f *fakeSynthClient) Name() string                            { return "fake" }
func (f *fakeSynthClient) Version(context.Context) (string, error) { return "fake 1.0", nil }
func (f *fakeSynthClient) Capabilities(context.Context) (map[string]bool, error) {
	return map[string]bool{}, nil
}
func (f *fakeSynthClient) Model() string { return f.model }
func (f *fakeSynthClient) Complete(_ context.Context, req extract.Request) (extract.Response, error) {
	f.calls = append(f.calls, req)
	if f.err != nil {
		return extract.Response{}, f.err
	}
	return extract.Response{Text: f.text, Usage: f.usage}, nil
}

func TestBuildPromptContainsDocIDsButNotEvidence(t *testing.T) {
	doc := testDoc("d1", "demo", "Fix cache", testTime)
	doc.Evidence = []extract.Evidence{{EventSeq: 1, Kind: "error_output", Quote: "SECRET-RAW-EVIDENCE"}}
	exp := &Experience{Project: "demo", Kind: KindProject, KindLabel: "项目经历", Period: "2025-08", Stack: []string{"Go"}}

	prompt, err := BuildPrompt(exp, []*extract.Doc{doc})
	if err != nil {
		t.Fatalf("BuildPrompt: %v", err)
	}
	if !strings.Contains(prompt, "d1") || !strings.Contains(prompt, "Fix cache") {
		t.Fatalf("prompt missing doc details: %s", prompt)
	}
	if strings.Contains(prompt, "SECRET-RAW-EVIDENCE") {
		t.Fatal("prompt leaked raw evidence quote")
	}
}

func TestSynthesizeDropsUnknownDocIDs(t *testing.T) {
	doc := testDoc("d1", "demo", "Fix cache", testTime)
	exp := &Experience{Project: "demo", Kind: KindProject, KindLabel: "项目经历", Stack: []string{"Go"}}
	client := &fakeSynthClient{text: `{
		"summary": "did work",
		"background": "background",
		"contributions": [
			{"text": "valid", "doc_ids": ["d1", "unknown"]},
			{"text": "no valid ids", "doc_ids": ["unknown"]}
		],
		"challenges": [{"text": "only unknown", "doc_ids": ["unknown"]}],
		"outcomes": [],
		"stories": []
	}`, model: "fake-model", usage: extract.Usage{Known: true, InputTokens: 10, OutputTokens: 5}}

	synthesis, err := Synthesize(context.Background(), client, "claude-code", "fake-model", exp, []*extract.Doc{doc}, testTime)
	if err != nil {
		t.Fatalf("Synthesize: %v", err)
	}
	if len(synthesis.Contributions) != 1 || synthesis.Contributions[0].Text != "valid" {
		t.Fatalf("contributions = %+v", synthesis.Contributions)
	}
	if len(synthesis.Contributions[0].DocIDs) != 1 || synthesis.Contributions[0].DocIDs[0] != "d1" {
		t.Fatalf("doc ids = %+v", synthesis.Contributions[0].DocIDs)
	}
	if len(synthesis.Challenges) != 0 {
		t.Fatalf("challenges = %+v", synthesis.Challenges)
	}
	if len(synthesis.DocIDs) != 1 || synthesis.DocIDs[0] != "d1" {
		t.Fatalf("doc ids = %+v", synthesis.DocIDs)
	}
	if !synthesis.Unverified {
		t.Fatal("synthesis is not marked unverified")
	}
	if synthesis.Usage == nil || synthesis.Usage.InputTokens != 10 || synthesis.Usage.OutputTokens != 5 {
		t.Fatalf("usage = %+v", synthesis.Usage)
	}
	if len(client.calls) != 1 {
		t.Fatalf("calls = %d", len(client.calls))
	}
}

func TestSynthesizeRejectsGarbage(t *testing.T) {
	doc := testDoc("d1", "demo", "Fix cache", testTime)
	exp := &Experience{Project: "demo", Kind: KindProject, KindLabel: "项目经历"}
	client := &fakeSynthClient{text: "not json at all"}
	if _, err := Synthesize(context.Background(), client, "claude-code", "", exp, []*extract.Doc{doc}, testTime); err == nil {
		t.Fatal("garbage output was accepted")
	}
}

func TestSynthesisCacheRoundTripAndInvalidation(t *testing.T) {
	doc := testDoc("d1", "demo", "Fix cache", testTime)
	exp := &Experience{Project: "demo", Kind: KindProject, KindLabel: "项目经历"}
	client := &fakeSynthClient{text: `{
		"summary": "did work",
		"contributions": [{"text": "valid", "doc_ids": ["d1"]}]
	}`, model: "fake-model"}

	synthesis, err := Synthesize(context.Background(), client, "claude-code", "fake-model", exp, []*extract.Doc{doc}, testTime)
	if err != nil {
		t.Fatalf("Synthesize: %v", err)
	}
	cacheDir := t.TempDir()
	if err := SaveCachedSynthesis(cacheDir, synthesis); err != nil {
		t.Fatalf("SaveCachedSynthesis: %v", err)
	}
	loaded, ok := LoadCachedSynthesis(cacheDir, exp, []*extract.Doc{doc})
	if !ok || loaded.Summary != "did work" {
		t.Fatalf("LoadCachedSynthesis = %+v, %v", loaded, ok)
	}

	changed := *doc
	changed.Summary = "different summary"
	if _, ok := LoadCachedSynthesis(cacheDir, exp, []*extract.Doc{&changed}); ok {
		t.Fatal("cache was reused after input changed")
	}

	exps := []Experience{*exp}
	AttachCachedSynthesis(exps, map[string][]*extract.Doc{"demo": {doc}}, cacheDir)
	if exps[0].Synthesized == nil {
		t.Fatal("AttachCachedSynthesis did not attach")
	}
}
