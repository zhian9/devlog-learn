# Changelog

All notable changes to this project are documented in this file.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and the project follows [Semantic Versioning](https://semver.org/).

## [Unreleased]

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
