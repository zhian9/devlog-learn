## Summary

Describe the user-visible change and why it is needed.

## Verification

- [ ] `gofmt -l .`
- [ ] `go vet ./...`
- [ ] `go test ./... -race -shuffle=on -count=1`
- [ ] `cd web && npm run build` when frontend source changed
- [ ] `go build -trimpath ./cmd`

## Compatibility and privacy

- [ ] Doc/index/cursor schema changes are additive or include a migration plan.
- [ ] Cache invalidation and review preservation were considered.
- [ ] New write paths use the cache writer lock.
- [ ] New deletion behavior uses manifest ownership and hash checks.
- [ ] Tests and fixtures contain no real session logs, credentials, or personal paths.
- [ ] No `.codex/`, `.devlog-*`, binaries, or `node_modules/` are included.

## Linked issue

Closes #
