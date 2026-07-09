package main

import (
	"encoding/json"
	"io"
	"os"

	"github.com/ggwhite/4x/internal/guard"
	"github.com/ggwhite/4x/internal/protocol"
	"github.com/spf13/cobra"
)

// preToolUseOutput 對應 Claude Code PreToolUse hook 期望的決策 JSON 結構。
type preToolUseOutput struct {
	HookSpecificOutput struct {
		HookEventName            string `json:"hookEventName"`
		PermissionDecision       string `json:"permissionDecision"`
		PermissionDecisionReason string `json:"permissionDecisionReason"`
	} `json:"hookSpecificOutput"`
}

// writeToolNames 是會觸發 role-scope 寫入 gate 的工具集合（帶 file_path）。
var writeToolNames = map[string]bool{
	"Edit":      true,
	"Write":     true,
	"MultiEdit": true,
}

// newGuardToolCmd 建立隱藏指令 `4x guard-tool`，供 claude runner 注入的 PreToolUse hook 呼叫。
// 從 stdin 讀 Claude Code hook JSON、從 env 讀 FOURX_ROLE / FOURX_REVIEW_PACKAGE，分派：
//   - Bash → 對 reviewer/deep-reviewer 自跑的 git diff/log/show 回傳 deny 引導改讀 review-package.md。
//   - Edit/Write/MultiEdit → 依當前 role 對越界 / 非法寫入 source 回傳 deny（role-scope 即時 gate）。
//
// 任何 parse / 讀取失敗一律放行（exit 0，不阻斷），維持既有 fail-open 慣例。
func newGuardToolCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "guard-tool",
		Short:  "PreToolUse hook: intercept reviewer git exploration and out-of-scope writes (machine use)",
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			data, err := io.ReadAll(os.Stdin)
			if err != nil {
				return nil // 讀不到輸入 → 放行
			}
			var in guard.ToolHookInput
			if err := json.Unmarshal(data, &in); err != nil {
				return nil // parse 失敗 → 放行
			}

			switch {
			case in.ToolName == "Bash":
				role := os.Getenv("FOURX_ROLE")
				pkg := os.Getenv("FOURX_REVIEW_PACKAGE")
				if deny, reason := guard.EvaluateReviewerToolIntercept(in, role, pkg); deny {
					emitDeny(reason)
				}
			case writeToolNames[in.ToolName] && in.ToolInput.FilePath != "":
				if deny, reason := evaluateWriteHook(in.ToolInput.FilePath); deny {
					emitDeny(reason)
				}
			}
			return nil
		},
	}
}

// evaluateWriteHook 對 Edit/Write/MultiEdit 的目標檔案跑 role-scope 寫入判定。
// 一律 fail-open：任何錯誤（非 4x 專案、無法解析 feature、無 state.json、路徑在 workspace 外）→ 放行。
func evaluateWriteHook(filePath string) (bool, string) {
	cwd, err := os.Getwd()
	if err != nil {
		return false, ""
	}
	ws, err := protocol.Find(cwd)
	if err != nil {
		return false, ""
	}
	featureID, ok := resolvePathFeatureID(ws, nil)
	if !ok {
		return false, ""
	}
	if _, err := ws.ReadState(featureID); err != nil {
		return false, ""
	}
	relPath, ok := relWithinWorkspace(ws.Root, cwd, filePath)
	if !ok {
		return false, ""
	}
	return guard.EvaluateWritePathForRun(ws, featureID, relPath)
}

// emitDeny 輸出 Claude Code PreToolUse hook 期望的 deny 決策 JSON（輸出失敗也不阻斷）。
func emitDeny(reason string) {
	var out preToolUseOutput
	out.HookSpecificOutput.HookEventName = "PreToolUse"
	out.HookSpecificOutput.PermissionDecision = "deny"
	out.HookSpecificOutput.PermissionDecisionReason = reason
	_ = json.NewEncoder(os.Stdout).Encode(&out)
}
