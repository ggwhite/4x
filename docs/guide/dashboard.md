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

## Feature Cards

Each feature card shows tags for its priority, dependencies, stop reason (if the feature halted abnormally), and — when a non-default [pipeline profile](concepts.md#pipeline-profiles) is active — a **profile tag** (e.g. `quick`, `normal`). High-priority features (P0/P1) get accent borders. Completed dependencies show a green checkmark. The `profile` and `stopReason` fields are carried in the `/api/tasks` JSON.

## Dependency DAG

The overview renders a dependency graph of all features as inline SVG — no external charting library (d3, mermaid, chart.js) is loaded. Features are laid out in layers by dependency depth; edges run from each feature to the features it depends on. Node color follows phase status: green = done, blue = running (active run or an in-progress phase such as coding/reviewing/testing), gray = todo, red = blocked / needs-attention. Clicking a node opens that feature's detail, the same path as clicking a feature card. The graph is rebuilt from the cached `/api/tasks` data on every polling cycle, so colors update live as features advance.

## Batch Panel

The overview also hosts a batch control panel backed by the [Batch Control API](#batch-control). It shows **Start / Stop / Continue Batch** buttons (Start is confirmed before launching), a running indicator, the scheduled queue with per-feature progress (done check, running marker, or waiting position), and — when a merge conflict pauses the batch — a conflict card listing the feature, repo, and conflicting files alongside the Continue Batch action. The panel refreshes from `GET /api/batch/status` on the same polling loop as the rest of the dashboard.

## Server API

The dashboard exposes REST and SSE endpoints:

### REST

| Endpoint | Method | Description |
|---|---|---|
| `/api/tasks` | GET | List all features |
| `/api/new` | POST | Create a new feature |
| `/api/run` | POST | Start a feature run (spawns `4x run` subprocess) |
| `/api/stop` | POST | Stop a running feature |
| `/api/done` | POST | Mark feature as done; auto-merges worktree if present (multi-repo: all-or-nothing) |
| `/api/runs` | GET | List active runs |
| `/api/batch/start` | POST | Start a batch run (`4x batch run` subprocess); 409 if a batch conflict is unresolved |
| `/api/batch/stop` | POST | Gracefully stop the batch (writes `.4x/batch-stop`) |
| `/api/batch/continue` | POST | Clear the conflict signal and restart the batch (after resolving in the worktree) |
| `/api/batch/status` | GET | Batch running state, scheduled queue, current feature, and conflict signal |
| `/api/events/{id}` | GET | Get events for a feature |
| `/api/overview/{id}` | GET | Get feature overview (YAML fields + spec/plan content) |
| `/api/messages/{id}` | GET | Get messages for a feature |
| `/api/features/{id}/screenshots` | GET | Get screenshots grouped by round |
| `/api/features/{id}/screenshots/{filename}` | GET | Serve one screenshot image |
| `/api/logs/{id}` | GET | List log files for a feature |
| `/api/logs/{id}/{file}` | GET | Get a specific log file |
| `/api/projects` | GET | List registered projects |
| `/api/projects` | POST | Add a project (supports `init: true` for on-the-fly initialization) |
| `/api/projects` | DELETE | Remove a project |
| `/api/browse` | GET | Folder picker |
| `/api/settings` | GET | Get project settings (`.4x/settings.json`) |
| `/api/settings` | PUT | Update project settings (validates, backs up, writes) |
| `/api/user-config` | GET | Get user config (`~/.4x/settings.json`) |
| `/api/user-config` | PUT | Update user config (backs up to `.bak`, then writes) |
| `/api/merged-config` | GET | Read-only view of project + user merged effective config |
| `/api/locales` | GET | 回傳支援的 locale 清單 |
| `/api/locales/{lang}` | GET | 回傳對應語言的翻譯 JSON |

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

#### Batch Control

The dashboard can drive a batch run end-to-end without dropping back to the terminal. A dedicated `BatchManager` (separate from the per-feature `ProcessManager`) owns the single `4x batch run` subprocess for a project — only one batch may run at a time.

- **Start** (`POST /api/batch/start`) — the UI confirms first to avoid accidental launches, then starts the run. If `.4x/batch-conflict.json` still exists, the endpoint returns **HTTP 409** so a stale conflict must be resolved or continued first. The request body may carry `{runner, maxRounds}`; omitted fields fall back to the merged project/user config.
- **Stop** (`POST /api/batch/stop`) — writes `.4x/batch-stop` for a graceful stop (the batch finishes the current feature, then exits). It does **not** kill the subprocess.
- **Continue** (`POST /api/batch/continue`) — clears `.4x/batch-conflict.json`, then restarts the batch. Use after resolving the conflict in the worktree.
- **Status** (`GET /api/batch/status`) — returns the running flag, the scheduled queue, the current feature, and the conflict signal (or `null`):

  ```json
  {
    "running": true,
    "queue": [
      {"featureId": "F001-auth", "name": "Auth", "status": "done", "state": "done", "position": 0},
      {"featureId": "F002-api", "name": "API", "status": "coding", "state": "running", "position": 1}
    ],
    "currentFeature": "F002-api",
    "conflict": null
  }
  ```

  The queue is built from `batch.PlanBatch` so it honors the same dependency-and-priority ordering as the CLI. Each item's `state` is `done` (feature done / ready-for-review), `running` (an active run that isn't done), or `waiting`; `position` numbers the unfinished items.

### Screenshots Tab

Feature detail includes a **Screenshots** tab when screenshots exist for that feature. Screenshots are grouped by round, displayed as thumbnails, and can be opened in a lightbox with left/right navigation and ESC-to-close.

### SSE (Server-Sent Events)

| Endpoint | Description |
|---|---|
| `/sse/events/{id}` | Stream events for a feature (1-second polling) |
| `/sse/logs/{id}` | Stream the latest log file for a feature |

The event stream tracks a byte offset into `events.jsonl` and only sends newly appended lines. If the file is **truncated or rotated** — for example `4x transition --to init` resets the feature and rewrites `events.jsonl` from scratch — the new file size drops below the tracked offset. The stream detects this (`size < lastOffset`), resets the offset to 0, and re-reads the whole file from the beginning so the client recovers instead of silently stalling forever. A size equal to the offset still means "no new content" and is skipped.

The log stream (`/sse/logs/{id}`) likewise tracks a byte offset and only sends newly appended content. To avoid per-tick garbage, it reuses a single fixed 32KB read buffer allocated once per connection instead of allocating a new buffer sized to each delta. On every tick it loop-reads from the offset to EOF; a delta larger than 32KB is split across several SSE messages, each carrying the same `{"file": "...", "content": "..."}` payload. The client appends content as it arrives, so splitting is transparent. When `size <= lastOffset` (no new content) the tick is skipped without opening the file.

### Multi-Project Routing

With multiple projects, endpoints are prefixed with `/api/project/{project-id}/...` and `/sse/project/{project-id}/...`. Single-project mode uses the unprefixed paths for backward compatibility.

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
