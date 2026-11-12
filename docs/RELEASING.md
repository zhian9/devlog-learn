# Releasing

## Version sources

Before a release, keep these values synchronized:

- `cli/internal/version/version.go`
- `cli/web/package.json`
- `cli/web/package-lock.json`
- `README.md`
- `cli/README.md`
- `CHANGELOG.md`

Release binaries override the Go development version with GoReleaser
`-ldflags`.

## Pre-release verification

From `cli/`:

```bash
cd web
npm ci
npm run build
cd ..

gofmt -l .
go vet ./...
go test ./... -count=1
go test ./... -race -count=1
go build -o devlog-learn ./cmd
```

Confirm that:

- `gofmt -l .` prints nothing;
- no test calls a real model;
- the embedded frontend opens at `serve`;
- local cache, cursor, docs, and binaries are absent from `git status`;
- the version printed by the built binary matches the changelog.

## Local snapshot build

With GoReleaser installed:

```bash
goreleaser release --snapshot --clean
```

Inspect `dist/` and verify the Windows, Linux, and macOS archives plus
`checksums.txt`.

## GitHub release

1. Merge all release changes into `main`.
2. Create an annotated tag:

   ```bash
   git tag -a v0.13.0 -m "devlog-learn 0.13.0"
   git push origin main
   git push origin v0.13.0
   ```

3. The `Release` workflow builds binaries with GoReleaser and creates the
   GitHub Release from the tag.
4. Review generated notes, checksums, and archive contents before announcing.

Never move or reuse a published release tag. Publish a new patch version for a
fix.
