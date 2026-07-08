package protocol

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/ggwhite/4x/internal/feature"
)

// enumLiteralAllowlist 是無法消費 SoT accessor 的既知平行清單，key 為 repo 相對路徑。
// 只有「技術上不可能動態產生」的位置才可加入（並註明原因）；能改的請改為消費
// accessor（AllStatuses / AllPhases / …），不要往這裡加。
var enumLiteralAllowlist = map[string]string{
	// jsonschema struct tag 是編譯期常量，無法在 tag 內消費 accessor；
	// MCP SDK 也不提供 per-field 的動態 description 注入點。
	"internal/mcp/tools.go": "jsonschema struct tag 為編譯期常量",
}

// TestNoParallelEnumLiteralLists 掃描 cmd/ 與 internal/ 的非測試 Go 原始碼，偵測
// 單一行內以字面值並列 3 個以上同一 SoT 列舉集合成員的「平行清單」（如錯誤訊息
// valid 值清單、CLI usage 字串）。這類清單在列舉新增值時不會被編譯器抓到，是靜默
// 漂移的來源；新增命中時應改為消費 SoT accessor，確實無法動態產生者（如 jsonschema
// struct tag 的編譯期常量）才加入 enumLiteralAllowlist 並註明原因。
func TestNoParallelEnumLiteralLists(t *testing.T) {
	sets := map[string][]string{}

	var statusVals []string
	for _, s := range feature.AllStatuses() {
		statusVals = append(statusVals, string(s))
	}
	sets["feature-status"] = statusVals
	sets["subtask-status"] = feature.AllSubtaskStatuses()

	var phaseVals []string
	for _, p := range AllPhases() {
		phaseVals = append(phaseVals, string(p))
	}
	sets["phase"] = phaseVals

	// 偵測「值 分隔符 value 分隔符 值」的鏈狀清單模式（如 "a/b/c" 或 "a, b, c"），
	// 而非只數同一行出現幾個值——錯誤訊息裡碰巧出現多個列舉字（如
	// "testing → accepting blocked"）不是平行清單，不應誤報。
	// word boundary 同時避免子字串誤判（如 "abandoned" 含 "done"）。
	res := map[string]*regexp.Regexp{}
	for name, vals := range sets {
		quoted := make([]string, len(vals))
		for i, v := range vals {
			quoted[i] = regexp.QuoteMeta(v)
		}
		alt := `\b(?:` + strings.Join(quoted, "|") + `)\b`
		sep := `\s*[/,|]\s*`
		res[name] = regexp.MustCompile(alt + sep + alt + sep + alt)
	}

	// SoT 定義檔本身與測試檔不掃。
	sotFiles := map[string]bool{
		"internal/feature/enums.go":  true,
		"internal/protocol/enums.go": true,
	}

	root := filepath.Join("..", "..")
	var violations []string
	for _, base := range []string{"cmd", "internal"} {
		err := filepath.WalkDir(filepath.Join(root, base), func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			rel, _ := filepath.Rel(root, path)
			rel = filepath.ToSlash(rel)
			if sotFiles[rel] {
				return nil
			}
			if _, ok := enumLiteralAllowlist[rel]; ok {
				return nil
			}

			f, err := os.Open(path)
			if err != nil {
				return err
			}
			defer f.Close()
			scanner := bufio.NewScanner(f)
			scanner.Buffer(make([]byte, 0, 1<<20), 1<<20)
			lineNo := 0
			for scanner.Scan() {
				lineNo++
				line := scanner.Text()
				trimmed := strings.TrimSpace(line)
				// 只看含字串字面值的程式行；註解屬文件範疇不在此 guard。
				if strings.HasPrefix(trimmed, "//") || !strings.Contains(trimmed, `"`) {
					continue
				}
				for name, re := range res {
					if re.MatchString(trimmed) {
						violations = append(violations, fmt.Sprintf("%s:%d [%s] %s", rel, lineNo, name, trimmed))
						break
					}
				}
			}
			return scanner.Err()
		})
		if err != nil {
			t.Fatalf("walk %s: %v", base, err)
		}
	}

	if len(violations) > 0 {
		t.Errorf("發現 %d 處平行維護的列舉字面值清單，請改為消費 SoT accessor（無法動態產生者加入 enumLiteralAllowlist 並註明原因）：\n%s",
			len(violations), strings.Join(violations, "\n"))
	}
}
