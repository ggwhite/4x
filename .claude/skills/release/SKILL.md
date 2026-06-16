---
name: release
description: "Release a new version of 4x: update CHANGELOG.md from git log, tag, and push. Use when the user says 「發布」「release」「發新版」「打 tag」「release new version」or similar. Also use when the user says 「更新 changelog」if they clearly intend to release afterward."
---

# Release

Publish a new 4x version. The CI pipeline (goreleaser + homebrew) triggers on tag push, so the job here is: write accurate release notes, get user approval, then tag and push.

## Workflow

### 1. Determine version

```bash
git tag -l 'v*' --sort=-v:refname | head -1
```

Bump the patch number by default (e.g., v0.1.8 -> v0.1.9). If the user specifies `minor` or `major`, bump accordingly.

Show the user: "上個版本是 vX.Y.Z，這次準備發 vA.B.C" and confirm before continuing.

### 2. Collect changes

```bash
git log <last-tag>..HEAD --oneline --no-decorate
```

If there are zero commits since the last tag, stop and tell the user there's nothing to release.

### 3. Draft changelog section

Read the existing `CHANGELOG.md` to match its style. The format follows [Keep a Changelog](https://keepachangelog.com/):

```markdown
## [X.Y.Z] - YYYY-MM-DD

### Fixes

- **Short name** — description of the fix

### Features

- **Short name** — description of the feature
```

Categorization rules:
- `fix` / `bugfix` commits -> `### Fixes`
- `feat` / feature commits -> `### Features`
- `ci` / workflow commits -> `### CI`
- `docs` commits -> `### Docs` (only if user-facing; skip changelog-only docs commits)
- `refactor` / `perf` / `chore` -> `### Internal` (only if noteworthy; skip trivial ones)

Each item should be a concise, user-facing description — not a raw commit message. Rewrite commit messages into the bold-name-em-dash style. Group related commits into a single item when they address the same thing.

Skip commits that are only changelog/release housekeeping (e.g., "docs: vX.Y.Z changelog").

### 4. Preview

Show the drafted changelog section to the user in full. Ask: "這樣 OK 嗎？要調整什麼？"

Do NOT proceed until the user approves. If the user requests changes, revise and show again.

### 5. Update CHANGELOG.md

Insert the new version section **at the top**, right after the file header (the "# Changelog" heading and the format description paragraph). The existing sections must not be modified — they correspond to already-released tags.

### 6. Commit

```bash
git add CHANGELOG.md
git commit -m "docs: vX.Y.Z changelog"
```

### 7. Tag

```bash
git tag vX.Y.Z
```

### 8. Push

Ask the user before pushing: "要 push 了，確認？"

```bash
git push && git push --tags
```

After push, tell the user: "Tag 已推送，CI 會自動跑 goreleaser 發布 release + 更新 homebrew。" and provide a link to the Actions page: `https://github.com/ggwhite/4x/actions`

## Important rules

- **Never modify existing version sections** — they match published releases
- **Date format** — YYYY-MM-DD, use today's date (timezone: Asia/Taipei)
- **Commit message** — always `docs: vX.Y.Z changelog` (no emoji, no scope parentheses)
- **One version per run** — if the user wants to skip versions, they say so explicitly
