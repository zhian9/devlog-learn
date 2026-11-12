package main

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	"devlog-learn/internal/extract"
)

func TestMCPServerHandshakeAndTools(t *testing.T) {
	approved := commandDoc("alpha", "demo", "Fix cache eviction", outputTime, "cache", "go")
	approved.ReviewStatus = extract.ReviewApproved
	pending := commandDoc("beta", "demo", "Session selection", outputTime.Add(-time.Hour), "cli", "go")
	stale := commandDoc("old", "demo", "Old cache note", outputTime.Add(-2*time.Hour), "cache")
	stale.Lifecycle = extract.LifecycleStale
	cacheDir := seedCache(t, approved, pending, stale)

	server := &mcpServer{
		cacheDir: cacheDir,
		outDir:   t.TempDir(),
		review:   "any",
		limit:    5,
		log:      io.Discard,
	}
	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"search_docs","arguments":{"query":"cache"}}}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"get_doc","arguments":{"id":"alpha","format":"markdown"}}}`,
		`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"related_docs","arguments":{"id":"alpha"}}}`,
		`{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"list_projects"}}`,
		`{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"missing_tool"}}`,
		`{"jsonrpc":"2.0","id":8,"method":"unknown/method"}`,
		`not json`,
	}, "\n")

	var output bytes.Buffer
	if err := serveMCP(strings.NewReader(input), &output, server); err != nil {
		t.Fatalf("serveMCP: %v", err)
	}

	responses, parseErrors := decodeMCPLines(t, output.String())
	if len(parseErrors) != 1 || parseErrors[0].Code != -32700 {
		t.Fatalf("parse errors = %+v", parseErrors)
	}
	if len(responses) != 8 {
		t.Fatalf("responses = %d: %s", len(responses), output.String())
	}

	initialize := responses[1]
	result := initialize["result"].(map[string]any)
	if result["protocolVersion"] != "2025-06-18" {
		t.Fatalf("protocolVersion = %v", result["protocolVersion"])
	}
	if result["serverInfo"].(map[string]any)["name"] != "devlog-learn" {
		t.Fatalf("serverInfo = %+v", result["serverInfo"])
	}

	tools := responses[2]["result"].(map[string]any)["tools"].([]any)
	if len(tools) != 4 {
		t.Fatalf("tools = %+v", tools)
	}

	searchText := toolText(t, responses[3])
	var searchPayload struct {
		Count   int `json:"count"`
		Results []struct {
			ID string `json:"id"`
		} `json:"results"`
	}
	if err := json.Unmarshal([]byte(searchText), &searchPayload); err != nil {
		t.Fatalf("search payload: %v (%s)", err, searchText)
	}
	if searchPayload.Count != 1 || searchPayload.Results[0].ID != "alpha" {
		t.Fatalf("search payload = %s", searchText)
	}
	if strings.Contains(searchText, `"old"`) {
		t.Fatalf("stale doc leaked into search: %s", searchText)
	}

	markdown := toolText(t, responses[4])
	if !strings.Contains(markdown, "# Fix cache eviction") || !strings.Contains(markdown, "review_status") {
		t.Fatalf("markdown = %s", markdown)
	}

	relatedText := toolText(t, responses[5])
	if !strings.Contains(relatedText, `"beta"`) {
		t.Fatalf("related = %s", relatedText)
	}

	projectsText := toolText(t, responses[6])
	if !strings.Contains(projectsText, `"demo"`) || !strings.Contains(projectsText, `"docs":2`) {
		t.Fatalf("projects = %s", projectsText)
	}

	if responses[7]["result"].(map[string]any)["isError"] != true {
		t.Fatalf("unknown tool should be an error: %+v", responses[7])
	}
	if code := errorCode(responses[8]); code != -32601 {
		t.Fatalf("unknown method code = %d", code)
	}
}

func TestMCPServerGetDocErrorsAndFilters(t *testing.T) {
	approved := commandDoc("alpha", "Demo Project", "Fix cache eviction", outputTime, "cache")
	approved.ReviewStatus = extract.ReviewApproved
	pending := commandDoc("beta", "Demo Project", "Session selection", outputTime, "cli")
	stale := commandDoc("old", "Demo Project", "Old cache note", outputTime, "cache")
	stale.Lifecycle = extract.LifecycleStale
	cacheDir := seedCache(t, approved, pending, stale)

	server := &mcpServer{
		cacheDir: cacheDir,
		outDir:   t.TempDir(),
		project:  "Demo-Project", // safe-name match
		review:   "approved",
		limit:    5,
		log:      io.Discard,
	}
	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"search_docs","arguments":{}}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"get_doc","arguments":{"id":"old"}}}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"get_doc","arguments":{"id":"missing"}}}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"search_docs","arguments":{"review":"bogus"}}}`,
	}, "\n")

	var output bytes.Buffer
	if err := serveMCP(strings.NewReader(input), &output, server); err != nil {
		t.Fatalf("serveMCP: %v", err)
	}
	responses, _ := decodeMCPLines(t, output.String())
	if len(responses) != 4 {
		t.Fatalf("responses = %d: %s", len(responses), output.String())
	}

	searchText := toolText(t, responses[1])
	if !strings.Contains(searchText, `"alpha"`) || strings.Contains(searchText, `"beta"`) {
		t.Fatalf("review/project filter = %s", searchText)
	}
	for _, id := range []int64{2, 3, 4} {
		if responses[id]["result"].(map[string]any)["isError"] != true {
			t.Fatalf("response %d should be a tool error: %+v", id, responses[id])
		}
	}
}

