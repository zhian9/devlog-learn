# Security Policy

## Supported versions

| Version | Supported |
| --- | --- |
| 0.13.x | Yes |
| Earlier versions | No |

## Reporting a vulnerability

Do not open a public issue for a suspected vulnerability.

After the GitHub repository is published, use **Security > Report a vulnerability** to open a private GitHub Security Advisory. If that feature is temporarily unavailable, contact the maintainer through the GitHub profile listed on the repository.

Include:

- affected version and platform;
- the smallest synthetic reproduction you can provide;
- expected and observed behavior;
- impact and any known preconditions;
- whether a cache, rendered output, cookie, token, or subprocess is involved.

Do not include real session logs, credentials, customer data, home-directory contents, or unredacted model responses. Replace sensitive values with synthetic placeholders.

## Scope

Examples of in-scope reports:

- bypassing loopback, token, Host, or Origin checks in the local Web API;
- path traversal, symbolic-link escape, or unintended deletion through manifest pruning;
- command injection or unsafe argument handling in local CLI adapters;
- cross-process cache corruption, lost review state, or lock bypass;
- redaction failures where a known supported credential pattern is persisted;
- accidental inclusion of local state in release artifacts.

## Out of scope

- model quality, incorrect summaries, or a model provider's own data handling;
- costs caused by explicitly requested model calls;
- vulnerabilities in the local `claude` or `codex` CLI that devlog-learn cannot control;
- reports requiring an already-compromised local account or administrator privileges.

The project handles sensitive developer-session data. Redaction and local-only HTTP protections reduce risk; they do not turn untrusted logs into safe public documents. See [docs/PRIVACY.md](docs/PRIVACY.md).
