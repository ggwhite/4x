# Threat Model: 4x

## 1. System context

4x is a multi-role AI development loop framework written in Go (1.26.3+). It decomposes software engineering into four sequentially isolated AI-driven phases — Designer, Coder, Reviewer, Tester — orchestrated by a deterministic Go CLI that never calls LLMs directly. Each role is executed by an LLM CLI subprocess (Claude Code, Codex, Gemini, or Antigravity) spawned via `exec.Command` with the runner binary and arguments configured in `.4x/settings.json`.

All inter-role communication happens via plain files in a `.4x/` protocol directory: YAML feature definitions, JSON state files, and markdown artifacts (task briefs, review reports, test reports). A localhost HTTP server (default port 4567) with no authentication serves a dashboard consumed by a macOS native app (WKWebView-based) or web browser. The server exposes full CRUD operations: starting/stopping AI agent runs, rewriting project and user settings, triggering git merges to main, browsing arbitrary filesystem paths, and streaming events via SSE.

The project targets individual developers and small teams who want structured, auditable AI-assisted development with adversarial review separation. It is designed for local-machine use; runners are configured with `--dangerously-skip-permissions` / `-y` flags for autonomous unattended execution. The codebase is approximately 33,000 lines of Go, plus a Swift macOS desktop app and a Python icon generator script.

## 2. Assets

