# Third-party notices

devlog-learn is distributed under the MIT License. Its source and release
artifacts use third-party packages whose licenses remain in effect.

## Go runtime dependencies

| Module | License |
| --- | --- |
| `github.com/spf13/cobra` | Apache-2.0 |
| `github.com/spf13/pflag` | BSD-3-Clause |
| `github.com/inconshreveable/mousetrap` | Apache-2.0 |

## Frontend dependencies

The production Web UI bundle includes packages from the dependency tree recorded
in `cli/web/package-lock.json`, including React, React DOM, React Router,
TanStack Query, `react-markdown`, `remark-gfm`, and Lucide. These packages are
distributed primarily under MIT, ISC, and Apache-2.0 licenses.

When redistributing binaries or frontend assets, retain the applicable license
and notice files required by each dependency. Dependency versions and integrity
hashes are recorded in `cli/go.sum` and `cli/web/package-lock.json`.
