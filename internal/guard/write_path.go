package guard

import (
	"fmt"
	"strings"

	"github.com/ggwhite/4x/internal/feature"
	"github.com/ggwhite/4x/internal/protocol"
	"github.com/ggwhite/4x/internal/state"
)

// sourceWritingRoles 是唯一可寫「source」（`.4x/` 目錄以外檔案）的角色集合。
// 其餘角色（designer, design-reviewer, reviewer, deep-reviewer, tester, acceptor）
// 寫 source 會被即時 gate deny；寫 `.4x/` 底下的 artifact 不受此規則限制。
//
// RoleMiniCoder（deep-reviewing/reviewing 自癒收斂迴圈的子 role，見
// internal/orchestrator/deep_review.go、review_converge.go 的 s.Role = protocol.RoleMiniCoder）
// 明確納入：mini-coder 在收斂迴圈中會寫 source 修正問題，權威 `4x check` 並無角色白名單限制
// （checkScope 只查 repo scope，不查 role），若即時 gate 缺此角色會誤擋 mini-coder 合法寫入，
// 卡死收斂迴圈——比 4x check 更嚴格，違反本檔頂層原則。
var sourceWritingRoles = map[protocol.Role]bool{
	protocol.RoleCoder:     true,
	protocol.RoleFixer:     true,
	protocol.RoleMiniCoder: true,
}

// scopeGapsPath 是 `.4x/plugins/CLAUDE.md`「Scope Gaps (Non-Blocking Discoveries)」段落
// 明訂、任何 role 皆可 append 的跨 feature 通道檔案。權威 4x check 的 checkScope 不對此路徑
// 做角色限制（scope-gaps 通道本就設計給所有角色使用），即時 gate 的 rule-1（非 coder/fixer/
// mini-coder 寫非 .4x 檔即 deny）若未放行此路徑會誤擋非 coder/fixer 角色的合法 append。
const scopeGapsPath = "docs/reference/discovered-feature-gaps.md"

// EvaluateWritePath 是純函式的 role×path 寫入判定核心，供即時 PreToolUse gate 使用。
//
// relPath 必須是相對 workspace root 的乾淨路徑（以 "/" 分隔，呼叫端負責正規化）。
// 依序套用兩條規則，回傳第一個 deny：
//
//  1. role-source 規則：role ∉ sourceWritingRoles（coder/fixer/mini-coder）且 relPath
//     不在 `.4x/` 底下 → deny。
//  2. repo-scope 規則：len(f.Repos) > 0、relPath 含 "/"（非 root-level 檔案）且
//     relPath 不在 `.4x/` 底下，取第一段目錄為 repo；若 repo 不在 f.Repos 也不在
//     hubRepos → deny（scope violation）。
//
// `.4x/` 底下的 artifact 路徑、scopeGapsPath 對兩條規則都放行（細粒度歸屬由事後 4x check 管）。
// f.Repos 為空（monorepo）時、或 relPath 為 root-level 檔案時，scope 規則一律不 deny。
func EvaluateWritePath(f feature.Feature, role protocol.Role, relPath string, hubRepos map[string]bool) (deny bool, reason string) {
	if isArtifactPath(relPath) {
		return false, ""
	}

	// scope-gaps 通道對所有 role 放行，兩條規則皆略過（見 scopeGapsPath 註解）。
	if relPath == scopeGapsPath {
		return false, ""
	}

	// 規則 1：非 source-writing 角色不可寫 source。role 為空字串（理論上不應發生：
	// EvaluateWritePathForRun 在 state.Role 為空時已用 PhaseToRole 推導；此處仍防禦處理，
	// 避免未來新呼叫端直接傳空 role）一律落入 deny，reason 明確標示 empty，避免與真實
	// role 名稱混淆。
	if !sourceWritingRoles[role] {
		roleLabel := string(role)
		if roleLabel == "" {
			roleLabel = "(empty)"
		}
		return true, fmt.Sprintf("role %q cannot modify source file %q", roleLabel, relPath)
	}

	// 規則 2：repo 越界攔截（僅 multi-repo feature 適用）。
	// 對齊 detectChangedRepos（internal/guard/check.go）：沒有 "/" 的 root-level 檔案
	// （Makefile、go.mod 等）不是任何 repo 底下的檔案，不可當成 repo 名稱比對，
	// 否則會把整個檔名誤判為 repo 而誤報 scope violation。
	if len(f.Repos) > 0 && strings.Contains(relPath, "/") {
		repo := firstSegment(relPath)
		if !inRepoSet(repo, f.Repos) && !hubRepos[repo] {
			return true, fmt.Sprintf("scope violation: repo %q not in feature repos", repo)
		}
	}

	return false, ""
}

