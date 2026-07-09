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
var sourceWritingRoles = map[protocol.Role]bool{
	protocol.RoleCoder: true,
	protocol.RoleFixer: true,
}

// EvaluateWritePath 是純函式的 role×path 寫入判定核心，供即時 PreToolUse gate 使用。
//
// relPath 必須是相對 workspace root 的乾淨路徑（以 "/" 分隔，呼叫端負責正規化）。
// 依序套用兩條規則，回傳第一個 deny：
//
//  1. role-source 規則：role ∉ {coder, fixer} 且 relPath 不在 `.4x/` 底下 → deny。
//  2. repo-scope 規則：len(f.Repos) > 0 且 relPath 不在 `.4x/` 底下，取第一段目錄為 repo；
//     若 repo 不在 f.Repos 也不在 hubRepos → deny（scope violation）。
//
// `.4x/` 底下的 artifact 路徑對兩條規則都放行（細粒度歸屬由事後 4x check 管）。
// f.Repos 為空（monorepo）時 scope 規則一律不 deny。
func EvaluateWritePath(f feature.Feature, role protocol.Role, relPath string, hubRepos map[string]bool) (deny bool, reason string) {
	if isArtifactPath(relPath) {
		return false, ""
	}

	// 規則 1：非 source-writing 角色不可寫 source。
	if !sourceWritingRoles[role] {
		return true, fmt.Sprintf("role %q cannot modify source file %q", role, relPath)
	}

	// 規則 2：repo 越界攔截（僅 multi-repo feature 適用）。
	if len(f.Repos) > 0 {
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
