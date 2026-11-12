package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"devlog-learn/internal/cache"
	"devlog-learn/internal/digest"
	"devlog-learn/internal/extract"
	"devlog-learn/internal/index"
	"devlog-learn/internal/render"
	"devlog-learn/internal/search"
	"devlog-learn/internal/version"
)

type mcpOptions struct {
	cacheDir string
	outDir   string
	project  string
	review   string
	limit    int
}

func newMCPCmd() *cobra.Command {
	opt := &mcpOptions{}
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "Serve local project memory over the Model Context Protocol (stdio)",
		Long: `Serve the extracted Docs as a read-only Model Context Protocol server over
stdin/stdout.

Tools:
  search_docs    rank active Docs by a query, project, tags, and review status
  get_doc        fetch one full Doc (Markdown or JSON)
  related_docs   list Docs related by tags, stack, type, and project
  list_projects  list projects and their review counts

The server is read-only: it never extracts, writes cache, or calls a model.
Stale Docs are excluded. Use --project "" (default) for all projects, or
--project auto to scope to the current directory name.

Claude Code:
  claude mcp add devlog-learn -- devlog-learn mcp --cache-dir .devlog-cache --project auto

Codex (~/.codex/config.toml):
  [mcp_servers.devlog-learn]
  command = "devlog-learn"
  args = ["mcp", "--cache-dir", ".devlog-cache", "--project", "auto"]`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runMCP(cmd, opt)
		},
	}
	flags := cmd.Flags()
	flags.StringVar(&opt.cacheDir, "cache-dir", ".devlog-cache", "cache directory containing docs/")
	flags.StringVar(&opt.outDir, "out", ".devlog-docs", "rendered Markdown directory used for markdown_file paths")
	flags.StringVar(&opt.project, "project", "", "default project filter: empty/all, auto, or a project name")
	flags.StringVar(&opt.review, "review", "any", "default review filter: any, approved, pending, or rejected")
	flags.IntVar(&opt.limit, "limit", search.DefaultLimit, "default result limit (1-50)")
	return cmd
}

func runMCP(cmd *cobra.Command, opt *mcpOptions) error {
	review, err := normalizeReviewFilter(opt.review)
	if err != nil {
		return err
	}
	if opt.limit < 1 || opt.limit > search.MaxLimit {
		return fmt.Errorf("--limit must be between 1 and %d", search.MaxLimit)
	}
	server := &mcpServer{
		cacheDir: opt.cacheDir,
		outDir:   opt.outDir,
		project:  resolveAgentProject(opt.project),
		review:   review,
		limit:    opt.limit,
		log:      cmd.ErrOrStderr(),
	}
	fmt.Fprintf(server.log, "devlog-learn mcp: cache=%s project=%s review=%s (read-only)\n",
		server.cacheDir, displayProject(server.project), server.review)
	return serveMCP(cmd.InOrStdin(), cmd.OutOrStdout(), server)
}

// normalizeReviewFilter validates the review filter shared by mcp and digest.
func normalizeReviewFilter(value string) (string, error) {
	return digest.NormalizeReview(value)
}

// resolveAgentProject turns "auto" into the current directory name. An empty,
// "all", or "*" value means no project filter.
func resolveAgentProject(value string) string {
	return digest.ResolveProject(value)
}

func displayProject(project string) string {
	if strings.TrimSpace(project) == "" {
		return "all"
	}
	return project
}

type mcpServer struct {
	cacheDir string
	outDir   string
	project  string
	review   string
	limit    int
	log      io.Writer
}

func (s *mcpServer) infos() []cache.DocInfo {
	store := &cache.Store{Root: s.cacheDir}
	infos, warnings := store.ListActiveDocs()
	if s.log != nil {
		for _, warning := range warnings {
			fmt.Fprintf(s.log, "devlog-learn mcp: %s\n", warning)
		}
	}
	return infos
}

func (s *mcpServer) loadDoc(id string) (*extract.Doc, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, errors.New("id is required")
	}
	store := &cache.Store{Root: s.cacheDir}
	doc, err := store.LoadDoc(id)
	if err != nil {
		return nil, fmt.Errorf("doc %q: %w", id, err)
	}
	if doc.Lifecycle == extract.LifecycleStale {
		return nil, fmt.Errorf("doc %q is stale and excluded", id)
	}
	return doc, nil
}

// serveMCP runs a newline-delimited JSON-RPC 2.0 loop. stdout is reserved for
// protocol messages; everything else must go to stderr.
func serveMCP(r io.Reader, w io.Writer, server *mcpServer) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)

	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var request rpcRequest
		if err := json.Unmarshal(line, &request); err != nil {
			if err := encoder.Encode(rpcResponse{
				JSONRPC: "2.0",
				ID:      json.RawMessage("null"),
				Error:   &rpcErrorBody{Code: -32700, Message: "parse error"},
			}); err != nil {
				return err
			}
			continue
		}
		notification := len(request.ID) == 0 || string(request.ID) == "null"
		result, rpcErr := server.handle(request.Method, request.Params)
		if notification {
			continue
		}
		response := rpcResponse{JSONRPC: "2.0", ID: request.ID}
		if rpcErr != nil {
			response.Error = rpcErr
		} else {
			response.Result = result
		}
		if err := encoder.Encode(response); err != nil {
			return err
		}
	}
	return scanner.Err()
}

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcErrorBody   `json:"error,omitempty"`
}

