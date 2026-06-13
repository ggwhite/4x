# F057: Log SSE Buffer Allocation Optimization

## 現狀

`handleLogSSE`（server.go:852）在每個 1 秒 tick 都分配新的 byte buffer：
```go
buf := make([]byte, info.Size()-lastOffset)
```
大型 log 檔頻繁追加時，每秒產生新 buffer 再立刻丟棄，增加 GC 壓力。

## 設計

在 ticker loop 外分配一個 32KB 的 reusable buffer，tick 時用 loop read 直到 EOF，每個 chunk 獨立推送 SSE message。

### 改動

只改 `internal/server/server.go` 的 `handleLogSSE` 函式（行 818-862）：

1. 行 818 之後加 `buf := make([]byte, 32*1024)`
2. 行 852-854 的單次讀取改為 loop read：
   ```go
   for {
       n, err := f.Read(buf)
       if n > 0 {
           chunk, _ := json.Marshal(map[string]string{"file": current, "content": string(buf[:n])})
           fmt.Fprintf(w, "data: %s\n\n", chunk)
           lastOffset += int64(n)
       }
       if err != nil {
           break
       }
   }
   f.Close()
   flusher.Flush()
   ```
3. 移除 `info.Size()` 檢查中的 buffer 大小計算（不再需要預知 delta 大小）

### 行為變化

- 大 delta（> 32KB）會分成多個 SSE message 推送，每個 message 格式不變
- 前端 append 行為不受影響（本來就是累加 content）
- 小 delta（< 32KB）行為完全相同，只是不再每次分配新 buffer

## 約束

- 不改 SSE message 格式（`{"file": "...", "content": "..."}`）
- 不引入額外 goroutine
- 不移除 `info.Size() <= lastOffset` 的提前跳過檢查（避免無意義的 open/close）
