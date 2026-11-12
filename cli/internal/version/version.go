// Package version holds the version markers that participate in cache keys
// and incremental-scan decisions.
//
// These values are intentionally small and dependency-free. They are defined
// in phase 1 even though caching is implemented later, because the ingest
// layer must be able to report ParserVersion and callers must be able to
// invalidate derived data when parsing behaviour changes.
package version

// ToolVersion is the human-facing CLI release version. Release builds may
// override it with -ldflags. It does not participate in cache keys or
// incremental-scan decisions.
var ToolVersion = "0.13.0"

const (
	// SchemaVersion covers the shape of Doc JSON.
	SchemaVersion = 1
	// ParserVersion covers ingest.Event/Session semantics.
	ParserVersion = 1
	// SegmenterVersion covers segment boundaries and SegmentID generation.
	SegmenterVersion = 1
	// RedactionVersion covers built-in redaction rules.
	RedactionVersion = 1
	// BuildVersion covers prompt/transcript construction.
	BuildVersion = 1
)
