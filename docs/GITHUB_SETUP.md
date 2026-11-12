# Publishing on GitHub

The repository is already initialized with Git. Do not run `git init` again.

## 1. Protect your email before the first commit

If your Git email is private or should not be public:

1. Open GitHub **Settings > Emails**.
2. Enable **Keep my email addresses private**.
3. Enable **Block command line pushes that expose my email**.
4. Set the local Git identity to the GitHub noreply address:

   ```bash
   git config user.name "YOUR GITHUB NAME"
   git config user.email "YOUR_ID+YOUR_USERNAME@users.noreply.github.com"
   ```

The first commit is permanent in public history. Check it before pushing.

## 2. Review the initial commit contents

```bash
git status --short --ignored
git add .
git status --short
git diff --cached --stat
```

Expected exclusions include:

- `.codex/`
- `.devlog-cache/`
- `.devlog-docs/`
- `.devlog-cursor.json`
- `cli/devlog-learn`
- `cli/devlog-learn.exe`
- `cli/web/node_modules/`

Search the staged tree for accidental secrets:

```bash
git grep -n -i -E 'api[_-]?key|secret|password|bearer [a-z0-9]|private key|ghp_|github_pat_|sk-' -- \
  ':!*.md' \
  ':!**/*_test.go' \
  ':!cli/web/package-lock.json' \
  ':!cli/internal/webapi/dist/**'
```

Review every match. Test placeholders such as `sk-abcdefghijklmnopqrstuvwx` are
expected; real credentials are not.

Create the initial commit only after the staged diff is clean:

```bash
git commit -m "Initial open-source release"
```

## 3. Authenticate GitHub CLI

```bash
gh auth login
gh auth status
```

Use HTTPS or SSH according to your environment. The repository does not
currently have a remote.

## 4. Create the public repository

From the repository root:

```bash
gh repo create devlog-learn \
  --public \
  --source=. \
  --remote=origin \
  --push
```

If the organization or repository name differs, replace `devlog-learn` and the
remote URL accordingly.

## 5. Configure repository security

After the first push, open repository **Settings** and enable:

- **General > Pull Requests > Allow squash merging** and disable unused merge
  methods if desired.
- **Rules > Rulesets** or **Branches**: require pull requests and the `CI` and
  `Security` checks for `main`.
- **Code security > Dependabot alerts**.
- **Code security > Code scanning**; confirm CodeQL results appear.
- **Code security > Secret scanning** and push protection when available.
- **Code security > Private vulnerability reporting** so `SECURITY.md` is
  actionable.

Recommended additional settings:

- Require conversation resolution.
- Require branches to be up to date before merging.
- Block force pushes and deletion of `main`.
- Limit Actions to GitHub-authored and explicitly approved actions.

## 6. Final repository metadata

Set:

- About: `Turn local Claude Code and Codex session logs into traceable project memory.`
- Website: optional project documentation URL.
- Topics: `codex`, `claude-code`, `developer-tools`, `go`, `knowledge-base`,
  `local-first`, `mcp`, `rag`.

After a release exists, replace `OWNER/REPOSITORY` placeholders in badges,
links, and issue-template URLs with the real GitHub path.

## 7. First release

Follow [RELEASING.md](RELEASING.md). Create `v0.13.0` only after `main` is green
and the public repository no longer contains any local-state files.
