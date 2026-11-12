# Architecture

devlog-learn converts local Claude Code and Codex session logs into reviewable
knowledge documents and Agent-readable memory. It is a single-user CLI with an
embedded local Web UI.

## Data flow

```text
Claude Code / Codex JSONL
        |
        v
ingest -> segment -> redact -> prompt -> local claude/codex CLI
                                              |
                                              v
                                  validated Doc JSON
                                              |
                 +----------------------------+------------------+
                 |                            |                  |
                 v                            v                  v
              cache/docs                 Web UI / MCP       render/index/digest
                 |                            |                  |
                 +----------------------------+------------------+
                                              |
                                              v
                                  Markdown, indexes, AGENTS.md
```

The pipeline is incremental. File SHA-256 fingerprints decide whether parsing
is needed; segment hashes and parser/prompt versions decide whether extraction
cache entries are reusable.

## Packages

| Path | Responsibility |
| --- | --- |
| `cli/cmd` | Cobra command tree and user-facing I/O |
| `cli/internal/ingest` | Claude Code and Codex JSONL discovery/parsing |
| `cli/internal/segment` | Segment boundaries, IDs, hashes, and filtering |
| `cli/internal/redact` | Built-in and configured sensitive-data redaction |
| `cli/internal/pipeline` | Incremental orchestration and concurrent extraction |
| `cli/internal/cache` | Versioned entries, canonical docs, segments, writer lock |
| `cli/internal/cursor` | File fingerprints and incremental state |
| `cli/internal/extract` | Doc schema, prompts, CLI adapters, evidence validation |
| `cli/internal/render` | Stable Agent and learning Markdown profiles |
| `cli/internal/index` | Global/project indexes and related-document scoring |
| `cli/internal/manifest` | Output ownership records and safe pruning |
| `cli/internal/search` | Local lexical search and project summaries |
| `cli/internal/digest` | Compact `AGENTS.md` projection |
| `cli/internal/webapi` | Loopback HTTP API, SSE jobs, static SPA |
| `cli/web` | React, Vite, and TypeScript source |

The production frontend build is written to `cli/internal/webapi/dist` and is
embedded into the Go binary.

## Contracts

### Doc JSON

- `schema_version` changes only when downstream compatibility requires it.
- Additive optional fields are preferred over deleting or renaming fields.
- `id` remains stable when a session receives appended messages.
- `segment_hash` changes when the segment content changes.
- Evidence must reference `event_seq` values present in the prompt input.
- Strong claims without verified evidence are downgraded.

### Review state

Human review and resolved questions are stored separately where practical:

- Doc review fields live in `docs/<id>.json`.
- Resolved question state lives in `questions.json`.
- Re-extraction preserves review state only when `segment_hash` is unchanged.

### Output ownership

Every generated output is recorded in `<out>/.devlog-manifest.json` with its
profile, document ID, size, and SHA-256. Pruning is explicit and constrained:

- only files from the same profile are candidates;
- the file must still match the recorded hash;
- paths must remain below the output root;
- symbolic-link traversal is rejected.

## Concurrency and durability

`<cache-dir>/.devlog.lock` is the single writer lock for one cache root. The CLI
and Web UI use the same OS-level exclusive lock. Web write conflicts return HTTP
409; CLI write conflicts return an error.

The lock covers:

- extraction and cursor updates;
- whole `run` and `wizard` workflows;
- review and question mutations;
- rendering, indexing, digest generation, and export.

Cache files and the manifest are replaced through temporary files where
possible. On `serve` shutdown, the active job is canceled, SSE streams close,
and the HTTP server drains with a bounded timeout.

## Trust boundaries

1. **Session logs are sensitive and untrusted.** Parsers impose line and event
   limits and preserve malformed-line diagnostics.
2. **Redaction is best effort.** It reduces known credential and PII patterns;
   it does not classify arbitrary business data.
3. **The Web UI is local-only.** It binds to loopback, requires a per-process
   token, validates Host/Origin on writes, and sends restrictive security
   headers.
4. **Model adapters cross a provider boundary.** Prompts are passed to the local
   `claude` or `codex` CLI. That CLI may send content to its configured model
   provider.
5. **Model output is untrusted.** JSON repair, schema normalization, redaction,
   evidence validation, and confidence downgrades run before persistence.

## Extension points

- Add a provider by implementing discovery plus parsing into the unified
  `ingest.Session`/`ingest.Event` model, with synthetic fixtures and tests.
- Add an output format by consuming canonical Docs rather than parsing Markdown.
- Add retrieval features behind the existing search/MCP interfaces while
  preserving lexical fallback.
- Preserve cache invalidation by updating the relevant parser, segmenter,
  redaction, build, or prompt version.