// EvaluateWritePathForRun 是 EvaluateWritePath 的 run-wiring 包裝：從 workspace 讀出
// state（取 role，Role 為空時以 state.PhaseToRole(Phase) 推導）、feature、config（組 hubRepos），
// 再委派給純函式判定。
//
// 任一讀取步驟出錯（ReadState/LoadFeature/ReadConfig 失敗）一律 fail-open 回 (false, "")：
// 即時 gate 是「提早示警」最佳化，不可因暫時性讀取錯誤擋住合法工作；權威 fail-closed 仍是事後
// 4x check。特別是 config 讀取失敗時若以空 hubRepos 繼續判定，會把寫入 hub repo 的合法路徑
// 誤判為 scope violation，故 config 出錯必須一併 fail-open。
func EvaluateWritePathForRun(ws *protocol.Workspace, featureID, relPath string) (deny bool, reason string) {
	st, err := ws.ReadState(featureID)
	if err != nil {
		return false, ""
	}
	role := st.Role
	if role == "" {
		role = state.PhaseToRole(st.Phase)
	}

	f, err := ws.LoadFeature(featureID)
	if err != nil {
		return false, ""
	}

	cfg, err := ws.ReadConfig()
	if err != nil {
		return false, ""
	}
	hubRepos := make(map[string]bool)
	for _, h := range protocol.EffectiveHubRepos(cfg) {
		hubRepos[h] = true
	}

	// e2e repo（test-strategy.yaml e2e_repos）在 testing phase（含）之後的合法寫入放行，
	// 對齊 checkScope（internal/guard/check.go）的 e2eAllowedPhase / testingHasRun 判定與
	// e2eRepoSet 建構邏輯——checkScope 把 e2e_repos 併入合法 repo 集合，即時 gate 若漏了這段
	// 會比 4x check 更嚴格，誤擋 e2e repo 的合法寫入。讀 test-strategy 失敗時維持不放行
	// （fail-safe，比照 hubRepos 讀 config 失敗維持空 map 的慣例）。合入 hubRepos 一併傳給
	// EvaluateWritePath：兩者語意相同——「呼叫端已判定此路徑對此次寫入無條件放行的 repo」。
	e2eAllowed := e2eAllowedPhase(st.Phase) ||
		(st.Phase == protocol.PhaseAmending && testingHasRun(ws, featureID))
	if e2eAllowed {
		if ts, tsErr := ws.ReadTestStrategy(featureID); tsErr == nil {
			for _, repo := range ts.E2ERepos {
				hubRepos[repo] = true
			}
		}
	}

	return EvaluateWritePath(f, role, relPath, hubRepos)
}

// isArtifactPath 判斷相對路徑是否落在 `.4x/` 目錄底下（含 `.4x` 本身）。
func isArtifactPath(relPath string) bool {
	return relPath == protocol.DirName || strings.HasPrefix(relPath, protocol.DirName+"/")
}

// firstSegment 取相對路徑的第一段目錄名（repo 名）。
func firstSegment(relPath string) string {
	return strings.SplitN(relPath, "/", 2)[0]
}

// inRepoSet 判斷 repo 是否在允許清單內。
func inRepoSet(repo string, repos []string) bool {
	for _, r := range repos {
		if r == repo {
			return true
		}
	}
	return false
}
