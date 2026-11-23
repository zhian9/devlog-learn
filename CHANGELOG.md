# Changelog

All notable changes to this project are documented in this file.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and the project follows [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added

- Project experience aggregation: group extracted Docs under the same project
  into a project experience (项目经历) or internship experience (实习经验), with
  a deterministic timeline, highlights, challenges, outcomes, STAR stories, and
  a static review checklist.
- `experience list|show|render|set|unset|checklist` CLI commands and a read-only
  Web UI "项目经历" page.
- Optional `experience synthesize` model supplement, cached separately and
  rendered as an explicitly unverified section; unknown `doc_id` references are
  dropped.
- `experiences.json` sidecar for human-owned kind/organization/role/period, with
  automatic kind suggestions that never override manual metadata.
- Experience generation from the Web UI export flow, plus `EXPERIENCE.md`
  freshness in `status` and links from generated README/index files.
- Document library facets for knowledge type, resolution status, and learning
  value, with clickable badges, dashboard cards, and knowledge-type pills.
- Segment-level selection on the Web UI extraction page, including
  indeterminate session checkboxes and exact partial-selection requests.
- Route-level error boundary so a single page failure keeps the sidebar usable
  instead of blanking the whole app.
- Copy buttons for configured paths, session selectors, and evidence events.

### Fixed

- Empty question results were serialized as `null`, which crashed the Web UI
  when a status filter had no entries.
- Clipboard copies now fall back to `execCommand` and report failure instead of
  silently doing nothing.

### Changed

- Session rows and document cards expose larger click targets; the brand links
  home and related-document reasons link to matching library filters.

## [0.13.0] - 2026-11-12

### Added

- Cross-process cache writer lock shared by CLI and Web UI.
- Cross-platform locking through `flock` on Unix and `LockFileEx` on Windows.
- Graceful `serve` shutdown with active-job cancellation and bounded draining.
- Output manifest with profile, document ID, size, and SHA-256 ownership data.
- Safe `--prune` support for `render`, `index`, `run`, and Web export.
- Frontend control for manifest-based stale output cleanup.
- Open-source repository documentation and GitHub automation.

### Changed

- `run` holds one write lock across extraction, rendering, and index generation.
- Concurrent write attempts now fail explicitly instead of sharing cache state.
- Output cleanup skips user-modified files, unregistered files, other profiles,
  and symbolic-link traversal.
- Version updated to `0.13.0`.

### Security

- Manifest paths are constrained to the output root and reject symbolic-link
  traversal below that root.
- Cache writers from different processes cannot update cursor, docs, review
  state, questions, or exports concurrently.

## [0.12.0] - 2026-11-12

### Added

- Local Web UI for reading, review, question resolution, evidence inspection,
  session selection, SSE progress, cancellation, and retry.
- Token usage diagnostics for Claude Code and Codex extraction calls.
- Manual review and question-closure sidecars.
