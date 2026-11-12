package webapi

import (
	"encoding/json"
	"time"

	"devlog-learn/internal/extract"
	"devlog-learn/internal/index"
	"devlog-learn/internal/ingest"
	"devlog-learn/internal/pipeline"
	"devlog-learn/internal/questions"
	"devlog-learn/internal/search"
)

// StatusResponse is the dashboard payload. It intentionally mirrors the CLI
// "status" report but stays a web-specific shape so the CLI text output can
// keep evolving independently.
type StatusResponse struct {
	ToolVersion   string           `json:"tool_version"`
	SchemaVersion int              `json:"schema_version"`
	GeneratedAt   time.Time        `json:"generated_at"`
	Cache         StatusCache      `json:"cache"`
	Questions     StatusQuestions  `json:"questions"`
	Cursor        StatusCursor     `json:"cursor"`
	Outputs       StatusOutputs    `json:"outputs"`
	Providers     []StatusProvider `json:"providers"`
	Projects      []search.Project `json:"projects"`
	Knowledge     map[string]int   `json:"knowledge_types"`
	Recent        []search.Result  `json:"recent"`
	Warnings      []string         `json:"warnings,omitempty"`
}

type StatusCache struct {
	Dir       string `json:"dir"`
	Exists    bool   `json:"exists"`
	SizeBytes int64  `json:"size_bytes"`
	Active    int    `json:"active_docs"`
	Stale     int    `json:"stale_docs"`
	Pending   int    `json:"pending"`
	Approved  int    `json:"approved"`
	Rejected  int    `json:"rejected"`
}

type StatusQuestions struct {
	Open     int `json:"open"`
	Resolved int `json:"resolved"`
}

type StatusCursor struct {
	Path          string     `json:"path"`
	Exists        bool       `json:"exists"`
	UpdatedAt     *time.Time `json:"updated_at,omitempty"`
	Files         int        `json:"files"`
	Errors        int        `json:"errors"`
	VersionsMatch bool       `json:"versions_match"`
}

type StatusOutputs struct {
	Dir              string     `json:"dir"`
	IndexExists      bool       `json:"index_exists"`
	IndexGeneratedAt *time.Time `json:"index_generated_at,omitempty"`
	IndexStale       bool       `json:"index_stale"`
	DigestExists     bool       `json:"digest_exists"`
	DigestModifiedAt *time.Time `json:"digest_modified_at,omitempty"`
	DigestStale      bool       `json:"digest_stale"`
}

type StatusProvider struct {
	Name         string `json:"name"`
	Bin          string `json:"bin"`
	BinFound     bool   `json:"bin_found"`
	Dir          string `json:"dir"`
	DirExists    bool   `json:"dir_exists"`
	SessionFiles int    `json:"session_files"`
}

// DocListResponse is the library payload.
type DocListResponse struct {
	Total    int             `json:"total"`
	Docs     []search.Result `json:"docs"`
	Warnings []string        `json:"warnings,omitempty"`
}

// DocDetailResponse carries everything the detail page needs in one request.
type DocDetailResponse struct {
	Doc               *extract.Doc      `json:"doc"`
	MarkdownPath      string            `json:"markdown_path"`
	LearnMarkdownPath string            `json:"learn_markdown_path"`
	Related           []index.Related   `json:"related"`
	Questions         []questions.Entry `json:"questions"`
	Warnings          []string          `json:"warnings,omitempty"`
}

// EventsResponse returns the source events referenced by evidence. Raw parser
// envelopes are omitted: they are large and never useful in the reading view.
type EventsResponse struct {
	SegmentID  string      `json:"segment_id"`
	Source     string      `json:"source"`
	SessionID  string      `json:"session_id"`
	Project    string      `json:"project"`
	Title      string      `json:"title"`
	Start      time.Time   `json:"start"`
	End        time.Time   `json:"end"`
	EventCount int         `json:"event_count"`
	Events     []EventView `json:"events"`
}

type EventView struct {
	Seq           int                `json:"seq"`
	Kind          string             `json:"kind"`
	Role          string             `json:"role,omitempty"`
	Time          time.Time          `json:"time,omitzero"`
	Text          string             `json:"text,omitempty"`
	ToolName      string             `json:"tool_name,omitempty"`
	ToolInput     json.RawMessage    `json:"tool_input,omitempty"`
	ToolInputText string             `json:"tool_input_text,omitempty"`
	ToolResult    string             `json:"tool_result,omitempty"`
	IsError       bool               `json:"is_error,omitempty"`
	Truncated     bool               `json:"truncated,omitempty"`
	Exec          *ingest.ExecResult `json:"exec,omitempty"`
}

