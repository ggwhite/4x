package guard

import (
	"os"
	"strings"
)

// ToolHookInput 對應 Claude Code PreToolUse hook 從 stdin 餵入的 JSON：
// tool_name 為工具名稱（如 "Bash" / "Edit" / "Write" / "MultiEdit"），
// tool_input.command 為 Bash 指令字串，tool_input.file_path 為 Edit/Write/MultiEdit 的目標檔案。
type ToolHookInput struct {
	ToolName  string `json:"tool_name"`
	ToolInput struct {
		Command  string `json:"command"`
		FilePath string `json:"file_path"`
	} `json:"tool_input"`
}

// reviewerInterceptRoles 是唯一會被攔截 git 探索指令的角色集合。
var reviewerInterceptRoles = map[string]bool{
	"reviewer":      true,
	"deep-reviewer": true,
}

// EvaluateReviewerToolIntercept 判定 reviewer/deep-reviewer 的一個 Bash 指令是否應被
// PreToolUse hook deny（軟性引導改讀 review-package.md）。回傳 deny=true 時 reason 為引導訊息。
//
// 只在以下條件全部成立時 deny：
//   - ToolName == "Bash"
//   - role 屬 {reviewer, deep-reviewer}
//   - command 匹配 git 探索樣式（git diff / git log / git show，容忍前導空白與 env 前綴）
//   - reviewPackagePath 非空且該檔實際存在（保留 fallback：package 不存在時放行讓 reviewer 自跑 git）
//
// 其餘一律 deny=false。build/test/lint（make/go test…）與 git status/commit/add 永不匹配。
func EvaluateReviewerToolIntercept(in ToolHookInput, role, reviewPackagePath string) (deny bool, reason string) {
	if in.ToolName != "Bash" {
		return false, ""
	}
	if !reviewerInterceptRoles[role] {
		return false, ""
	}
	if !matchesGitExploration(in.ToolInput.Command) {
		return false, ""
	}
	if reviewPackagePath == "" {
		return false, ""
	}
	if _, err := os.Stat(reviewPackagePath); err != nil {
		return false, "" // 檔案不存在 → 保留 fallback，放行
	}
	return true, "本輪的 diff 與變更檔全文已預先產生於 " + reviewPackagePath +
		"。請改讀該檔取得變更內容，不要自跑 git diff/log/show。若需未 inline 的檔案，用 Read/grep/cat 讀取。"
}

// matchesGitExploration 判斷 command 是否為 git diff / git log / git show 探索指令。
// 容忍前導空白、多重空白、以及 `env X=1 git ...` 這類前綴；明確不匹配 make/build/test/lint
// 與 git status/commit/add 等非探索指令。
func matchesGitExploration(command string) bool {
	fields := strings.Fields(command)
	// 跳過形如 KEY=VAL 的 env 前綴（含明確的 `env` 指令）。
	for len(fields) > 0 {
		f := fields[0]
		if f == "env" || strings.Contains(f, "=") {
			fields = fields[1:]
			continue
		}
		break
	}
	if len(fields) < 2 {
		return false
	}
	if fields[0] != "git" {
		return false
	}
	switch fields[1] {
	case "diff", "log", "show":
		return true
	default:
		return false
	}
}
