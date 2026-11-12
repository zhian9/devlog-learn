package webapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"devlog-learn/internal/cache"
	"devlog-learn/internal/extract"
	"devlog-learn/internal/ingest"
	"devlog-learn/internal/segment"
)

func TestRequiresTokenAndLoopback(t *testing.T) {
	server, _, _ := newTestServer(t)

	request := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	request.Host = "127.0.0.1:8787"
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("no token status = %d, want 401", response.Code)
	}

	request = httptest.NewRequest(http.MethodGet, "/api/status", nil)
	request.Host = "evil.example"
	request.Header.Set("X-Devlog-Token", "test-token")
	response = httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("bad host status = %d, want 403", response.Code)
	}

	request = httptest.NewRequest(http.MethodPost, "/api/export", strings.NewReader(`{}`))
	request.Host = "127.0.0.1:8787"
	request.Header.Set("X-Devlog-Token", "test-token")
	request.Header.Set("Origin", "https://evil.example")
	response = httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("cross-origin write status = %d, want 403", response.Code)
	}
}

func TestServeStopsOnContextCancel(t *testing.T) {
	server, _, _ := newTestServer(t)
	listener, err := server.Listen()
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Serve(ctx, listener)
	}()
	cancel()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Serve: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Serve did not stop after context cancellation")
	}
}

func TestTokenExchangeAndStatic(t *testing.T) {
	server, _, _ := newTestServer(t)

	request := httptest.NewRequest(http.MethodGet, "/?token=test-token", nil)
	request.Host = "127.0.0.1:8787"
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusFound {
		t.Fatalf("token exchange status = %d, want 302", response.Code)
	}
	if !strings.Contains(response.Header().Get("Set-Cookie"), "devlog_token=test-token") {
		t.Fatalf("missing auth cookie: %q", response.Header().Get("Set-Cookie"))
	}

	request = httptest.NewRequest(http.MethodGet, "/library", nil)
	request.Host = "127.0.0.1:8787"
	response = httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("SPA route status = %d, want 200", response.Code)
	}
	if !strings.Contains(response.Body.String(), `id="root"`) {
		t.Fatalf("SPA fallback did not serve index.html")
	}
}

func TestDocsListDetailAndEvents(t *testing.T) {
	server, cacheDir, docID := newTestServer(t)

	response := get(t, server, "/api/docs")
	var list DocListResponse
	decode(t, response, &list)
	if list.Total != 1 || len(list.Docs) != 1 {
		t.Fatalf("active docs = %d/%d, want 1/1", list.Total, len(list.Docs))
	}
	if list.Docs[0].Lifecycle != extract.LifecycleActive {
		t.Fatalf("lifecycle = %q", list.Docs[0].Lifecycle)
	}

	response = get(t, server, "/api/docs?lifecycle=all")
	decode(t, response, &list)
	if list.Total != 2 {
		t.Fatalf("all docs = %d, want 2", list.Total)
	}

	response = get(t, server, "/api/docs/"+docID)
	var detail DocDetailResponse
	decode(t, response, &detail)
	if detail.Doc.ID != docID || detail.MarkdownPath == "" || detail.LearnMarkdownPath == "" {
		t.Fatalf("unexpected detail: %+v", detail)
	}
	if len(detail.Related) != 0 {
		t.Fatalf("related = %d, want 0 for unrelated fixture", len(detail.Related))
	}

	response = get(t, server, "/api/docs/"+docID+"/events?seq=2")
	var events EventsResponse
	decode(t, response, &events)
	if len(events.Events) != 1 || events.Events[0].Seq != 2 {
		t.Fatalf("events = %+v, want only seq 2", events.Events)
	}

	response = get(t, server, "/api/docs/"+docID+"/markdown?profile=learning")
	if response.Code != http.StatusOK || response.Body.Len() == 0 {
		t.Fatalf("learning markdown status=%d body=%q", response.Code, response.Body.String())
	}
	if !strings.HasPrefix(response.Body.String(), "---") {
		t.Fatalf("learning markdown missing front matter: %q", response.Body.String()[:min(80, response.Body.Len())])
	}

	// The stale document must be readable by id but absent from the default list.
	stalePath := filepath.Join(cacheDir, "docs", "bbbbbbbbbbbbbbbb.json")
	if _, err := os.Stat(stalePath); err != nil {
		t.Fatalf("stale fixture missing: %v", err)
	}
}