type QuestionsResponse struct {
	Entries  []questions.Entry `json:"entries"`
	Open     int               `json:"open"`
	Resolved int               `json:"resolved"`
	Warnings []string          `json:"warnings,omitempty"`
}

type ReviewRequest struct {
	Status string  `json:"status"`
	Note   *string `json:"note"`
}

type NoteRequest struct {
	Note string `json:"note"`
}

type ExportRequest struct {
	Profile      string `json:"profile"`
	IncludeStale bool   `json:"include_stale"`
	Prune        bool   `json:"prune"`

	Digest        bool   `json:"digest"`
	DigestReview  string `json:"digest_review"`
	DigestProject string `json:"digest_project"`
	DigestLimit   int    `json:"digest_limit"`
	ForceDigest   bool   `json:"force_digest"`
}

type ExportResponse struct {
	Profile       string   `json:"profile"`
	Rendered      int      `json:"rendered"`
	StaleSkipped  int      `json:"stale_skipped"`
	Pruned        int      `json:"pruned"`
	PruneSkipped  int      `json:"prune_skipped"`
	IndexDocs     int      `json:"index_docs"`
	IndexProjects int      `json:"index_projects"`
	DigestPath    string   `json:"digest_path,omitempty"`
	DigestDocs    int      `json:"digest_docs"`
	Paths         []string `json:"paths,omitempty"`
	Warnings      []string `json:"warnings,omitempty"`
	OutDir        string   `json:"out_dir"`
}

// ExtractJobRequest starts one extraction job. Sessions and segments use the
// same selectors as the CLI; at least one is required so a click can never
// accidentally extract every historical segment.
type ExtractJobRequest struct {
	Provider         string   `json:"provider"`
	Since            string   `json:"since"`
	Sessions         []string `json:"sessions"`
	Segments         []string `json:"segments"`
	Concurrency      int      `json:"concurrency"`
	TimeoutSeconds   int      `json:"timeout_seconds"`
	Model            string   `json:"model"`
	IncludeSubagents bool     `json:"include_subagents"`
	Full             bool     `json:"full"`
	Force            bool     `json:"force"`
}

type JobEvent struct {
	pipeline.ProgressEvent
	Seq int       `json:"seq"`
	At  time.Time `json:"at"`
}

type JobSegment struct {
	SegmentID        string  `json:"segment_id"`
	Title            string  `json:"title"`
	Project          string  `json:"project"`
	Source           string  `json:"source"`
	Cached           bool    `json:"cached"`
	Error            string  `json:"error,omitempty"`
	DurationMS       int64   `json:"duration_ms"`
	Confidence       float64 `json:"confidence"`
	EvidenceChecked  int     `json:"evidence_checked"`
	EvidenceVerified int     `json:"evidence_verified"`
}

type JobResult struct {
	Files          int          `json:"files"`
	Parsed         int          `json:"parsed"`
	Skipped        int          `json:"skipped"`
	FailedFiles    int          `json:"failed_files"`
	Segments       int          `json:"segments"`
	Selected       int          `json:"selected"`
	CacheHits      int          `json:"cache_hits"`
	Extracted      int          `json:"extracted"`
	FailedSegments int          `json:"failed_segments"`
	Reports        []JobSegment `json:"reports"`
	Warnings       []string     `json:"warnings,omitempty"`
	DurationMS     int64        `json:"duration_ms"`
}

type JobSnapshot struct {
	ID         string     `json:"id"`
	Status     string     `json:"status"` // running, done, failed, canceled
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
	Total      int        `json:"total"`
	Done       int        `json:"done"`
	Cached     int        `json:"cached"`
	Extracted  int        `json:"extracted"`
	Failed     int        `json:"failed"`
	Error      string     `json:"error,omitempty"`
	Result     *JobResult `json:"result,omitempty"`
	Events     []JobEvent `json:"events,omitempty"`
}

type ConfigResponse struct {
	ToolVersion string `json:"tool_version"`
	CacheDir    string `json:"cache_dir"`
	CursorPath  string `json:"cursor_path"`
	OutDir      string `json:"out_dir"`
	Addr        string `json:"addr"`
	LocalOnly   bool   `json:"local_only"`
	ClaudeBin   string `json:"claude_bin"`
	CodexBin    string `json:"codex_bin"`
	ClaudeDir   string `json:"claude_dir"`
	CodexDir    string `json:"codex_dir"`
}
