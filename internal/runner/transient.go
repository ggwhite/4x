package runner

import (
	"regexp"
	"strings"
	"time"
)

// DefaultTransientRetries 是暫態錯誤的預設重試上限（不含原始那次嘗試）。
// 命中暫態 pattern 時最多再重跑同一命令這麼多次，避免網路抖動中斷整個 batch。
const DefaultTransientRetries = 3

// maxStderrCapture 限制捕捉 stderr 尾段的位元組數，避免子程序大量輸出佔記憶體；
// 暫態錯誤訊息通常落在輸出尾段，留最後這麼多 bytes 已足夠判定。
const maxStderrCapture = 8192

// defaultBackoffBase 是指數退避的預設基準間隔，在 SubprocessRunner.backoffBase 未注入（<=0）時採用。
const defaultBackoffBase = 1 * time.Second

// maxBackoff 是單次重試等待的上限，避免 attempt 變大時退避時間無限成長。
const maxBackoff = 30 * time.Second

// transientPatterns 是判定「暫態（可重試）錯誤」的 stderr 子字串集合（皆為小寫，比對前先 ToLower）。
//
// 只收錄「網路抖動 / 上游服務暫時不可用」這類重跑同一命令即可能恢復的錯誤；
// 刻意不含編譯錯誤、assertion 失敗、panic 等確定性失敗——那些重跑也不會好，
// 重試只是浪費時間並拖慢整個 batch。新增 pattern 時須一併在 transient_test.go 補界定案例。
var transientPatterns = []string{
	"socket closed",
	"socket hang up",
	"connection reset",
	"connection refused",
	"rate limit",
	"rate_limit",
	"too many requests",
	"internal server error",
	"bad gateway",
	"service unavailable",
	"gateway timeout",
	"overloaded",
	"server_error",
	"unexpected eof",
	"i/o timeout",
	"tls handshake timeout",
}

// transientStatusRe 比對獨立的 HTTP 暫態狀態碼（429 / 5xx）。用 word boundary 避免誤傷
// 像 "5000" 這種正常數字輸出——單純子字串比對 "500" 會誤命中。
var transientStatusRe = regexp.MustCompile(`\b(429|500|502|503|504)\b`)

// isTransientError 判斷子程序 stderr 尾段是否命中暫態錯誤 pattern。
//
// 只對網路 / 上游服務類的暫時性失敗回 true（重跑同命令可能恢復），藉此把「值得重試的抖動」
// 與「重跑也不會好的確定性失敗」區分開，避免對編譯錯誤、assertion 失敗等盲目重試。
// 比對採 case-insensitive。空字串一律回 false。
func isTransientError(stderr string) bool {
	if stderr == "" {
		return false
	}
	lower := strings.ToLower(stderr)
	for _, p := range transientPatterns {
		if strings.Contains(lower, p) {
			return true
		}
	}
	return transientStatusRe.MatchString(lower)
}

// backoffDuration 計算第 attempt 次重試前的等待時間（attempt 從 1 起算）：
// base * 2^(attempt-1)，並以 maxBackoff 為上限。base <= 0 時套用 defaultBackoffBase。
func backoffDuration(attempt int, base time.Duration) time.Duration {
	if base <= 0 {
		base = defaultBackoffBase
	}
	if attempt < 1 {
		attempt = 1
	}
	d := base
	for i := 1; i < attempt; i++ {
		d *= 2
		if d >= maxBackoff {
			return maxBackoff
		}
	}
	if d > maxBackoff {
		return maxBackoff
	}
	return d
}

// tailWriter 是 bounded io.Writer：只保留最後 limit bytes 的內容，供重試判定讀取 stderr 尾段，
// 避免子程序大量輸出佔用記憶體。Write 永不回報錯誤，純粹當旁路 tee 使用。
type tailWriter struct {
	buf   []byte
	limit int
}

// newTailWriter 建立保留最後 limit bytes 的 tailWriter；limit <= 0 時 Write 一律丟棄內容。
func newTailWriter(limit int) *tailWriter {
	return &tailWriter{limit: limit}
}

func (w *tailWriter) Write(p []byte) (int, error) {
	n := len(p)
	if w.limit <= 0 {
		return n, nil
	}
	w.buf = append(w.buf, p...)
	if len(w.buf) > w.limit {
		// 只保留最後 limit bytes；copy 到新 slice 確保 backing array 不會隨寫入無限成長。
		trimmed := make([]byte, w.limit)
		copy(trimmed, w.buf[len(w.buf)-w.limit:])
		w.buf = trimmed
	}
	return n, nil
}

// String 回傳目前保留的 stderr 尾段內容。
func (w *tailWriter) String() string {
	return string(w.buf)
}

// transientLogSnippet 把 stderr 尾段壓成單行、截斷後的短片段，供 warn log 標示命中內容，避免污染 log。
func transientLogSnippet(s string) string {
	const max = 200
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > max {
		s = s[len(s)-max:]
	}
	return s
}
