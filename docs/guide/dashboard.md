# 4x Live Dashboard

Real-time monitoring of your AI development loop.

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

## Multi-Project Support

The dashboard supports multiple projects simultaneously. Without path arguments, it loads from `~/.4x/recent-projects.json` (LRU, max 20 entries).

The project tab bar ends with two actions: **Add Project** (folder-plus icon) and **Global Settings** (gear icon). The sidebar header carries the active project's **Project Settings** gear and, next to it, a **Clean** button (trash icon). Clicking Clean opens a confirmation dialog warning that cleaned features lose their detailed logs, reports, and round history in the dashboard (feature definitions and status are preserved); confirming calls [`POST /api/clean`](#post-apiclean) for the whole project and shows a toast with the result.

## Feature Cards

Each feature card shows tags for its priority, dependencies, stop reason (if the feature halted abnormally), and — when a non-default [pipeline profile](concepts.md#pipeline-profiles) is active — a **profile tag** (e.g. `quick`, `normal`). High-priority features (P0/P1) get accent borders. Completed dependencies show a green checkmark. The `profile`, `stopReason`, and `stopMessage` fields are carried in the `/api/tasks` JSON. `stopReason` is a short category code (e.g. `runner-error`, `guard-fail`, `no-progress`) used for color-coding; `stopMessage` is the human-readable detail shown below the category label.

## New Feature Form

The **New Feature** modal is a progressive form. The basic area always shows **Name** (required), **Description** (optional, defaults to the name), and a **Priority** select (P0–P3 or none). An **Advanced** toggle reveals **Custom ID** (leave empty to auto-generate), **Depends** (comma-separated feature IDs), **Rules** (comma-separated), and a dynamic **Subtasks** list (add/remove rows of id + name). Submitting `POST`s to [`/api/new`](#rest); the CLI `4x new` and the dashboard now share a single creation path (`feature.Create`, see [Concepts](concepts.md#feature-creation)), so both honor the same flags/fields and ID generation.

## Dependency DAG

The overview renders a dependency graph of all features as inline SVG — no external charting library (d3, mermaid, chart.js) is loaded. Features are laid out in layers by dependency depth; edges run from each feature to the features it depends on. Node color follows phase status: green = done, blue = running (active run or an in-progress phase such as coding/reviewing/testing), gray = todo, red = blocked / needs-attention. Clicking a node opens that feature's detail, the same path as clicking a feature card. The graph is rebuilt from the cached `/api/tasks` data on every polling cycle, so colors update live as features advance.

## Batch Panel

The overview also hosts a batch control panel backed by the [Batch Control API](#batch-control). It shows **Start / Stop / Continue Batch** buttons (Start is confirmed before launching), a running indicator, the scheduled queue with per-feature progress (done check, running marker, or waiting position), and — when a merge conflict pauses the batch — a conflict card listing the feature, repo, and conflicting files alongside the Continue Batch action. The panel refreshes from `GET /api/batch/status` on the same polling loop as the rest of the dashboard.

## Server API

The dashboard exposes REST and SSE endpoints:

Read-heavy endpoints (`/api/tasks`, `/api/overview`, `/api/projects`, `/api/settings`, …) are served through a `*protocol.CachedWorkspace` rather than a plain `*protocol.Workspace`. Because the server is long-running, this mtime-based in-memory cache avoids re-parsing every feature YAML and `settings.json` on each request — see [Workspace Read Cache](concepts.md#workspace-read-cache-dashboard-server). Cache invalidation is automatic: a write (via the dashboard or a runner) changes the file mtime, so the next read re-parses transparently.

### REST

| Endpoint | Method | Description |
|---|---|---|
| `/api/tasks` | GET | List all features (includes `warnings` array when a feature YAML has format issues) |
| `/api/new` | POST | Create a new feature (accepts `name`, `description`, plus optional `customId`, `priority`, `depends`, `rules`, `subtasks`) |
| `/api/run` | POST | Start a feature run (spawns `4x run` subprocess) |
| `/api/stop` | POST | Stop a running feature |
| `/api/done` | POST | Mark feature as done; auto-merges worktree if present (multi-repo: all-or-nothing) |
| `/api/clean` | POST | Remove workspace artifacts for all cleanable (done/abandoned) features in the project |
| `/api/runs` | GET | List active runs |
| `/api/batch/start` | POST | Start a batch run (`4x batch run` subprocess); 409 if a batch conflict is unresolved |
| `/api/batch/stop` | POST | Gracefully stop the batch (writes `.4x/batch-stop`) |
| `/api/batch/continue` | POST | Clear the conflict signal and restart the batch (after resolving in the worktree) |
| `/api/batch/status` | GET | Batch running state, scheduled queue, current feature, and conflict signal |
| `/api/events/{id}` | GET | Get events for a feature |
| `/api/overview/{id}` | GET | Get feature overview (YAML fields + spec/plan content, resolved via the shared `protocol.ResolveDesignDoc` — see [Design Doc Resolution](concepts.md#design-doc-resolution)) |
| `/api/messages/{id}` | GET | Get messages for a feature |
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
| `/api/user-config` | GET | Get user config (`~/.4x/settings.json`) |
| `/api/user-config` | PUT | Update user config (backs up to `.bak`, then writes) |
| `/api/merged-config` | GET | Read-only view of project + user merged effective config |
| `/api/locales` | GET | 回傳支援的 locale 清單 |
| `/api/locales/{lang}` | GET | 回傳對應語言的翻譯 JSON |
| `/api/supported-runners` | GET | List supported runner names |

#### `POST /api/done` Response

Returns HTTP 200 in the normal case. The `status` field is `"done"` only after the state transition succeeds. If merge conflict or merge error occurs, `status` remains `"pending-review"`. Additional fields indicate merge result:

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

Removes the `.4x/{feature-id}/` workspace artifacts (logs, `rounds/`, reports, `state.json`, `events.jsonl`) for **every** cleanable feature in the project in one call — the same set `4x clean` would clean: `done`/`abandoned`, not active, with an existing workspace directory. Feature definitions (`.4x/features/*.yaml`) are preserved, so cleaned features still show in listings with their final status. See [Workspace Cleanup](concepts.md#workspace-cleanup) for the underlying protocol functions.

Non-`POST` requests return **HTTP 405**. Each feature is cleaned independently; one that fails (e.g. a race makes it active) is skipped without aborting the rest. The handler always returns HTTP 200 with:

| Field | Type | Meaning |
|---|---|---|
| `cleaned` | int | Number of features whose artifacts were removed |
| `freed` | int64 | Total bytes freed |
| `freed_human` | string | `freed` formatted human-readably (e.g. `38M`) |
| `features` | string[] | IDs of the cleaned features (`[]` when nothing was cleaned) |

When there is nothing to clean the response is `{"cleaned":0,"freed":0,"freed_human":"0B","features":[]}`.

#### Batch Control

The dashboard can drive a batch run end-to-end without dropping back to the terminal. A dedicated `BatchManager` (separate from the per-feature `ProcessManager`) owns the single `4x batch run` subprocess for a project — only one batch may run at a time.

- **Start** (`POST /api/batch/start`) — the UI confirms first to avoid accidental launches, then starts the run. If `.4x/batch-conflict.json` still exists, the endpoint returns **HTTP 409** so a stale conflict must be resolved or continued first. The request body may carry `{runner, maxRounds}`; omitted fields fall back to the merged project/user config.
- **Stop** (`POST /api/batch/stop`) — writes `.4x/batch-stop` for a graceful stop (the batch finishes the current feature, then exits). It does **not** kill the subprocess.
- **Continue** (`POST /api/batch/continue`) — clears `.4x/batch-conflict.json`, then restarts the batch. Use after resolving the conflict in the worktree.
- **Status** (`GET /api/batch/status`) — returns the running flag, the scheduled queue, the current feature, the conflict signal (or `null`), and `lastReport` (the parsed `.4x/batch-report.json`, or omitted when no report exists):

  ```json
  {
    "running": true,
    "queue": [
      {"featureId": "F001-auth", "name": "Auth", "status": "done", "state": "done", "position": 0},
      {"featureId": "F002-api", "name": "API", "status": "coding", "state": "running", "position": 1}
    ],
    "currentFeature": "F002-api",
    "conflict": null,
    "lastReport": null
  }
  ```

  The queue is built from `batch.PlanBatch` so it honors the same dependency-and-priority ordering as the CLI. Each item's `state` is `done` (feature done / ready-for-review), `running` (an active run that isn't done), `error` (blocked / needs-attention), or `waiting`; `position` numbers the unfinished items (excludes `done` and `error`).

  `lastReport` carries the most recent batch run's report (`outcome`, counts, runner, duration, and per-feature breakdown — see [Batch Mode](batch.md#run-report)). When no batch is running, the panel renders it as a "last batch report" summary card that expands to per-feature detail; for a `crashed` outcome it also surfaces the `panicMessage`.

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

The leaf routes (`/api/tasks`, `/api/settings`, `/api/run`, `/api/batch/*`, `/sse/events/...`, …) are defined **once** in `NewMux` (`internal/server/server.go`). Rather than binding a fixed workspace, `NewMux` takes a `WorkspaceResolver` — a function that, given the incoming request, returns the target `*protocol.CachedWorkspace`, its `*ProcessManager`, and its `*BatchManager` (or an error). Each data-backed handler calls the resolver first; routes that need none of them (`/api/user-config`, `/api/supported-runners`, `/api/locales`, static assets) skip it. This removes the ~150 lines of duplicated handler registration that single- and multi-project mode previously each carried.

Two resolvers back the two modes:

- **`singleResolver(ws, pm)`** — single-project mode (`server.Start`). Closes over one workspace and always returns the same `ws`/`pm`/`bm` triple.
- **`multiResolver(reg)`** — multi-project mode (`NewMultiMux`). Resolution is a three-step flow:
  1. **Prefix dispatch (outer mux).** `NewMultiMux` registers `/api/project/` and `/sse/project/` handlers that strip the `/api/project/{id}` (or `/sse/project/{id}`) prefix, look up the entry via `getEntry(id)` (unknown id → **404**), rewrite `r.URL.Path` to the remaining sub-path, inject the resolved entry into the request `context`, and forward to the shared inner `NewMux` handler. The prefix strip must happen in the outer mux because `http.ServeMux` selects the handler **before** it runs — an un-stripped `/api/project/{id}/api/tasks` would only ever match the static `/` route.
  2. **Context read.** Inside the inner handler, `multiResolver` first checks the request context for the entry injected in step 1 and returns it directly when present.
  3. **No-prefix compat.** When no entry was injected (an unprefixed path), it falls back on `reg.Count()`: `0` → **400** `no projects loaded`, `1` → that sole project, `≥2` → **400** `multiple projects loaded — use /api/project/{id}…`.

`NewMultiMux` itself only registers the global endpoints (`/api/projects`, `/api/projects/`, `/api/browse`) plus the two prefix dispatchers and a catch-all that forwards to the single shared `inner := NewMux(multiResolver(reg))`. Adding a project no longer builds a per-entry mux; `registryEntry` carries just `id`/`ws`/`pm`/`bm`.

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
