package guard

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/ggwhite/4x/internal/gitops"
	"github.com/ggwhite/4x/internal/protocol"
)

// DefaultProtectedPaths 是 self-mod guard 的內建受保護路徑前綴白名單，
// 涵蓋三層架構紅線：狀態機、guardrail、protocol。settings 未設定時套用。
var DefaultProtectedPaths = []string{"internal/state/", "internal/guard/", "internal/protocol/"}

// DefaultSelfModMaxDiffLines 是受保護路徑單輪 diff 行數的內建上限。
const DefaultSelfModMaxDiffLines = 200

// SelfModPolicy 是套用預設後可直接使用的 self-mod guard 策略。
type SelfModPolicy struct {
	ProtectedPaths []string
	MaxDiffLines   int
	RequireTests   bool
}

// SelfModDetector 是檔案層變更偵測的可選介面，由 gitops.Ops 實作。
// checkSelfMod 以 type-assert 取得它；取不到時 fallback 到本地 git helper。
type SelfModDetector interface {
	DetectChangedFiles(featureID string) []protocol.ChangedFile
}

// ResolveSelfMod 回傳套用內建預設後的 self-mod 策略。
// cfg.SelfMod 為 nil 或欄位為零值時：ProtectedPaths 退回 DefaultProtectedPaths、
// MaxDiffLines 退回 DefaultSelfModMaxDiffLines、RequireTests 預設 true。
func ResolveSelfMod(cfg protocol.Config) SelfModPolicy {
	policy := SelfModPolicy{
		ProtectedPaths: DefaultProtectedPaths,
		MaxDiffLines:   DefaultSelfModMaxDiffLines,
		RequireTests:   true,
	}
	if cfg.SelfMod == nil {
		return policy
	}
	if len(cfg.SelfMod.ProtectedPaths) > 0 {
		policy.ProtectedPaths = cfg.SelfMod.ProtectedPaths
	}
	if cfg.SelfMod.MaxDiffLines > 0 {
		policy.MaxDiffLines = cfg.SelfMod.MaxDiffLines
	}
	if cfg.SelfMod.RequireTests != nil {
		policy.RequireTests = *cfg.SelfMod.RequireTests
	}
	return policy
}

// MatchProtected 回報 path 是否落在任一受保護前綴下。
func MatchProtected(path string, protected []string) bool {
	for _, prefix := range protected {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

// ProtectedHits 過濾出落在受保護前綴下的變更檔案。
func ProtectedHits(files []protocol.ChangedFile, protected []string) []protocol.ChangedFile {
	var hits []protocol.ChangedFile
	for _, f := range files {
		if MatchProtected(f.Path, protected) {
			hits = append(hits, f)
		}
	}
	return hits
}

// HasAccompanyingTests 回報受保護路徑的變更是否附帶對應測試。
// 若 paths 中受保護的非 _test.go 的 .go 檔有變更，則必須同時存在受保護的 _test.go 變更才回 true；
// 若受保護變更全是測試檔，或無受保護 .go 變更，則回 true。
func HasAccompanyingTests(paths []string, protected []string) bool {
	hasProdGo, hasTestGo := false, false
	for _, p := range paths {
		if !MatchProtected(p, protected) {
			continue
		}
		if strings.HasSuffix(p, "_test.go") {
			hasTestGo = true
		} else if strings.HasSuffix(p, ".go") {
			hasProdGo = true
		}
	}
	if !hasProdGo {
		return true
	}
	return hasTestGo
}

// SelfModNeedsApproval 回報是否因 self-mod guard 而需人工 approve 才能 merge。
// approved 為本次指令是否帶 --approve-self-mod flag。
func SelfModNeedsApproval(s protocol.State, approved bool) bool {
	return s.SelfModTouched && !s.SelfModApproved && !approved
}

// checkSelfMod 偵測本輪變更是否觸及受保護路徑，並把結果寫進 CheckResult。
// 觸及僅標記 + warn，不令 Pass=false（讓該輪能續跑 review/test）；
// 唯有受保護 diff 行數超過 policy.MaxDiffLines 時才 Pass=false（diff-budget 落到 needs-attention）。
func checkSelfMod(ws *protocol.Workspace, featureID string, detector ScopeDetector, r *CheckResult) {
	cfg, err := ws.ReadConfig()
	if err != nil {
		// 設定讀不到時套內建預設，仍對受保護路徑生效（fail-safe，不靜默關閉 guard）。
		cfg = protocol.Config{}
	}
	policy := ResolveSelfMod(cfg)

	var files []protocol.ChangedFile
	if sd, ok := detector.(SelfModDetector); ok && detector != nil {
		files = sd.DetectChangedFiles(featureID)
	} else {
		files = detectChangedFilesLocal(gitops.ScopeRoot(ws.Root, featureID))
	}

	hits := ProtectedHits(files, policy.ProtectedPaths)
	if len(hits) == 0 {
		return
	}

	totalLines := 0
	paths := make([]string, 0, len(hits))
	for _, h := range hits {
		paths = append(paths, h.Path)
		// *_test.go 不計入 diff budget：補測不應被行數上限逼成分批 commit 或
		// 犧牲可讀性；路徑仍列入 SelfModPaths 供 test-gate 判定「附帶測試」。
		if strings.HasSuffix(h.Path, "_test.go") {
			continue
		}
		totalLines += h.Lines
	}

	r.SelfModTouched = true
	r.SelfModPaths = paths
	r.SelfModDiffLines = totalLines
	r.Warns = append(r.Warns, fmt.Sprintf(
		"self-modification detected: 受保護路徑變更需人工 approve（%s）", strings.Join(paths, ", ")))

	if totalLines > policy.MaxDiffLines {
		r.Pass = false
		r.Errors = append(r.Errors, fmt.Sprintf(
			"self-mod: protected diff %d lines exceeds budget %d", totalLines, policy.MaxDiffLines))
	}
}

// detectChangedFilesLocal 是 SelfModDetector 取不到時的 fallback：直接在 root 跑 git 取檔案層變更。
// 與 gitops 的偵測語意一致（tracked numstat + untracked 行數），兩條指令各自獨立容錯。
func detectChangedFilesLocal(root string) []protocol.ChangedFile {
	var files []protocol.ChangedFile

	numstatCmd := exec.Command("git", "diff", "--numstat", "HEAD")
	numstatCmd.Dir = root
	if out, err := numstatCmd.Output(); err == nil {
		for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			if line == "" {
				continue
			}
			parts := strings.SplitN(line, "\t", 3)
			if len(parts) < 3 {
				continue
			}
			files = append(files, protocol.ChangedFile{
				Path:  parts[2],
				Lines: gitops.ParseNumstatCount(parts[0]) + gitops.ParseNumstatCount(parts[1]),
			})
		}
	}

	untrackedCmd := exec.Command("git", "ls-files", "--others", "--exclude-standard")
	untrackedCmd.Dir = root
	if out, err := untrackedCmd.Output(); err == nil {
		for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			if line == "" {
				continue
			}
			files = append(files, protocol.ChangedFile{Path: line, Lines: gitops.CountFileLines(filepath.Join(root, line))})
		}
	}

	return files
}
