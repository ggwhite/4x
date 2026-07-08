package main

import (
	"encoding/json"
	"io"
	"os"

	"github.com/ggwhite/4x/internal/guard"
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

// newGuardToolCmd 建立隱藏指令 `4x guard-tool`，供 claude runner 注入的 PreToolUse hook 呼叫。
// 從 stdin 讀 Claude Code hook JSON、從 env 讀 FOURX_ROLE / FOURX_REVIEW_PACKAGE，
// 對 reviewer/deep-reviewer 自跑的 git diff/log/show 回傳 deny 決策引導改讀 review-package.md。
// 任何 parse 失敗一律放行（exit 0，不阻斷 reviewer）。
func newGuardToolCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "guard-tool",
		Short:  "PreToolUse hook: intercept reviewer git exploration (machine use)",
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
			role := os.Getenv("FOURX_ROLE")
			pkg := os.Getenv("FOURX_REVIEW_PACKAGE")
			deny, reason := guard.EvaluateReviewerToolIntercept(in, role, pkg)
			if !deny {
				return nil
			}
			var out preToolUseOutput
			out.HookSpecificOutput.HookEventName = "PreToolUse"
			out.HookSpecificOutput.PermissionDecision = "deny"
			out.HookSpecificOutput.PermissionDecisionReason = reason
			enc := json.NewEncoder(os.Stdout)
			if err := enc.Encode(&out); err != nil {
				return nil // 輸出失敗也不阻斷
			}
			return nil
		},
	}
}