func TestReviewAndQuestions(t *testing.T) {
	server, cacheDir, docID := newTestServer(t)

	response := post(t, server, "/api/docs/"+docID+"/review", `{"status":"approved","note":"verified"}`)
	var doc extract.Doc
	decode(t, response, &doc)
	if doc.ReviewStatus != "approved" || doc.ReviewNote != "verified" {
		t.Fatalf("review = %q/%q", doc.ReviewStatus, doc.ReviewNote)
	}
	stored, err := os.ReadFile(filepath.Join(cacheDir, "docs", docID+".json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(stored, []byte(`"review_status": "approved"`)) {
		t.Fatalf("review not persisted: %s", stored)
	}

	response = get(t, server, "/api/questions?status=open")
	var questions QuestionsResponse
	decode(t, response, &questions)
	if len(questions.Entries) != 1 || questions.Open != 1 {
		t.Fatalf("questions = %+v", questions)
	}
	questionID := questions.Entries[0].ID

	post(t, server, "/api/questions/"+questionID+"/resolve", `{"note":"done"}`)
	response = get(t, server, "/api/questions?status=resolved")
	decode(t, response, &questions)
	if len(questions.Entries) != 1 || questions.Entries[0].Note != "done" {
		t.Fatalf("resolved questions = %+v", questions)
	}

	post(t, server, "/api/questions/"+questionID+"/reopen", `{}`)
	response = get(t, server, "/api/questions?status=open")
	decode(t, response, &questions)
	if len(questions.Entries) != 1 {
		t.Fatalf("reopened questions = %+v", questions)
	}
}

func TestExportWritesBothProfilesAndIndex(t *testing.T) {
	server, _, _ := newTestServer(t)
	response := post(t, server, "/api/export", `{"profile":"both","include_stale":false,"digest":true,"digest_review":"any"}`)
	var result ExportResponse
	decode(t, response, &result)
	if result.Rendered != 2 {
		t.Fatalf("rendered = %d, want 2", result.Rendered)
	}
	if result.IndexDocs != 1 {
		t.Fatalf("index docs = %d, want 1", result.IndexDocs)
	}
	var agentFile, learningFile bool
	for _, path := range result.Paths {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("rendered file missing: %v", err)
		}
		if strings.HasSuffix(path, ".learn.md") {
			learningFile = true
		} else if strings.HasSuffix(path, ".md") {
			agentFile = true
		}
	}
	if !agentFile || !learningFile {
		t.Fatalf("rendered profiles agent=%v learning=%v paths=%v", agentFile, learningFile, result.Paths)
	}
	indexPath := filepath.Join(result.OutDir, "index.json")
	if _, err := os.Stat(indexPath); err != nil {
		t.Fatalf("index missing: %v", err)
	}
	if result.DigestPath == "" || result.DigestDocs != 1 {
		t.Fatalf("digest = %q docs=%d", result.DigestPath, result.DigestDocs)
	}
	digestData, err := os.ReadFile(result.DigestPath)
	if err != nil {
		t.Fatalf("digest missing: %v", err)
	}
	if !bytes.Contains(digestData, []byte("# Project memory (devlog-learn)")) {
		t.Fatalf("digest content = %s", digestData)
	}
}