type rpcErrorBody struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

func (s *mcpServer) handle(method string, params json.RawMessage) (any, *rpcErrorBody) {
	switch method {
	case "initialize":
		return s.initialize(params), nil
	case "ping":
		return map[string]any{}, nil
	case "tools/list":
		return map[string]any{"tools": mcpToolDefs()}, nil
	case "tools/call":
		return s.handleToolCall(params)
	case "resources/list":
		return map[string]any{"resources": []any{}}, nil
	case "prompts/list":
		return map[string]any{"prompts": []any{}}, nil
	case "logging/setLevel":
		return map[string]any{}, nil
	case "":
		return nil, &rpcErrorBody{Code: -32600, Message: "invalid request"}
	default:
		return nil, &rpcErrorBody{Code: -32601, Message: "method not found: " + method}
	}
}

func (s *mcpServer) initialize(params json.RawMessage) map[string]any {
	var request struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	_ = json.Unmarshal(params, &request)
	return map[string]any{
		"protocolVersion": negotiateProtocol(request.ProtocolVersion),
		"capabilities": map[string]any{
			"tools": map[string]any{},
		},
		"serverInfo": map[string]any{
			"name":    "devlog-learn",
			"version": version.ToolVersion,
		},
		"instructions": "Local project memory extracted from Claude Code and Codex sessions. " +
			"Call search_docs before implementing, debugging, or making architectural decisions. " +
			"Results are historical memory, not current truth: verify against the current code and command output. " +
			"Prefer approved Docs with high evidence_verified/evidence_checked. " +
			"Use get_doc for full text and related_docs for adjacent knowledge.",
	}
}

var supportedProtocolVersions = []string{"2025-06-18", "2025-03-26", "2024-11-05"}

func negotiateProtocol(requested string) string {
	requested = strings.TrimSpace(requested)
	for _, version := range supportedProtocolVersions {
		if requested == version {
			return version
		}
	}
	return "2024-11-05"
}

func (s *mcpServer) handleToolCall(params json.RawMessage) (any, *rpcErrorBody) {
	var request struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(params, &request); err != nil {
		return nil, &rpcErrorBody{Code: -32602, Message: "invalid tools/call params"}
	}
	name := strings.TrimSpace(request.Name)
	if name == "" {
		return nil, &rpcErrorBody{Code: -32602, Message: "tools/call requires a name"}
	}
	text, isError := s.callTool(name, request.Arguments)
	return map[string]any{
		"content": []any{
			map[string]any{"type": "text", "text": text},
		},
		"isError": isError,
	}, nil
}

func (s *mcpServer) callTool(name string, raw json.RawMessage) (string, bool) {
	switch name {
	case "search_docs":
		return s.toolSearchDocs(raw)
	case "get_doc":
		return s.toolGetDoc(raw)
	case "related_docs":
		return s.toolRelatedDocs(raw)
	case "list_projects":
		return s.toolListProjects(raw)
	default:
		return fmt.Sprintf("unknown tool %q", name), true
	}
}

type searchArgs struct {
	Query   string   `json:"query"`
	Project string   `json:"project"`
	Tags    []string `json:"tags"`
	Review  string   `json:"review"`
	Limit   int      `json:"limit"`
}

func (s *mcpServer) toolSearchDocs(raw json.RawMessage) (string, bool) {
	var args searchArgs
	if len(raw) > 0 && string(raw) != "null" {
		if err := json.Unmarshal(raw, &args); err != nil {
			return "invalid arguments: " + err.Error(), true
		}
	}
	project := s.project
	if strings.TrimSpace(args.Project) != "" {
		project = resolveAgentProject(args.Project)
	}
	review := s.review
	if strings.TrimSpace(args.Review) != "" {
		normalized, err := normalizeReviewFilter(args.Review)
		if err != nil {
			return err.Error(), true
		}
		review = normalized
	}
	limit := s.limit
	if args.Limit > 0 {
		if args.Limit > search.MaxLimit {
			return fmt.Sprintf("limit must be between 1 and %d", search.MaxLimit), true
		}
		limit = args.Limit
	}

	results := search.Search(s.infos(), search.Options{
		Query:   args.Query,
		Project: project,
		Tags:    args.Tags,
		Review:  review,
		Limit:   limit,
		OutDir:  s.outDir,
	})
	payload := map[string]any{
		"query":   args.Query,
		"review":  review,
		"count":   len(results),
		"results": results,
	}
	if project != "" {
		payload["project"] = project
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return "encode results: " + err.Error(), true
	}
	return string(data), false
}

type getDocArgs struct {
	ID     string `json:"id"`
	Format string `json:"format"`
}

