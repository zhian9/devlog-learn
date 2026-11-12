# Privacy and Data Handling

devlog-learn is designed for local developer-session data, but session logs can
contain source code, terminal output, credentials, customer data, and personal
information. Treat every source log and generated document as sensitive.

## What stays local

- Log discovery, parsing, segmentation, redaction, caching, indexing, and the
  local Web API run on the user's machine.
- The project does not include telemetry, analytics, remote fonts, CDN assets,
  or a project-operated cloud service.
- The Web UI binds only to loopback addresses and requires a per-process token.

## Model calls

Extraction is requested explicitly and runs through the locally configured
`claude` or `codex` executable. devlog-learn does not control that executable's
network behavior, authentication, retention policy, or provider configuration.
Depending on that CLI and its account settings, prompt content may be sent to a
model provider.

Before extracting private logs:

1. Review the provider and account data policy.
2. Prefer a synthetic or minimized repository when evaluating the workflow.
3. Configure `--model`, provider binaries, and model settings deliberately.
4. Start with one selected segment rather than a full historical scan.

## Redaction limitations

Built-in rules cover common credential formats, authentication headers,
credential assignments, PEM blocks, email addresses, phone numbers, private IP
addresses, long hexadecimal strings, and home-directory paths. Redaction is
defense in depth, not a guarantee that all sensitive data is removed.

Unknown secret formats, encoded payloads, proprietary identifiers, and business
data may remain. Always inspect output before publishing, sharing, or committing
it.

## Local files to protect

Do not commit or upload:

- `.devlog-cache/`
- `.devlog-docs/`
- `.devlog-cursor.json`
- `.codex/` configuration containing local paths
- generated `AGENTS.md`, Markdown, JSON, or logs from real sessions
- build binaries, database files, screenshots, or model raw responses

The repository `.gitignore` excludes the standard local paths, but users remain
responsible for custom `--cache-dir` and `--out` locations.

## Safe contribution checklist

- Use synthetic session fixtures.
- Replace names, domains, paths, IDs, and tokens with placeholders.
- Do not paste real model responses into issues or pull requests.
- Report security problems through the private process in
  [SECURITY.md](../SECURITY.md).
