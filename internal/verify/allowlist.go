package verify

import "strings"

// substitutionMarkers 是 command/process substitution 的觸發子字串。
// 只要整條命令含其中任一，即整條拒絕（不論前綴是否吻合）——因為 executeCommand 仍以
// sh -c 執行，內嵌的命令替換會先被 shell 執行。變數展開（$VAR、${VAR}）只做字面展開、
// 不執行子命令，故不列入（僅 $( 觸發，$ 後接非左括號不觸發）。
var substitutionMarkers = []string{"$(", "`", "<(", ">("}

// redirectionChars 是 I/O 重導向運算子含有的字元（涵蓋 > >> < 2> &> >| << 等變體）。
// 命令含其一即整條拒絕：重導向能把輸出寫到 allowlist 外的任意檔案（如
// `make test > ~/.zshrc`），是 allowlist 意圖攔截的注入面。一般 verify 命令不用重導向，
// 直接拒絕是安全且正確的保守選擇。
const redirectionChars = "<>"

// safeFilterTools 是常見唯讀過濾/文字處理工具集合。位於 pipe（單一 `|`）下游的
// segment 若前綴屬此集合，即視為安全輔助放行，不要求出現在使用者 allowlist——
// 讓 `go test ./... | grep -v '^ok'` 這類慣用寫法不被 pipe 分段檢查誤擋。
// 僅放寬 pipe 的下游 filter 段；compound（; && || &）分段仍各自需匹配 allowlist。
var safeFilterTools = map[string]bool{
	"grep": true, "egrep": true, "fgrep": true, "tee": true, "awk": true,
	"sed": true, "wc": true, "head": true, "tail": true, "sort": true,
	"uniq": true, "cut": true, "tr": true, "cat": true, "xargs": true,
	"column": true, "jq": true,
}

// shellSegment 是切段後的單一命令片段，afterPipe 標記其前導運算子是否為單一 pipe `|`。
type shellSegment struct {
	text      string
	afterPipe bool
}

// CommandAllowed 回報 cmd 是否被 allowlist 放行。
//
//   - allowlist 為空 → 一律 true（不強制，向下相容，維持現狀）。
//   - allowlist 非空時先掃整條命令：含 command/process substitution 構造
//     （$( 、反引號、<( 、>( ）任一 → false，不論前綴是否吻合。
//   - 含 I/O 重導向字元（< 或 >）→ false，避免寫入 allowlist 外的檔案。
//   - 依 ; & | && || 與換行把 cmd 切段，每個 TrimSpace 後非空的 segment 須以清單中某前綴
//     開頭（word-boundary：segment == 前綴 或 以「前綴+空白」開頭）才放行；任一段不符 → false。
//     例外：位於單一 pipe `|` 下游的 segment 若前綴屬 safeFilterTools（grep/awk/… 等唯讀
//     過濾工具）即放行，不要求出現在 allowlist。compound（; && || &）分段不放寬。
//   - 全段皆符且無替換/重導向構造 → true。
//
// 不做 quote-aware 解析：引號內的 ;、&&、$( 也保守視為分段/替換符（DR-2c 刻意保守誤擋）。
func CommandAllowed(cmd string, allowlist []string) bool {
	if len(allowlist) == 0 {
		return true
	}

	for _, marker := range substitutionMarkers {
		if strings.Contains(cmd, marker) {
			return false
		}
	}

	if strings.ContainsAny(cmd, redirectionChars) {
		return false
	}

	for _, seg := range splitShellSegments(cmd) {
		text := strings.TrimSpace(seg.text)
		if text == "" {
			continue
		}
		if seg.afterPipe && isSafeFilter(text) {
			continue
		}
		if !segmentAllowed(text, allowlist) {
			return false
		}
	}
	return true
}

// segmentAllowed 回報單一 segment 是否符合 allowlist 中任一前綴（word-boundary）。
func segmentAllowed(seg string, allowlist []string) bool {
	for _, entry := range allowlist {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		if seg == entry || strings.HasPrefix(seg, entry+" ") {
			return true
		}
	}
	return false
}

// isSafeFilter 回報 segment 的第一個 token 是否為 safeFilterTools 內的唯讀過濾工具。
func isSafeFilter(seg string) bool {
	tokens := strings.Fields(seg)
	if len(tokens) == 0 {
		return false
	}
	return safeFilterTools[tokens[0]]
}

// splitShellSegments 依 shell 控制運算子（; & && | || 換行）把 cmd 切成 segment，
// 並標記每段的前導運算子是否為單一 pipe `|`（供 safeFilterTools 放寬用）。
// 對 && 與 || 這類雙字元運算子做前瞻消耗第二字元，避免 || 被誤判為 pipe。
func splitShellSegments(cmd string) []shellSegment {
	var segs []shellSegment
	var buf strings.Builder
	afterPipe := false

	flush := func(nextAfterPipe bool) {
		segs = append(segs, shellSegment{text: buf.String(), afterPipe: afterPipe})
		buf.Reset()
		afterPipe = nextAfterPipe
	}

	runes := []rune(cmd)
	for i := 0; i < len(runes); i++ {
		switch runes[i] {
		case ';', '\n':
			flush(false)
		case '&':
			if i+1 < len(runes) && runes[i+1] == '&' {
				i++
			}
			flush(false)
		case '|':
			if i+1 < len(runes) && runes[i+1] == '|' {
				i++
				flush(false)
			} else {
				flush(true)
			}
		default:
			buf.WriteRune(runes[i])
		}
	}
	flush(false)
	return segs
}
