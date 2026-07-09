package verify

import "strings"

// substitutionMarkers 是 command/process substitution 的觸發子字串。
// 只要整條命令含其中任一，即整條拒絕（不論前綴是否吻合）——因為 executeCommand 仍以
// sh -c 執行，內嵌的命令替換會先被 shell 執行。變數展開（$VAR、${VAR}）只做字面展開、
// 不執行子命令，故不列入（僅 $( 觸發，$ 後接非左括號不觸發）。
var substitutionMarkers = []string{"$(", "`", "<(", ">("}

// shellSegmentDelimiters 是把命令切成 segment 的 shell 控制運算子（單字元集合）。
// 換行併入 ; & | 一併切；&&/|| 會被切成中間空段，空段一律 skip，語意等同。
const shellSegmentDelimiters = ";&|\n"

// CommandAllowed 回報 cmd 是否被 allowlist 放行。
//
//   - allowlist 為空 → 一律 true（不強制，向下相容，維持現狀）。
//   - allowlist 非空時先掃整條命令：含 command/process substitution 構造
//     （$( 、反引號、<( 、>( ）任一 → false，不論前綴是否吻合。
//   - 依 ; & | 與換行把 cmd 切段，每個 TrimSpace 後非空的 segment 須以清單中某前綴
//     開頭（word-boundary：segment == 前綴 或 以「前綴+空白」開頭）才放行；任一段不符 → false。
//   - 全段皆符且無替換構造 → true。
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

	for _, seg := range splitShellSegments(cmd) {
		seg = strings.TrimSpace(seg)
		if seg == "" {
			continue
		}
		if !segmentAllowed(seg, allowlist) {
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

// splitShellSegments 依 shell 控制運算子（; & | 換行）把 cmd 切成 segment。
// 單字元切法對 &&/|| 會產生中間空段，交由呼叫端 skip。
func splitShellSegments(cmd string) []string {
	return strings.FieldsFunc(cmd, func(r rune) bool {
		return strings.ContainsRune(shellSegmentDelimiters, r)
	})
}
