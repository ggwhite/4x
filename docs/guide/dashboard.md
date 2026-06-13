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

## Server API

The dashboard exposes REST and SSE endpoints:

### REST

| Endpoint | Method | Description |
|---|---|---|
| `/api/tasks` | GET | List all features |
| `/api/new` | POST | Create a new feature |
| `/api/run` | POST | Start a feature run (spawns `4x run` subprocess) |
| `/api/stop` | POST | Stop a running feature |
| `/api/done` | POST | Mark feature as done; auto-merges worktree if present |
| `/api/runs` | GET | List active runs |
| `/api/events/{id}` | GET | Get events for a feature |
| `/api/overview/{id}` | GET | Get feature overview (YAML fields + spec/plan content) |
| `/api/messages/{id}` | GET | Get messages for a feature |
| `/api/logs/{id}` | GET | List log files for a feature |
| `/api/logs/{id}/{file}` | GET | Get a specific log file |
| `/api/projects` | GET | List registered projects |
| `/api/projects` | POST | Add a project (supports `init: true` for on-the-fly initialization) |
| `/api/projects` | DELETE | Remove a project |
| `/api/browse` | GET | Folder picker |
| `/api/settings` | GET | Get project settings (`.4x/settings.json`) |
| `/api/settings` | PUT | Update project settings (validates, backs up, writes) |
| `/api/locales` | GET | 回傳支援的 locale 清單 |
| `/api/locales/{lang}` | GET | 回傳對應語言的翻譯 JSON |

#### `POST /api/done` Response

Always returns HTTP 200. The `status` field is `"done"` only after the state transition succeeds. If merge conflict or merge error occurs, `status` remains `"pending-review"`. Additional fields indicate merge result:

| Field | Type | Meaning |
|---|---|---|
| `merged` | bool | `true` if branch was merged and worktree cleaned up |
| `merged` | bool | `false` if no worktree existed (state-only transition) |
| `merge_conflict` | bool | `true` if merge had conflicts; worktree preserved |
| `merge_error` | string | Merge error message; feature remains pending-review |
| `conflicts` | string[] | List of conflicting files (only present when `merge_conflict: true`) |

After a conflict, resolve the files in the worktree and run `4x merge <id>` to complete.

### SSE (Server-Sent Events)

| Endpoint | Description |
|---|---|
| `/sse/events/{id}` | Stream events for a feature (1-second polling) |
| `/sse/logs/{id}` | Stream the latest log file for a feature |

### Multi-Project Routing

With multiple projects, endpoints are prefixed with `/api/project/{project-id}/...` and `/sse/project/{project-id}/...`. Single-project mode uses the unprefixed paths for backward compatibility.

## Process Management

The dashboard manages runner subprocesses:

- Respects `max_concurrent_runs` from project config
- Captures stdout/stderr as run-output/run-error events
- Graceful shutdown: SIGTERM → 5 seconds → SIGKILL

## Platforms

| Platform | Status |
|---|---|
| Web UI (embedded) | Available |
| macOS native (Swift) | Planned |
| Electron (Windows/Linux) | Planned |
