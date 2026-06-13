# F057: Log SSE Buffer Optimization — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 消除 `handleLogSSE` 每秒分配新 byte buffer 的 GC 壓力，改用固定 32KB reusable buffer。

**Architecture:** 只改 `handleLogSSE` 一個函式，把 buffer 分配移到 loop 外，讀取改為 loop read。

**Tech Stack:** Go 1.26+

---

### Task 1: 重構 handleLogSSE buffer 分配

**Files:**
- Modify: `internal/server/server.go:818-862`

- [ ] **Step 1: 把 buffer 分配移到 ticker loop 外**

在 `internal/server/server.go` 的 `handleLogSSE` 函式中，找到行 819：

```go
ticker := time.NewTicker(time.Second)
defer ticker.Stop()
```

在其後加入：

```go
buf := make([]byte, 32*1024)
```

- [ ] **Step 2: 把單次讀取改為 loop read**

找到行 845-860 的讀取邏輯：

```go
f, err := os.Open(path)
if err != nil {
    continue
}
if lastOffset > 0 {
    f.Seek(lastOffset, 0)
}
buf := make([]byte, info.Size()-lastOffset)
n, _ := f.Read(buf)
f.Close()
if n > 0 {
    chunk, _ := json.Marshal(map[string]string{"file": current, "content": string(buf[:n])})
    fmt.Fprintf(w, "data: %s\n\n", chunk)
}
lastOffset += int64(n)
flusher.Flush()
```

替換為：

```go
f, err := os.Open(path)
if err != nil {
    continue
}
if lastOffset > 0 {
    f.Seek(lastOffset, 0)
}
for {
    n, readErr := f.Read(buf)
    if n > 0 {
        chunk, _ := json.Marshal(map[string]string{"file": current, "content": string(buf[:n])})
        fmt.Fprintf(w, "data: %s\n\n", chunk)
        lastOffset += int64(n)
    }
    if readErr != nil {
        break
    }
}
f.Close()
flusher.Flush()
```

- [ ] **Step 3: 編譯確認無錯誤**

Run: `go build ./cmd/4x && go vet ./...`
Expected: 無錯誤

- [ ] **Step 4: 跑全部測試**

Run: `go test ./...`
Expected: 全部 PASS

- [ ] **Step 5: Commit**

```bash
git add internal/server/server.go
git commit -m "perf(F057): reuse 32KB buffer in handleLogSSE instead of per-tick allocation"
```
