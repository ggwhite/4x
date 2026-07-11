package server

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ggwhite/4x/internal/codexlog"
	"github.com/ggwhite/4x/internal/protocol"
)

// handleSSE 用 polling 方式 tail events.jsonl 並以 SSE 推送
func handleSSE(ws *protocol.CachedWorkspace, featureID string, w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	slog.Debug("sse connected", "feature", featureID)
	defer slog.Debug("sse disconnected", "feature", featureID)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher.Flush()

	path := filepath.Join(ws.FeatureDir(featureID), protocol.EventsFile)
	var lastOffset int64

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			info, err := os.Stat(path)
			if err != nil {
				continue
			}
			// 檔案被 truncate/rotate（如 4x transition --to init 重置 events.jsonl）後
			// size 會小於 lastOffset，此時 reset 回 0 從頭重讀，否則 SSE 會永遠卡住不再送新 event。
			if info.Size() < lastOffset {
				lastOffset = 0
			}
			if info.Size() == lastOffset {
				continue
			}
			f, err := os.Open(path)
			if err != nil {
				continue
			}
			if lastOffset > 0 {
				f.Seek(lastOffset, 0)
			}
			scanner := bufio.NewScanner(f)
			consumed := lastOffset
			for scanner.Scan() {
				line := scanner.Text()
				consumed += int64(len(scanner.Bytes())) + 1
				if line == "" {
					continue
				}
				fmt.Fprintf(w, "data: %s\n\n", line)
			}
			// scanner 提前中止（超長行超過 64KB token 上限、或讀取出錯）時不前進 lastOffset，
			// 讓下個 tick 從同一位置重試，避免靜默截斷導致事件永久遺失。
			// 仍 flush 本輪在出錯前已送出的合法行（不因錯誤而被扣留），但保留 lastOffset 重試。
			if err := scanner.Err(); err != nil {
				slog.Error("events SSE scanner error", "feature", featureID, "error", err)
				f.Close()
				flusher.Flush()
				continue
			}
			// events.jsonl 為 live append log，scanner 可能讀到 os.Stat 之後新 append 的完整行，
			// 使 consumed > info.Size()。保留實際已消費位移（不 clamp 回 stale size），
			// 否則下個 tick 會重 seek 並重送這些已送出的事件。
			lastOffset = consumed
			f.Close()
			flusher.Flush()
		}
	}
}

