# 4x Live Dashboard

Real-time monitoring of your AI development loop.

## macOS Gatekeeper

Unsigned builds of the 4x Live app (produced by `make package-macos` without a Developer ID) are ad-hoc signed only. macOS blocks them on first launch. A signed **and notarized** release (see [macOS release build](#macos-release-build) below) opens without any Gatekeeper prompt.

If you have an unsigned build, allow it via either option:

**Option A: Remove quarantine attribute (recommended)**

```bash
xattr -cr /Applications/4x\ Live.app
```

**Option B: Allow via System Settings**

1. Double-click the app — macOS shows "cannot be opened because the developer cannot be verified"
2. Open **System Settings → Privacy & Security**
3. Scroll down to the **Security** section — you'll see a message about the blocked app
4. Click **Open Anyway**, then enter your password or use Touch ID
5. macOS remembers your choice for future launches

## macOS release build

`make dashboard-release` produces a signed and notarized `.dmg` in one shot: cross-compile the darwin binaries → package the `.app`/`.dmg` → sign with a Developer ID Application certificate → submit for Apple notarization → staple the ticket. The end result opens with no Gatekeeper prompt.

Local development is unaffected: `make package-macos` still builds an ad-hoc–signed `.dmg` with no certificate required.

### Prerequisites

- An Apple Developer Program account with a **Developer ID Application** certificate installed in your login keychain (verify with `security find-identity -v -p codesigning`).
- Notarization credentials — either an **App Store Connect API key** (recommended) or an **Apple ID + app-specific password**.

### Configuration

Certificates and credentials are read from environment variables or a `.env` file at the repo root — never committed (`.env` is git-ignored). Copy `.env.example` to `.env` and fill in your values:

```bash
cp .env.example .env
```

| Variable | Purpose |
|---|---|
| `CODESIGN_IDENTITY` | Developer ID Application certificate name (e.g. `Developer ID Application: Your Name (TEAMID)`). Unset → ad-hoc signing. |
| `NOTARY_KEY_PATH` / `NOTARY_KEY_ID` / `NOTARY_ISSUER_ID` | App Store Connect API key (`.p8` path, Key ID, Issuer ID). |
| `NOTARY_APPLE_ID` / `NOTARY_PASSWORD` / `NOTARY_TEAM_ID` | Alternative: Apple ID email, app-specific password, Team ID. |

Provide **one** of the two notarization credential sets; the API key takes precedence if both are present.

### Run

```bash
make dashboard-release
```

The notarization submit step (`xcrun notarytool submit --wait`) blocks until Apple returns a verdict — usually a few minutes. On success the ticket is stapled to `dist/4x-Live.dmg`.

### CI (GitHub Actions) release

The `Release` workflow (`.github/workflows/release.yml`) runs `make dashboard-release` on its `macos` job, so tagged releases ship a signed and notarized `.dmg`. Configure these repository secrets (Settings → Secrets and variables → Actions):

| Secret | Purpose |
|---|---|
| `MACOS_CERTIFICATE_P12` | Base64 of the Developer ID Application certificate exported as `.p12` (`base64 -i cert.p12 \| pbcopy`). |
| `MACOS_CERTIFICATE_PASSWORD` | Password protecting the `.p12`. |
| `MACOS_CODESIGN_IDENTITY` | Certificate name passed as `CODESIGN_IDENTITY` (e.g. `Developer ID Application: Your Name (TEAMID)`). |
| `NOTARY_KEY_P8` | Base64 of the App Store Connect API key `.p8`. |
| `NOTARY_KEY_ID` | API key ID. |
| `NOTARY_ISSUER_ID` | API key issuer ID. |

The workflow imports the certificate into a temporary keychain and writes the API key to a runner-temp file, then deletes both after packaging — secrets are passed through environment variables, never echoed to logs, and never persist on the runner.

## Starting the Dashboard

```bash
# Start with recent projects
4x live

# Open specific projects
4x live /path/to/project1 /path/to/project2

# Custom port
4x live -p 8080

# Auto-open in browser
4x live -w

# Open macOS native app
4x live -a
```

**Authentication (bearer token).** On top of binding to `127.0.0.1`, `4x live` adds a
per-session bearer-token layer, enabled by default (secure by default). At startup a random
64-hex-char token is written to `~/.4x/live-token` (permissions `0600`, atomically replaced so an
existing wider-permission file is narrowed). Requests to `/api/*` and `/sse/*` (both reads and
writes) must present the token, either as an `Authorization: Bearer <token>` header or, for
`EventSource`/SSE which cannot set headers, as a `?token=<token>` query parameter; missing or wrong
tokens get `401`. Public exemptions (reachable without a token) are static assets, `/api/version`,
and `/api/locales` / `/api/locales/…`. The browser receives the token via the launch URL
(`http://localhost:PORT/?token=…`, Jupyter-style); the web frontend reads it from `window.location`,
keeps it in memory, and immediately strips it from the address bar with `history.replaceState`. The
macOS and Tauri shells read the token from `~/.4x/live-token`. To opt out, set `"dashboard_auth": false`
in `~/.4x/settings.json` (a `*bool`: unset/`null` = enabled, `false` = disabled, `true` = enabled);
when disabled no token is generated and no token file is written.

### Port source of truth

The default port (`4567`) has a single source of truth: `internal/server.DefaultPort`. `cmd/4x/live.go`'s `--port` flag default reads this constant; the macOS shell (`main.swift`'s `serverPort`) and the Tauri shell (`main.rs`'s `DEFAULT_PORT`) each hold their own local copy (cross-language builds can't reference the Go constant directly), kept in sync by `internal/server/port_sync_test.go`, which fails `make test` if any of the three literals drift.

## Multi-Project Support

The dashboard supports multiple projects simultaneously. Without path arguments, it loads from `~/.4x/recent-projects.json` (LRU, max 20 entries).

The project tab bar ends with two actions: **Add Project** (folder-plus icon) and **Global Settings** (gear icon). The sidebar header carries the active project's **Project Settings** gear and, next to it, a **Clean** button (trash icon). Clicking Clean opens a confirmation dialog warning that cleaned features lose their detailed logs, reports, and round history in the dashboard (feature definitions and status are preserved); confirming calls [`POST /api/clean`](#post-apiclean) for the whole project and shows a toast with the result.

## Feature Cards

Each feature card shows tags for its priority, dependencies, stop reason (if the feature halted abnormally), and — when a non-default [pipeline profile](concepts.md#pipeline-profiles) is active — a **profile tag** (e.g. `quick`, `normal`). High-priority features (P0/P1) get accent borders. Completed dependencies show a green checkmark. The `profile`, `stopReason`, and `stopMessage` fields are carried in the `/api/tasks` JSON. `stopReason` is a short category code (e.g. `runner-error`, `guard-fail`, `no-progress`) used for color-coding; `stopMessage` is the human-readable detail shown below the category label.

The phase label of a running feature is refined by `taskInfo.subPhase` (carried in the `/api/tasks` JSON, omitted when empty). While the feature is in `deep-reviewing`, the header appends the active sub-step so the progress reads `deep-reviewing (reviewing)`, `deep-reviewing (synthesizing)`, `deep-reviewing (fixing)`, or `deep-reviewing (reverifying)` instead of a bare `deep-reviewing`. The suffix only renders for `deep-reviewing`; any other phase ignores `subPhase`.

## New Feature Form

The **New Feature** modal is a progressive form. The basic area always shows **Name** (required), **Description** (optional, defaults to the name), and a **Priority** select (P0–P3 or none). An **Advanced** toggle reveals **Custom ID** (leave empty to auto-generate), **Depends** (comma-separated feature IDs), **Rules** (comma-separated), and a dynamic **Subtasks** list (add/remove rows of id + name). Submitting `POST`s to [`/api/new`](#rest); the CLI `4x new` and the dashboard now share a single creation path (`feature.Create`, see [Concepts](concepts.md#feature-creation)), so both honor the same flags/fields and ID generation.

The **Project Settings** modal complements the raw Form/JSON editors with three structured sections that write through dedicated endpoints (no full-file replace): **Defaults** (the `default_runner` / `default_profile` selectors, saved via `PATCH /api/settings`), **Profiles** (list, add, edit with a drag-sortable phase picker, and delete, via `PUT`/`DELETE /api/settings/profiles/{name}`), and **Roles** (per-role `model`, plus `deep_model` for reviewer/deep-reviewer, `screenshot_dir` for tester, and comma-separated `instructions` / `includes`, saved via `PUT /api/settings/roles/{role}`). Edits are reflected immediately without restarting the server, and all section labels and messages are localized.

## Dependency DAG

The overview renders a dependency graph of all features as inline SVG — no external charting library (d3, mermaid, chart.js) is loaded. Features are laid out in layers by dependency depth; edges run from each feature to the features it depends on. Node color follows phase status: green = done, blue = running (active run or an in-progress phase such as coding/reviewing/testing/fixing), gray = todo, red = blocked / needs-attention. Clicking a node opens that feature's detail, the same path as clicking a feature card. The graph is rebuilt from the cached `/api/tasks` data on every polling cycle, so colors update live as features advance.

## Server API

The dashboard exposes REST and SSE endpoints:

Read-heavy endpoints (`/api/tasks`, `/api/overview`, `/api/projects`, `/api/settings`, …) are served through a `*protocol.CachedWorkspace` rather than a plain `*protocol.Workspace`. Because the server is long-running, this mtime-based in-memory cache avoids re-parsing every feature YAML and `settings.json` on each request — see [Workspace Read Cache](concepts.md#workspace-read-cache-dashboard-server). Cache invalidation is automatic: a write (via the dashboard or a runner) changes the file mtime, so the next read re-parses transparently.

### REST

| Endpoint | Method | Description |
|---|---|---|
| `/api/tasks` | GET | List all features (includes `warnings` array when a feature YAML has format issues; `done`/`abandoned` features include `costUsd`, the feature's total cost via `protocol.Workspace.TotalCost`) |
| `/api/new` | POST | Create a new feature (accepts `name`, `description`, plus optional `customId`, `priority`, `depends`, `rules`, `subtasks`) |
| `/api/run` | POST | Start a feature run (spawns `4x run` subprocess) |
| `/api/stop` | POST | Stop a running feature |
| `/api/done` | POST | Mark feature as done; auto-merges worktree if present (multi-repo: all-or-nothing). The state write goes through the per-feature `state.json` lock, so it is serialized with an in-progress `4x run` and neither clobbers the other |
| `/api/clean` | POST | Remove workspace artifacts for all cleanable (done/abandoned) features in the project |
| `/api/runs` | GET | List active runs |
| `/api/events/{id}` | GET | Get events for a feature |
| `/api/overview/{id}` | GET | Get feature overview (YAML fields + spec/plan content, resolved via the shared `protocol.ResolveDesignDoc` — see [Design Doc Resolution](concepts.md#design-doc-resolution)) |
| `/api/messages/{id}` | GET | Get messages for a feature, plus the feature's authoritative total cost (see below) |
| `/api/evolve-report` | GET | Latest `4x evolve` round summary (`.4x/evolve-report.md`); `{content, exists}`, `exists:false` when absent |
| `/api/features/{id}/screenshots` | GET | Get screenshots grouped by round |
| `/api/features/{id}/screenshots/{filename}` | GET | Serve one screenshot image |
| `/api/logs/{id}` | GET | List log files for a feature |
| `/api/logs/{id}/{file}` | GET | Get a specific log file |
| `/api/projects` | GET | List registered projects |
| `/api/projects` | POST | Add a project (supports `init: true` for on-the-fly initialization) |
| `/api/projects/{id}` | DELETE | Remove a project |
| `/api/browse` | GET | Folder picker |
| `/api/settings` | GET | Get project settings (`.4x/settings.json`) |
| `/api/settings` | PUT | Update project settings (validates, backs up, writes) |
| `/api/settings` | PATCH | Partial update — merges only the fields present in the body (e.g. `default_runner`, `default_profile`, `roles`); type-mismatched or invalid payloads return **400** |
| `/api/settings/profiles/{name}` | PUT | Create or overwrite a single pipeline profile |
| `/api/settings/profiles/{name}` | DELETE | Delete a profile; clears `default_profile` if it pointed at the removed profile |
| `/api/settings/roles/{role}` | PUT | Create or overwrite a single role config (model, deep_model, screenshot_dir, instructions, includes) |
| `/api/user-config` | GET | Get user config (`~/.4x/settings.json`) |
| `/api/user-config` | PUT | Update user config (backs up to `.bak`, then writes) |
| `/api/merged-config` | GET | Read-only view of project + user merged effective config |
| `/api/locales` | GET | 回傳支援的 locale 清單 |
| `/api/locales/{lang}` | GET | 回傳對應語言的翻譯 JSON |
| `/api/supported-runners` | GET | List supported runner names |

GET-only endpoints (including `/api/messages/{id}` and `/api/overview/{id}`) reject non-`GET` requests with **HTTP 405 Method Not Allowed**.

#### `GET /api/messages/{id}` Response

Returns a JSON object (not a bare array):

| Field | Type | Meaning |
|---|---|---|
| `messages` | array \| null | Same message entries as before; `null` when the feature has no artifacts yet |
| `totalCostUSD` | float | Sum of `cost_usd` across every `run-end` event in the feature's `events.jsonl` (via `protocol.Workspace.TotalCost`), covering all past runs including interrupted/resumed ones |

Each message entry additionally carries an optional `codex` object (`{"primary_pct":…,"secondary_pct":…}`) mirroring the codex runner's live rate-limit usage for that round, sourced from the round's `run-end` event. The field is omitted entirely (via `omitempty`) for rounds with no codex observation (claude runners, or codex rounds where rate limits could not be parsed), so the dashboard renders a `NN%` tag (the higher of the two windows, rounded) only when the field is present — never a misleading `0%`. For the deep-reviewer's parallel sub-reviewers, the aggregated entry deterministically reports the most-constrained sample (highest `primary_pct`, ties broken by `secondary_pct`).

The dashboard surfaces this per-feature total; for a per-role / per-round breakdown on the command line (across all features or one feature), use `4x cost` (see `docs/guide/cli.md`), which reads the same run logs (`logs/*.stream.jsonl`, with `events.jsonl` as fallback).

#### `POST /api/done` Response

Returns HTTP 200 in the normal case. The `status` field is `"done"` only after the state transition succeeds. If merge conflict or merge error occurs, `status` remains `"pending-review"`. Error cases (invalid JSON, missing `id`, feature not found, wrong phase, internal failures) return their HTTP status (400/404/500) with a JSON body `{"error":"..."}` and `Content-Type: application/json`, so clients can parse all responses as JSON uniformly. Additional fields indicate merge result:

| Field | Type | Meaning |
|---|---|---|
| `merged` | bool | `true` if branch was merged and worktree cleaned up |
| `merged` | bool | `false` if no worktree existed (state-only transition) |
| `merge_conflict` | bool | `true` if merge had conflicts; worktree preserved |
| `merge_error` | string | Merge error message; feature remains pending-review |
| `conflicts` | string[] | List of conflicting files (only present when `merge_conflict: true`) |

After a conflict, resolve the files in the worktree and run `4x merge <id>` to complete.

If the feature's phase changes during the merge (a runner or background reconciler updated `state.json` while the merge was running), the endpoint returns **HTTP 409 Conflict** with `{"status":"<currentPhase>","error":"state changed during merge"}` and does not perform the done transition — this guards against overwriting a newer state with a stale pre-merge snapshot.

#### `POST /api/clean`

Removes the `.4x/run/{feature-id}/` workspace artifacts (logs, `rounds/`, reports, `state.json`, `events.jsonl`) for **every** cleanable feature in the project in one call — the same set `4x clean` would clean: `done`/`abandoned`, not active, with an existing workspace directory. Feature definitions (`.4x/features/*.yaml`) are preserved, so cleaned features still show in listings with their final status. See [Workspace Cleanup](concepts.md#workspace-cleanup) for the underlying protocol functions.

Non-`POST` requests return **HTTP 405**. Each feature is cleaned independently; one that fails (e.g. a race makes it active) is skipped without aborting the rest. The handler always returns HTTP 200 with:

| Field | Type | Meaning |
|---|---|---|
| `cleaned` | int | Number of features whose artifacts were removed |
| `freed` | int64 | Total bytes freed |
| `freed_human` | string | `freed` formatted human-readably (e.g. `38M`) |
| `features` | string[] | IDs of the cleaned features (`[]` when nothing was cleaned) |

When there is nothing to clean the response is `{"cleaned":0,"freed":0,"freed_human":"0B","features":[]}`.

### Screenshots Tab

Feature detail includes a **Screenshots** tab when screenshots exist for that feature. Screenshots are grouped by round, displayed as thumbnails, and can be opened in a lightbox with left/right navigation and ESC-to-close.

### SSE (Server-Sent Events)

| Endpoint | Description |
|---|---|
| `/sse/events/{id}` | Stream events for a feature (1-second polling) |
| `/sse/logs/{id}` | Stream the feature's active log files (one or more) |

The event stream tracks a byte offset into `events.jsonl` and only sends newly appended lines. If the file is **truncated or rotated** — for example `4x transition --to init` resets the feature and rewrites `events.jsonl` from scratch — the new file size drops below the tracked offset. The stream detects this (`size < lastOffset`), resets the offset to 0, and re-reads the whole file from the beginning so the client recovers instead of silently stalling forever. A size equal to the offset still means "no new content" and is skipped.

The log stream (`/sse/logs/{id}`) likewise tracks a byte offset and only sends newly appended content. To avoid per-tick garbage, it reuses a single fixed 32KB read buffer allocated once per connection instead of allocating a new buffer sized to each delta. On every tick it loop-reads from the offset to EOF; a delta larger than 32KB is split across several SSE messages, each carrying the same `{"file": "...", "content": "..."}` payload. The client appends content as it arrives, so splitting is transparent. When `size <= lastOffset` (no new content) the tick is skipped without opening the file.

When several roles write logs at the same time — parallel deep-review sub-reviewers, or the concurrent reviewer + tester — the stream tails **all** currently active logs instead of just the most recently modified one. Without a `?file=` query parameter it tracks every log whose mtime falls within a recent window (each with its own offset), and the per-message `file` field lets the client route content into the matching pane. Pass `?file=<name>` to pin the stream to a single log.

### Completion Notifications

On each SSE tick the dashboard reads the latest event from `/api/events/{id}` and, when it carries a `notify` hint (`run-end` success, `guard-fail`, or `escalation`), raises a native OS notification. The dispatcher picks the right channel for the environment: the macOS native app uses a `nativeNotify` WebKit bridge, the Tauri shell invokes a `notify` command backed by `tauri-plugin-notification`, and a plain browser uses the Web Notification API (after requesting permission). Unsupported or unpermitted environments degrade silently. Notification text is localized via the `notifications.*` i18n keys.

### Multi-Project Routing

With multiple projects, endpoints are prefixed with `/api/project/{project-id}/...` and `/sse/project/{project-id}/...`. Single-project mode uses the unprefixed paths for backward compatibility.

#### Workspace Resolution

The leaf routes (`/api/tasks`, `/api/settings`, `/api/run`, `/sse/events/...`, …) are defined **once** in `NewMux` (`internal/server/server.go`). Rather than binding a fixed workspace, `NewMux` takes a `WorkspaceResolver` — a function that, given the incoming request, returns the target `*protocol.CachedWorkspace` and its `*ProcessManager` (or an error). Each data-backed handler calls the resolver first; routes that need none of them (`/api/user-config`, `/api/supported-runners`, `/api/locales`, static assets) skip it. This removes the ~150 lines of duplicated handler registration that single- and multi-project mode previously each carried.

Two resolvers back the two modes:

- **`singleResolver(ws, pm)`** — single-project mode (`server.Start`). Closes over one workspace and always returns the same `ws`/`pm` pair.
- **`multiResolver(reg)`** — multi-project mode (`NewMultiMux`). Resolution is a three-step flow:
  1. **Prefix dispatch (outer mux).** `NewMultiMux` registers `/api/project/` and `/sse/project/` handlers that strip the `/api/project/{id}` (or `/sse/project/{id}`) prefix, look up the entry via `getEntry(id)` (unknown id → **404**), rewrite `r.URL.Path` to the remaining sub-path, inject the resolved entry into the request `context`, and forward to the shared inner `NewMux` handler. The prefix strip must happen in the outer mux because `http.ServeMux` selects the handler **before** it runs — an un-stripped `/api/project/{id}/api/tasks` would only ever match the static `/` route.
  2. **Context read.** Inside the inner handler, `multiResolver` first checks the request context for the entry injected in step 1 and returns it directly when present.
  3. **No-prefix compat.** When no entry was injected (an unprefixed path), it falls back on `reg.Count()`: `0` → **400** `no projects loaded`, `1` → that sole project, `≥2` → **400** `multiple projects loaded — use /api/project/{id}…`.

`NewMultiMux` itself only registers the global endpoints (`/api/projects`, `/api/projects/`, `/api/browse`) plus the two prefix dispatchers and a catch-all that forwards to the single shared `inner := NewMux(multiResolver(reg))`. Adding a project no longer builds a per-entry mux; `registryEntry` carries just `id`/`ws`/`pm`.

## Keyboard Shortcuts

| Shortcut | Action |
|---|---|
| `Cmd+K` | Search |
| `Cmd+,` | Project Settings (in project) / Global Settings (on home) |
| `Cmd+Shift+,` | Global Settings |
| `Esc` | Close current modal |

## Process Management

The dashboard manages runner subprocesses:

- Respects `max_concurrent_runs` from project config
- Captures stdout/stderr as run-output/run-error events
- Graceful shutdown: SIGTERM → 5 seconds → SIGKILL

When a runner subprocess exits, the server marks the feature inactive (`Active=false`, `StopReason=process-exit`). This is guarded against a race: a runner may write its own final `state.json` (e.g. `pending-review`) just before exiting. The server records the process exit time and, before overwriting, re-reads the state — if `state.json` was updated **at or after** the exit time (`UpdatedAt >= endTime`), the runner's final state is kept and the inactive write is skipped. This prevents the server from reverting a freshly-written phase or clobbering its `StopReason` with a stale in-memory snapshot.

## Shared Web Frontend

The dashboard UI (HTML/CSS/JS + locale JSON) lives in a single source of truth at `dashboard/web/` and is embedded into the `4x` binary via `dashboard/web/embed.go` (`web.Assets embed.FS`). The Go server (`internal/server/server.go`, `internal/server/multi.go`) serves static assets and locale files directly from `web.Assets`, so the same frontend backs every platform shell — the Go-served web UI, the macOS WKWebView, and the Tauri webview. There is no per-platform UI copy to keep in sync.

## Platforms

| Platform | Shell | Packaging |
|---|---|---|
| Web UI (embedded) | Go server serves `web.Assets` | `4x live` |
| macOS native | Swift WKWebView, auto-launches the bundled `4x live` server | universal `.dmg` (`make package-macos`) |
| Windows | Tauri v2 webview, `4x` sidecar | `.msi` (`dashboard/tauri`) |
| Linux | Tauri v2 webview, `4x` sidecar | `.AppImage` (`dashboard/tauri`) |

All desktop shells load the same `dashboard/web/` frontend over `http://localhost:<port>` backed by the embedded `4x` server. The CI matrix in `.github/workflows/desktop.yml` cross-compiles the per-platform `4x` binary and produces the `.dmg` / `.msi` / `.AppImage` artifacts.