func TestExportKeepsManualAgentsFile(t *testing.T) {
	server, _, _ := newTestServer(t)
	outDir := filepath.Join(filepath.Dir(server.opts.CursorPath), "out")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manual := filepath.Join(outDir, "AGENTS.md")
	if err := os.WriteFile(manual, []byte("manual agent instructions\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	response := post(t, server, "/api/export", `{"profile":"agent","digest":true}`)
	var result ExportResponse
	decode(t, response, &result)
	if result.DigestPath != "" {
		t.Fatalf("digest should not be reported: %+v", result)
	}
	joined := strings.Join(result.Warnings, " ")
	if !strings.Contains(joined, "was not generated") {
		t.Fatalf("warnings = %v", result.Warnings)
	}
	data, err := os.ReadFile(manual)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "manual agent instructions\n" {
		t.Fatalf("manual AGENTS.md was overwritten: %q", data)
	}
}

func TestExportPruneRemovesPreviouslyGeneratedStaleFile(t *testing.T) {
	server, _, _ := newTestServer(t)
	response := post(t, server, "/api/export",
		`{"profile":"agent","include_stale":true,"prune":false,"digest":false}`)
	var first ExportResponse
	decode(t, response, &first)
	if first.Rendered != 2 {
		t.Fatalf("initial render = %+v", first)
	}

	response = post(t, server, "/api/export",
		`{"profile":"agent","include_stale":false,"prune":true,"digest":false}`)
	var second ExportResponse
	decode(t, response, &second)
	if second.Rendered != 1 || second.Pruned != 1 {
		t.Fatalf("pruned render = %+v", second)
	}

	existing := 0
	for _, path := range first.Paths {
		if _, err := os.Stat(path); err == nil {
			existing++
		}
	}
	if existing != 1 {
		t.Fatalf("remaining generated files = %d, paths = %v", existing, first.Paths)
	}
}

func newTestServer(t *testing.T) (*Server, string, string) {
	t.Helper()
	root := t.TempDir()
	cacheDir := filepath.Join(root, "cache")
	store := &cache.Store{Root: cacheDir}

	doc := fixtureDoc("aaaaaaaaaaaaaaaa", "Fixture doc", extract.LifecycleActive)
	if err := store.SaveDoc(doc); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveSegment(segment.Segment{
		ID:         doc.ID,
		Hash:       "hash",
		Source:     "claude-code",
		SessionID:  "session-1",
		Project:    "demo",
		Title:      doc.Title,
		Start:      doc.OccurredAt,
		End:        doc.EndedAt,
		EventCount: 2,
		Events: []ingest.Event{
			{Seq: 1, Kind: ingest.KindUserText, Role: "user", Text: "fixture question"},
			{Seq: 2, Kind: ingest.KindAssistantText, Role: "assistant", Text: "fixture answer"},
		},
	}); err != nil {
		t.Fatal(err)
	}

	stale := fixtureDoc("bbbbbbbbbbbbbbbb", "Stale fixture", extract.LifecycleStale)
	if err := store.SaveDoc(stale); err != nil {
		t.Fatal(err)
	}

	server, err := New(Options{
		Addr:       "127.0.0.1:0",
		CacheDir:   cacheDir,
		CursorPath: filepath.Join(root, "cursor.json"),
		OutDir:     filepath.Join(root, "out"),
		Token:      "test-token",
	})
	if err != nil {
		t.Fatal(err)
	}
	return server, cacheDir, doc.ID
}

func fixtureDoc(id, title, lifecycle string) *extract.Doc {
	start := time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)
	return &extract.Doc{
		SchemaVersion: 1,
		ID:            id,
		Slug:          "fixture-doc",
		Title:         title,
		Summary:       "fixture summary",
		Project:       "demo",
		Source:        "claude-code",
		KnowledgeType: "bug_fix",
		OccurredAt:    start,
		EndedAt:       start.Add(10 * time.Minute),
		Stack:         []string{"go"},
		Tags:          []string{"fixture"},
		Difficulty:    "medium",
		Status:        "resolved",
		ReviewStatus:  extract.ReviewPending,
		Lifecycle:     lifecycle,
		Problem: extract.Problem{
			Statement: "fixture problem",
			Evidence:  []int{1},
		},
		RootCause: extract.RootCause{
			Text:       "fixture root cause",
			Known:      true,
			Confidence: "high",
			Evidence:   []int{2},
		},
		Solution: []extract.Step{
			{Order: 1, Action: "fix it", Detail: "detail", Evidence: []int{2}},
		},
		Insights: []extract.Insight{
			{Text: "fixture insight", Confidence: "medium", Evidence: []int{2}},
		},
		Lessons:       []string{"fixture lesson"},
		OpenQuestions: []string{"fixture open question"},
		Evidence: []extract.Evidence{
			{EventSeq: 1, Kind: "user_report", Quote: "fixture question", Verified: true},
			{EventSeq: 2, Kind: "assistant_analysis", Quote: "fixture answer", Verified: true},
		},
		Confidence: 0.8,
		Validation: extract.Validation{EvidenceChecked: 2, EvidenceVerified: 2},
		Provenance: extract.Provenance{
			SessionID:     "session-1",
			SegmentID:     id,
			SegmentHash:   "hash",
			Provider:      "claude-code",
			Model:         "fixture",
			PromptVersion: "extract-v1",
			ExtractedAt:   start,
		},
	}
}

func get(t *testing.T, server *Server, path string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, path, nil)
	request.Host = "127.0.0.1:8787"
	request.Header.Set("X-Devlog-Token", "test-token")
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	return response
}

func post(t *testing.T, server *Server, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	request.Host = "127.0.0.1:8787"
	request.Header.Set("X-Devlog-Token", "test-token")
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code >= 300 {
		t.Fatalf("POST %s status = %d body = %s", path, response.Code, response.Body.String())
	}
	return response
}

func decode(t *testing.T, response *httptest.ResponseRecorder, value any) {
	t.Helper()
	if response.Code < 200 || response.Code >= 300 {
		t.Fatalf("status = %d body = %s", response.Code, response.Body.String())
	}
	if err := json.Unmarshal(response.Body.Bytes(), value); err != nil {
		t.Fatalf("decode: %v body = %s", err, response.Body.String())
	}
}