// handleLogSSE 即時 tail log 檔案。支援 ?file= 指定特定檔案，未指定則追蹤最新的；
// 搭配 ?file= 時另支援 ?offset= 指定續傳起始位移（bytes），避免重送呼叫端已讀過的內容。
func handleLogSSE(ws *protocol.CachedWorkspace, featureID string, w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	slog.Debug("log sse connected", "feature", featureID)
	defer slog.Debug("log sse disconnected", "feature", featureID)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher.Flush()

	logsDir := filepath.Join(ws.FeatureDir(featureID), "logs")
	pinnedFile := filepath.Base(r.URL.Query().Get("file"))
	if pinnedFile != "" && pinnedFile != "." && !strings.HasSuffix(pinnedFile, ".log") {
		http.Error(w, "invalid log file", http.StatusBadRequest)
		return
	}
	// offsets 記每個正在 tail 的 log 已讀到的位移，跨 tick 持續累進。
	// 未 pin 檔案時可同時 tail 多個活躍 log（平行 sub-reviewer / reviewer+tester），各自獨立 offset。
	offsets := make(map[string]int64)
	// seen 記錄哪些 log 檔已完成首次 offset 初始化，避免重複跳到尾端。
	seen := make(map[string]bool)
	// 前端先用 REST /api/logs/ 一次性讀完整內容，緊接著才開這條 SSE 連線續 tail 新增內容。
	// 若呼叫端帶 ?offset=<n>（REST 內容的 UTF-8 byte 長度），從該位移接續，跳過
	// maxInitialTail 的「首次跳到尾端」邏輯，避免 REST 已顯示過的內容被當作新內容重送一次。
	if pinnedFile != "" && pinnedFile != "." {
		if offsetParam := r.URL.Query().Get("offset"); offsetParam != "" {
			if off, err := strconv.ParseInt(offsetParam, 10, 64); err == nil && off >= 0 {
				offsets[pinnedFile] = off
				seen[pinnedFile] = true
			}
		}
	}
	// needAlign 標記因初始 offset 可能落在多位元組 UTF-8 字元中間，需在首次讀取時對齊。
	needAlign := make(map[string]bool)

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	// 在連線生命週期內共用一個固定 32KB buffer，避免每秒 tick 重新分配造成 GC 壓力。
	buf := make([]byte, 32*1024)

	// SSE 首次 tail 每個 log 檔時，最多只串流尾端 maxInitialTail bytes 的內容，
	// 避免大檔（如 3MB+ 的 coder log）一口氣灌入導致 WebView 卡死。
	// 完整歷史內容由 REST /api/logs/ endpoint 提供。
	const maxInitialTail int64 = 64 * 1024

	// logDirCache 快取 findActiveLogs 的 ReadDir 結果，依目錄 mtime 失效，
	// 每個 SSE 連線獨立一份，免除每秒 ReadDir+stat 的 I/O 開銷。
	var logDirCache activeLogCache

	// carryBufs 保留各 log 檔尾不完整的 UTF-8 殘段，與 offsets 平行、跨 tick 持續保留。
	// 固定切分會把橫跨 read chunk / tick 邊界的多位元組字元切兩半，各自 json.Marshal
	// 會被替換成不可逆的 U+FFFD；故只 emit 完整 rune，殘段留待後續 bytes 補齊再送。
	// 殘段 bytes 不計入 offsets（offsets 仍前進 n，但殘段保存在此），由下個 tick 拼回。
	carryBufs := make(map[string][]byte)

	// codex log 走「逐行」轉換（非 rune-based 穿透）：每個 log 首次處理時以內容式
	// head-peek 辨識是否為 codex 格式並快取結果；codexCarry 保留尾端不完整的行，
	// 等收到換行湊齊完整行才轉換，避免半截 JSON 被誤判為 passthrough 洩漏破碎原文。
	codexChecked := make(map[string]bool)
	codexLog := make(map[string]bool)
	codexCarry := make(map[string][]byte)

	// emitCodexLine 對單一完整 codex log 行轉換後以 SSE message 送出（帶 file 欄位）。
	// handled && text=="" → 丟棄（codex 事件但無可顯示文字）；!handled → 原行穿透。
	emitCodexLine := func(current string, line []byte) {
		text, handled := codexlog.ConvertLine(line)
		var content string
		if handled {
			if text == "" {
				return
			}
			content = text + "\n"
		} else {
			content = string(line) + "\n"
		}
		chunk, _ := json.Marshal(map[string]string{"file": current, "content": content})
		fmt.Fprintf(w, "data: %s\n\n", chunk)
	}

	// tailCodex 對 codex log 做 line buffering：以 \n 為界切完整行逐行轉換，尾端不
	// 完整行留在 codexCarry 待下個 tick。offsets[current] 仍累進 raw byte（與 emit
	// 內容長度解耦），維持與原始檔 byte 位置一致的續傳語意。
	tailCodex := func(f *os.File, current string) {
		carry := codexCarry[current]
		// 初次跳入檔案中段（needAlign）時，第一個換行前必是殘缺行，須捨棄以免當 passthrough。
		dropPartial := needAlign[current]
		delete(needAlign, current)
		for {
			n, readErr := f.Read(buf)
			if n > 0 {
				carry = append(carry, buf[:n]...)
				offsets[current] += int64(n)
				for {
					idx := bytes.IndexByte(carry, '\n')
					if idx < 0 {
						break
					}
					line := carry[:idx]
					if dropPartial {
						dropPartial = false
					} else {
						emitCodexLine(current, line)
					}
					carry = carry[idx+1:]
				}
			}
			if readErr != nil {
				break
			}
		}
		// 複製殘段到自有 backing，避免與下個 tick 共用的 buf 別名。
		codexCarry[current] = append([]byte(nil), carry...)
	}

	// tailFile 讀取 current 自上次 offset 起的新增內容並以 SSE message 送出（帶 file 欄位）。
	tailFile := func(current string) {
		path := filepath.Join(logsDir, current)
		info, err := os.Stat(path)
		if err != nil {
			return
		}
		if !seen[current] {
			seen[current] = true
			if info.Size() > maxInitialTail {
				offsets[current] = info.Size() - maxInitialTail
				needAlign[current] = true
			}
		}
		if info.Size() <= offsets[current] {
			return
		}
		// 首次處理該 log 時做一次性內容式辨識（head-peek 第一個「完整」非空行是否為 codex 事件）。
		// 若目前尚無 \n-terminated 完整首行（runner 正寫入首行前半段），保持 undecided：
		// 不快取、不讀取、不前進 offset，待下個 tick 收齊完整首行再判定；避免把半截 JSON
		// 誤判為非 codex 而永久走 rune passthrough、洩漏破碎原文。
		if !codexChecked[current] {
			isCodex, decided := detectCodexLog(path)
			if !decided {
				return
			}
			codexChecked[current] = true
			codexLog[current] = isCodex
		}
		f, err := os.Open(path)
		if err != nil {
			return
		}
		defer f.Close()
		if offsets[current] > 0 {
			f.Seek(offsets[current], 0)
		}
		// codex log 走逐行轉換分支；非 codex 檔完全走以下既有 rune-based 穿透邏輯（一 byte 不改）。
		if codexLog[current] {
			tailCodex(f, current)
			return
		}
		if needAlign[current] {
			delete(needAlign, current)
			var probe [3]byte
			pn, _ := f.ReadAt(probe[:], offsets[current])
			skip := 0
			for skip < pn && !utf8.RuneStart(probe[skip]) {
				skip++
			}
			if skip > 0 {
				offsets[current] += int64(skip)
				f.Seek(offsets[current], 0)
			}
		}
		carry := carryBufs[current]
		for {
			n, readErr := f.Read(buf)
			if n > 0 {
				data := buf[:n]
				if len(carry) > 0 {
					data = append(append(make([]byte, 0, len(carry)+n), carry...), buf[:n]...)
				}
				complete, rest := splitCompleteUTF8(data)
				if len(complete) > 0 {
					chunk, _ := json.Marshal(map[string]string{"file": current, "content": string(complete)})
					fmt.Fprintf(w, "data: %s\n\n", chunk)
				}
				// 複製 rest 到 carry 自有 backing，避免別名共用的 buf 在下個 tick 被覆寫。
				carry = append(carry[:0], rest...)
				offsets[current] += int64(n)
			}
			if readErr != nil {
				// EOF：殘段可能是 writer 寫入中途的不完整 rune，保留至下個 tick
				// 收到後續 bytes 再拼成完整 rune 送出，不在此 emit，避免檔尾產生 U+FFFD。
				break
			}
		}
		carryBufs[current] = carry
	}

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			var files []string
			if pinnedFile != "" && pinnedFile != "." {
				files = []string{pinnedFile}
			} else {
				files = findActiveLogs(logsDir, &logDirCache)
			}
			for _, current := range files {
				tailFile(current)
			}
			flusher.Flush()
		}
	}
}