| asset | description | sensitivity |
|---|---|---|
| Runner command construction | `RunnerConfig.Command` + `Args` from settings.json, resolved with `{prompt}`, `{promptFile}`, `{model}` placeholders and executed via `exec.Command`. Attacker controlling settings.json achieves arbitrary command execution. | critical |
| Project config (.4x/settings.json) | Runner command/args templates, model tier mapping, build/test/lint commands, hook shell strings. Writable via `PUT /api/settings` (no auth). | high |
| User config (~/.4x/settings.json) | User-level runner overrides that take precedence over project config. Writable via `PUT /api/user-config` (no auth). | high |
| Source code integrity (main branch) | Git merge operations via `POST /api/done` or `4x done` land code on main. Corrupted worktree or race in merge path can inject unreviewed code. | high |
| Feature state (state.json) | Active phase, role, round counter, PID, runner name, stop reason. PID field used to signal/kill subprocesses; phase field drives state machine transitions. | high |
| Role artifacts (rounds/*.md) | task-brief, acceptance-criteria, coder-report, review-report, test-report. Written by one role, consumed by the next. Cross-role trust boundary. | high |
| Git worktree state | Per-feature isolated git worktree for code modifications; merged back to main branch. | high |
| Runner subprocess environment | Full `os.Environ()` including `ANTHROPIC_API_KEY`, `GITHUB_TOKEN`, `AWS_*` passed to child processes without filtering or scrubbing. | high |
| Dashboard HTTP server | Unauthenticated local server exposing full CRUD over features, settings, git merge, subprocess lifecycle, and filesystem browsing. | high |
| Process integrity (state machine) | State machine consistency; illegal transitions can corrupt feature lifecycle (e.g., done → blocked). | high |
| Published release binaries | Distributed via GitHub Releases and Homebrew tap. Supply chain target for end users. | high |
| HOMEBREW_TAP_GITHUB_TOKEN | Write access to `ggwhite/homebrew-tap`, managed outside this repo in GitHub Actions secrets. | high |
| Feature definitions (*.yaml) | Feature spec, subtasks, scope paths, dependency graph. Defines what AI agents implement. | medium |
| Event log (events.jsonl) | Append-only event log streamed via SSE. Contains model names, command strings, run timing. | medium |
| Verify evidence (verify.json) | Test commands, exit codes, durations, screenshot paths. Tester verdict based on this file. | medium |
| Baseline snapshot (baseline.json) | Git HEAD SHA and branch at run start. Scope guardrail depends on this for drift detection. | medium |
| Prompt temp files (/tmp/4x-prompt-*.md) | World-readable temp files with full role prompt including feature spec and codebase context. | medium |
| Batch orchestration files | batch-pid, batch-conflict.json, batch-report.json. PID field controls process adoption. | medium |

## 3. Entry points & trust boundaries

| entry_point | description | trust_boundary | reachable_assets |
|---|---|---|---|
| HTTP server (localhost, no auth) | TCP listener on `127.0.0.1:4567`, no auth/CORS/CSRF. Full CRUD: `/api/run`, `/api/stop`, `/api/done`, `/api/settings` PUT, `/api/user-config` PUT, `/api/batch/*`, `/api/new`, `/api/browse` | any local process → full API control | Runner command construction, Project config, User config, Source code integrity, Feature state, Git worktree state |
| Subprocess spawn (exec.Command) | Spawns AI runner binary using `RunnerConfig` from settings.json. Also executes hook shell strings and build/test commands via `sh -c` | settings.json (user-controlled) → shell execution | Runner subprocess environment, Source code integrity |
| AI-written test-strategy.yaml → sh -c | Tester AI role writes `test-strategy.yaml`; verify commands consumed verbatim by `exec.CommandContext("sh", "-c", cmd)`. Direct prompt-injection-to-shell path | AI output → shell execution | Runner subprocess environment, Source code integrity |
| YAML/JSON deserialization | `yaml.Unmarshal` on feature YAML, test-strategy, settings; `json.Unmarshal` on state, events, escalation, HTTP request bodies | local filesystem + AI-written files → Go structs | Feature state, Feature definitions, Process integrity |
| Screenshot file serve | Decodes base64 URL token → `filepath.Abs` → `HasPrefix` check → `http.ServeFile`. No `EvalSymlinks` before prefix check | URL input → filesystem read | any file reachable via symlink |
| CLI argv (Cobra) | Feature IDs, paths, flags across all subcommands | authenticated local user → CLI operations | Feature state, Project config |
| install.sh curl pipe | Fetches tag from GitHub API unauthenticated, downloads tarball, optionally `sudo install`. No checksum verification | internet → user shell (optionally root) | Published release binaries |
| GitHub Actions release workflow | Tag push triggers goreleaser + binary builds. Uses `GITHUB_TOKEN` (`contents:write`) and `HOMEBREW_TAP_GITHUB_TOKEN`. Unpinned third-party actions | GitHub Actions runner → published binaries | Published release binaries, HOMEBREW_TAP_GITHUB_TOKEN |
| macOS WKWebView bridge (nativeOpenFolder) | JS path from `NSOpenPanel` interpolated into JS eval with only single-quote escaping | WebView JS → native macOS process | Source code integrity |
| macOS NSDistributedNotificationCenter | Subscribes to `com.ggwhite.4x.notify` with no sender restriction (`object: nil`) | any local process → UNNotification | Dashboard HTTP server |
| Port auto-discovery (4567-4666) | `findAvailablePort` silently falls through if port occupied; port-squatting possible | user process namespace → server trust | Dashboard HTTP server, Source code integrity |
| /api/browse (multi-mode) | Directory listing with no root restriction. Any filesystem path accessible to process user | localhost → arbitrary filesystem traversal | any readable file/directory |
| Environment variables | `FOURX_LOG_LEVEL`, `HOME`, `LANG` used without validation. Full `os.Environ()` passed to child processes | process environment → child processes | Runner subprocess environment |
| macOS entitlement (allow-unsigned-executable-memory) | Weakens W^X enforcement for `4xLive` process | macOS hardened runtime boundary | Source code integrity |

## 4. Threats

| id | threat | actor | surface | asset | impact | likelihood | status | controls | evidence |
|---|---|---|---|---|---|---|---|---|---|
| T1 | Arbitrary command execution via AI-written test-strategy.yaml consumed by `sh -c` without sanitization or allowlist | local_user | AI-written test-strategy.yaml → sh -c | Runner subprocess environment, Source code integrity | critical | likely | unmitigated | none | |
| T2 | Unauthenticated localhost API enables any local process to spawn AI agents, rewrite runner config, merge code to main, and browse arbitrary filesystem paths | local_user | HTTP server (localhost, no auth), /api/browse (multi-mode) | Runner command construction, Project config, User config, Source code integrity | critical | likely | unmitigated | localhost binding only | 5b6ad54, 60c38d4 |
| T3 | AI agent scope bypass via guardrail evasion — untracked files, YAML corruption, or missing phase guards allow out-of-scope code modifications to pass unchecked | local_user | Subprocess spawn (exec.Command), YAML/JSON deserialization | Source code integrity, Process integrity | high | likely | partially_mitigated | scope guard (with known bypasses fixed and remaining) | 15f0dfd, 207bdaa, 4cd74b6, 9d4d0c0 |
| T4 | Supply-chain compromise via unauthenticated install.sh curl pipe with no checksum verification, or via unpinned third-party GitHub Actions with `contents:write` scope in release workflow | supply_chain | install.sh curl pipe, GitHub Actions release workflow | Published release binaries, HOMEBREW_TAP_GITHUB_TOKEN | critical | possible | unmitigated | TLS (no pinning), go.sum for Go deps | |
| T5 | Arbitrary command execution via runner config injection — attacker controlling settings.json (via `PUT /api/settings` or filesystem access) can inject arbitrary commands into `RunnerConfig.Command`/`Args` | local_user | HTTP server (localhost, no auth), Subprocess spawn (exec.Command) | Runner command construction, Runner subprocess environment | critical | possible | unmitigated | none | |
| T6 | Path traversal via screenshot serve endpoint — symlink inside workspace pointing outside the root allows reading arbitrary files accessible to the process user | local_user | Screenshot file serve | Source code integrity | high | possible | partially_mitigated | HasPrefix check (no EvalSymlinks) | |
| T7 | Port-squatting attack — malicious process occupies port 4567, macOS app silently connects to attacker-controlled server on a different port, serving crafted API responses | local_user | Port auto-discovery (4567-4666) | Dashboard HTTP server, Source code integrity | high | rare | unmitigated | none | |
| T8 | Sensitive data exposure via world-readable prompt temp files, unfiltered environment variable inheritance to child processes, and unauthenticated SSE/log streaming | local_user | Environment variables, HTTP server (localhost, no auth) | Runner subprocess environment, Prompt temp files | high | possible | unmitigated | none | |
| T9 | macOS notification spoofing via NSDistributedNotificationCenter — any local process can display fake 4x notifications to social-engineer the user into accepting merges or downloads | local_user | macOS NSDistributedNotificationCenter | Dashboard HTTP server | medium | possible | unmitigated | none | |
| T10 | State corruption via concurrent access — batch/MCP/manual runs on same feature with no file locking, read-modify-write races on state.json overwrite in-flight transitions | local_user | YAML/JSON deserialization, Subprocess spawn (exec.Command) | Feature state, Process integrity | high | likely | partially_mitigated | atomic write (temp+rename) for some files; ProcessAlive guard in run.go but not batch.go | |
| T11 | macOS app WebView JS injection via nativeOpenFolder path with insufficient escaping — backslash or other metacharacters can break out of string context | local_user | macOS WKWebView bridge (nativeOpenFolder) | Source code integrity | high | rare | partially_mitigated | single-quote escaping (incomplete) | |

## 5. Deprioritized

| threat | reason |
|---|---|
| SQL injection | No `database/sql` or ORM usage in the codebase. Not applicable. |
| Repudiation on CLI operations | Single-user local tool; all actions are by the authenticated local user. No multi-user audit trail requirement. |
| Denial of service via resource exhaustion on HTTP server | Localhost-only server; an attacker with local process access has simpler avenues (kill the process, exhaust disk). Low additional risk from HTTP-based DoS. |
| Network-based remote attack on dashboard | Server binds to `127.0.0.1` only; not reachable from the network without explicit port forwarding. Remote attack requires prior local access. |

## 6. Open questions

- **Deployment context**: Is the dashboard server ever exposed beyond localhost (e.g., via SSH tunnel, Docker port mapping, reverse proxy)? If so, T2 and T5 escalate from `likely` to `almost_certain`.
- **Runner trust model**: Are the AI runner subprocesses (Claude Code, Codex, etc.) considered trusted or untrusted? If untrusted, T1 (test-strategy.yaml → sh -c) is a critical gap. If trusted, the risk is residual (prompt injection from external code context).
- **Multi-user scenarios**: Is 4x ever used in a shared-machine context (e.g., CI server, shared dev VM)? The unauthenticated API (T2) and world-readable temp files (T8) become critical if multiple users share the host.
- **Homebrew tap token rotation**: When was `HOMEBREW_TAP_GITHUB_TOKEN` last rotated? What scope does it have — is it a fine-grained PAT limited to the tap repo, or a classic token with broader access?
- **macOS entitlement necessity**: Does the app actually need `allow-unsigned-executable-memory`? WKWebView's JIT should work without it on recent macOS. Removing it would re-enable W^X enforcement.
- **install.sh adoption**: How widely is `curl | sh` used vs. Homebrew/binary downloads? If most users use Homebrew, T4's install.sh vector is lower priority.

## 7. Provenance

- mode: bootstrap
- date: 2026-06-18
- target: /Users/white/github/4x @ c490c88
- inputs: git-log + CHANGELOG mined
- owner: unset

## 8. Recommended mitigations

| mitigation | threat_ids | closes_class | effort |
|---|---|---|---|
| Add per-session bearer token authentication to the HTTP server (generate random token at startup, pass to dashboard client via stdout/launch arg) | T2, T5 | yes | M |
| Allowlist or sandbox verify command execution — reject commands not matching known patterns, or execute in a restricted shell/container | T1 | yes | M |
| Pin all third-party GitHub Actions to commit SHA; add checksum verification to install.sh; consider cosigning releases with Sigstore | T4 | yes | S |
| Call `filepath.EvalSymlinks` before the `HasPrefix` containment check in screenshot serve | T6 | yes | S |
| Filter sensitive environment variables (`*_KEY`, `*_TOKEN`, `*_SECRET`, `AWS_*`) from child process environment; set restrictive permissions (0600) on prompt temp files | T8 | partial | S |
| Harden guardrails: fail-closed on any YAML/state error; detect untracked files in all gitops code paths; validate feature YAML integrity before scope checks | T3 | partial | M |
| Add `ProcessAlive` guard to batch run path; use signal files instead of direct state.json mutation for MCP stop operations | T10 | partial | S |
| Pin server to a fixed port or use a lockfile with the chosen port; warn user if the expected port is occupied instead of silently shifting | T7 | yes | S |
| Restrict `NSDistributedNotificationCenter` subscription to a specific sender object, or switch to `DistributedNotificationCenter` with sender verification | T9 | yes | S |