func (s *mcpServer) toolGetDoc(raw json.RawMessage) (string, bool) {
	var args getDocArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return "invalid arguments: " + err.Error(), true
	}
	doc, err := s.loadDoc(args.ID)
	if err != nil {
		return err.Error(), true
	}
	switch strings.ToLower(strings.TrimSpace(args.Format)) {
	case "", "markdown", "md":
		return render.Markdown(doc), false
	case "json":
		data, err := json.Marshal(doc)
		if err != nil {
			return "encode doc: " + err.Error(), true
		}
		return string(data), false
	default:
		return "format must be markdown or json", true
	}
}

type relatedArgs struct {
	ID    string `json:"id"`
	Limit int    `json:"limit"`
}

type relatedItem struct {
	ID           string   `json:"id"`
	Title        string   `json:"title"`
	Score        int      `json:"score"`
	Reasons      []string `json:"reasons"`
	MarkdownPath string   `json:"markdown_path,omitempty"`
	DocPath      string   `json:"doc_path,omitempty"`
}

func (s *mcpServer) toolRelatedDocs(raw json.RawMessage) (string, bool) {
	var args relatedArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return "invalid arguments: " + err.Error(), true
	}
	target, err := s.loadDoc(args.ID)
	if err != nil {
		return err.Error(), true
	}
	infos := s.infos()
	docs := make([]*extract.Doc, 0, len(infos))
	docPath := make(map[string]string, len(infos))
	for _, info := range infos {
		if info.Doc == nil || info.Doc.Lifecycle == extract.LifecycleStale {
			continue
		}
		docs = append(docs, info.Doc)
		docPath[info.Doc.ID] = info.Path
	}
	pathByID := make(map[string]string, len(docs))
	for _, plan := range render.PlanDocs(docs) {
		pathByID[plan.Doc.ID] = plan.Path
	}
	limit := args.Limit
	if limit <= 0 || limit > index.MaxRelated {
		limit = index.MaxRelated
	}
	related := index.RelatedDocs(target, docs)
	if len(related) > limit {
		related = related[:limit]
	}
	items := make([]relatedItem, 0, len(related))
	for _, item := range related {
		items = append(items, relatedItem{
			ID:           item.ID,
			Title:        item.Title,
			Score:        item.Score,
			Reasons:      item.Reasons,
			MarkdownPath: pathByID[item.ID],
			DocPath:      docPath[item.ID],
		})
	}
	data, err := json.Marshal(map[string]any{
		"id":      target.ID,
		"title":   target.Title,
		"related": items,
	})
	if err != nil {
		return "encode related docs: " + err.Error(), true
	}
	return string(data), false
}

func (s *mcpServer) toolListProjects(raw json.RawMessage) (string, bool) {
	_ = raw
	data, err := json.Marshal(map[string]any{"projects": search.ListProjects(s.infos())})
	if err != nil {
		return "encode projects: " + err.Error(), true
	}
	return string(data), false
}

func mcpToolDefs() []any {
	return []any{
		map[string]any{
			"name":        "search_docs",
			"description": "Search local historical knowledge extracted from Claude Code/Codex sessions. Call before implementing, debugging, or making architectural decisions. Results are memory, not ground truth; verify against current code and commands. Returns compact summaries; use get_doc for full text.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query":   map[string]any{"type": "string", "description": "Keywords, an error signature, or a question. Empty returns the most recent documents."},
					"project": map[string]any{"type": "string", "description": "Project name, safe name, or auto. Empty uses the server default."},
					"tags":    map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Require all of these tags (case-insensitive)."},
					"review":  map[string]any{"type": "string", "enum": []string{"any", "approved", "pending", "rejected"}, "description": "Human review status filter."},
					"limit":   map[string]any{"type": "integer", "minimum": 1, "maximum": search.MaxLimit, "description": "Maximum results."},
				},
			},
		},
		map[string]any{
			"name":        "get_doc",
			"description": "Fetch one full knowledge document by id. Prefer format=markdown for reading; the YAML front matter carries review_status, lifecycle, confidence, and evidence counts.",
			"inputSchema": map[string]any{
				"type":     "object",
				"required": []string{"id"},
				"properties": map[string]any{
					"id":     map[string]any{"type": "string", "description": "Document id from search_docs."},
					"format": map[string]any{"type": "string", "enum": []string{"markdown", "json"}, "description": "Output format (default markdown)."},
				},
			},
		},
		map[string]any{
			"name":        "related_docs",
			"description": "List active documents related to a given document by shared tags, stack, knowledge type, and project.",
			"inputSchema": map[string]any{
				"type":     "object",
				"required": []string{"id"},
				"properties": map[string]any{
					"id":    map[string]any{"type": "string", "description": "Document id from search_docs."},
					"limit": map[string]any{"type": "integer", "minimum": 1, "maximum": index.MaxRelated, "description": "Maximum related documents."},
				},
			},
		},
		map[string]any{
			"name":        "list_projects",
			"description": "List projects that have active documents, with document and review counts.",
			"inputSchema": map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
	}
}