// detectCodexLog 以內容式辨識判斷一個 log 檔是否為 codex JSONL 格式：
// 讀檔頭第一個「以 \n 結尾的完整」非空行，能被 codexlog.ConvertLine 認定為 codex 事件
// （handled==true）即為 codex log。顯示端不知道 runner 名稱（log 檔名不帶 runner、
// 同 round 可能多 runner），故一律採內容辨識。
//
// 回傳 (isCodex, decided)：decided==false 代表「目前無法判定」，呼叫端應維持 undecided、
// 不快取、不前進 offset，待後續 bytes 補齊再重試。只有在「首個非空 bytes 去空白後以 '{'
// 開頭、但尚無 \n」時才 undecided——這可能是 codex JSON 事件行寫到一半，若此時誤判為非
// codex 走 rune passthrough 會洩漏半截原始 JSON（HIGH 風險）。反之，若開頭不是 '{'，此行
// 不可能成為 codex 事件（codex 每行皆為 JSON 物件），即刻判定為非 codex 走 passthrough，
// 不空等 \n——否則無換行的合法非 codex 內容（claude 純文字、單一大 chunk）會被永久卡住不顯示。
func detectCodexLog(path string) (isCodex, decided bool) {
	f, err := os.Open(path)
	if err != nil {
		return false, false
	}
	defer f.Close()
	head := make([]byte, 64*1024)
	n, _ := f.Read(head)
	data := head[:n]
	for {
		idx := bytes.IndexByte(data, '\n')
		if idx < 0 {
			// 尚無 \n-terminated 完整行：僅在「可能是 codex JSON 半行」時 undecided。
			trimmed := bytes.TrimLeft(data, " \t\r\n")
			if len(trimmed) == 0 || trimmed[0] == '{' {
				return false, false
			}
			return false, true
		}
		line := data[:idx]
		data = data[idx+1:]
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		_, handled := codexlog.ConvertLine(line)
		return handled, true
	}
}