func TestResolveAgentProjectAndReviewFilter(t *testing.T) {
	if got := resolveAgentProject(""); got != "" {
		t.Fatalf("empty project = %q", got)
	}
	if got := resolveAgentProject("all"); got != "" {
		t.Fatalf("all project = %q", got)
	}
	if got := resolveAgentProject("auto"); got == "." {
		t.Fatalf("auto project = %q", got)
	}
	if got := resolveAgentProject("demo"); got != "demo" {
		t.Fatalf("explicit project = %q", got)
	}
	for _, value := range []string{"", "any", "APPROVED", "pending", "rejected"} {
		if _, err := normalizeReviewFilter(value); err != nil {
			t.Fatalf("review %q: %v", value, err)
		}
	}
	if _, err := normalizeReviewFilter("bogus"); err == nil {
		t.Fatal("expected invalid review filter to fail")
	}
}

func toolText(t *testing.T, response map[string]any) string {
	t.Helper()
	result := response["result"].(map[string]any)
	content := result["content"].([]any)
	if len(content) != 1 {
		t.Fatalf("content = %+v", content)
	}
	return content[0].(map[string]any)["text"].(string)
}

func errorCode(response map[string]any) int {
	errBody, ok := response["error"].(map[string]any)
	if !ok {
		return 0
	}
	code, _ := errBody["code"].(float64)
	return int(code)
}

// decodeMCPLines parses newline-delimited responses. Requests with an id are
// keyed by id; parse errors use id null and are returned separately.
func decodeMCPLines(t *testing.T, output string) (map[int64]map[string]any, []struct{ Code int }) {
	t.Helper()
	responses := map[int64]map[string]any{}
	var parseErrors []struct{ Code int }
	for line := range strings.SplitSeq(strings.TrimSpace(output), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var payload map[string]any
		if err := json.Unmarshal([]byte(line), &payload); err != nil {
			t.Fatalf("invalid response line %q: %v", line, err)
		}
		idValue, ok := payload["id"].(float64)
		if !ok {
			if errBody, ok := payload["error"].(map[string]any); ok {
				code, _ := errBody["code"].(float64)
				parseErrors = append(parseErrors, struct{ Code int }{Code: int(code)})
			}
			continue
		}
		responses[int64(idValue)] = payload
	}
	return responses, parseErrors
}