// activeLogWindow 是 findActiveLogs 判定 log「最近活躍」的時間窗：mtime 落在最新 log
// mtime 往前 activeLogWindow 內者視為仍在寫入，須一併 tail。窗夠大才能涵蓋平行 runner
// 之間的寫入時間差，又不至於把上一個 phase 的舊 log 拉進來。
const activeLogWindow = 15 * time.Second

// activeLogCache 快取 findActiveLogs 的 ReadDir 結果，以目錄 mtime 作為失效依據。
// log 檔案只在 phase 轉換時新增或刪除，因此目錄 mtime 未變時可直接回傳 cached 結果，
// 省去每秒一次的 ReadDir + per-entry stat 開銷。
type activeLogCache struct {
	dirMtime time.Time
	result   []string
}

// findActiveLogs 回傳目前正在寫入 / 最近活躍的 log 檔名清單（依 logSortKey 排序）。
// 以最新 log 的 mtime 為基準，回傳所有 mtime 落在 activeLogWindow 內的 .log，
// 讓平行 sub-reviewer（或 reviewer+tester）等多個同時寫入的 log 都能被 SSE 一起 tail，
// 而非只追到 mtime 最新的單一檔（修復 ParallelReviewTest 只看得到一個 log 的問題）。
//
// cache 參數由呼叫端持有（每個 SSE 連線一份），當目錄 mtime 未變時直接回傳快取結果，
// 避免 SSE hot loop 每秒執行 ReadDir+stat。
func findActiveLogs(dir string, cache *activeLogCache) []string {
	dirInfo, err := os.Stat(dir)
	if err != nil {
		return nil
	}
	if cache != nil && dirInfo.ModTime().Equal(cache.dirMtime) && cache.result != nil {
		return cache.result
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	type logEntry struct {
		name string
		mod  time.Time
	}
	var logs []logEntry
	var latest time.Time
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".log") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		logs = append(logs, logEntry{name: e.Name(), mod: info.ModTime()})
		if info.ModTime().After(latest) {
			latest = info.ModTime()
		}
	}
	if len(logs) == 0 {
		if cache != nil {
			cache.dirMtime = dirInfo.ModTime()
			cache.result = nil
		}
		return nil
	}
	cutoff := latest.Add(-activeLogWindow)
	var active []string
	for _, l := range logs {
		if !l.mod.Before(cutoff) {
			active = append(active, l.name)
		}
	}
	sort.Slice(active, func(i, j int) bool {
		return logSortKey(active[i]) < logSortKey(active[j])
	})
	if cache != nil {
		cache.dirMtime = dirInfo.ModTime()
		cache.result = active
	}
	return active
}

// splitCompleteUTF8 將 data 切成「結尾為完整 UTF-8 rune 的前段 complete」與
// 「結尾不完整的殘段 rest」。當 log 的 32KB chunk 邊界剛好落在多位元組字元中間時，
// 殘段（最長 3 bytes）需保留併入下一輪 Read 前綴後再嘗試送出，避免字元被切半損毀。
// 回傳的 complete／rest 共用 data 底層陣列，呼叫端若需跨輪保留 rest 應自行複製。
func splitCompleteUTF8(data []byte) (complete, rest []byte) {
	if len(data) == 0 {
		return data, nil
	}
	// 從尾端往前找最後一個 rune 的起始位元組（非 continuation byte）
	i := len(data) - 1
	for i >= 0 && !utf8.RuneStart(data[i]) {
		i--
	}
	// 整段都是 continuation bytes（理論上不會發生），全部送出避免卡死
	if i < 0 {
		return data, nil
	}
	// 最後一個 rune 已完整 → 整段皆可送出；否則保留該 rune 起點之後為殘段
	if utf8.FullRune(data[i:]) {
		return data, nil
	}
	return data[:i], data[i:]
}
