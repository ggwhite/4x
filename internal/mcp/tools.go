package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/ggwhite/4x/internal/protocol"
)

// Handlers 持有 MCP 工具所需的依賴參照，包含指令執行函式與工作區根目錄。
type Handlers struct {
	Exec          ExecFunc
	WorkspaceRoot string
}

// StatusInput 為 4x_status 工具的輸入參數。
type StatusInput struct {
	FeatureID string `json:"featureId,omitempty" jsonschema:"description=Feature ID (optional — omit to list all)"`
}

// NewInput 為 4x_new 工具的輸入參數。
type NewInput struct {
	Name        string `json:"name" jsonschema:"description=Feature name,required"`
	Description string `json:"description,omitempty" jsonschema:"description=Feature description"`
}

// RunInput 為 4x_run 工具的輸入參數。
type RunInput struct {
	FeatureID string `json:"featureId" jsonschema:"description=Feature ID,required"`
	Runner    string `json:"runner,omitempty" jsonschema:"description=Runner plugin name"`
	MaxRounds int    `json:"maxRounds,omitempty" jsonschema:"description=Max iteration rounds"`
}

// StopInput 為 4x_stop 工具的輸入參數。
type StopInput struct {
	FeatureID string `json:"featureId" jsonschema:"description=Feature ID,required"`
}

// CheckInput 為 4x_check 工具的輸入參數。
type CheckInput struct {
	FeatureID string `json:"featureId" jsonschema:"description=Feature ID,required"`
}

// TransitionInput 為 4x_transition 工具的輸入參數。
type TransitionInput struct {
	FeatureID string `json:"featureId" jsonschema:"description=Feature ID,required"`
	To        string `json:"to" jsonschema:"description=Target phase (designing/coding/reviewing/deep-reviewing/testing/accepting/done/blocked),required"`
}

// Status 列出所有 feature 狀態，或查詢單一 feature 的詳細資訊。
func (h *Handlers) Status(ctx context.Context, input StatusInput) (json.RawMessage, error) {
	args := []string{"status", "--json"}
	if input.FeatureID != "" {
		args = []string{"status", input.FeatureID, "--json"}
	}
	return h.Exec(ctx, args...)
}

// New 建立新的 feature。
func (h *Handlers) New(ctx context.Context, input NewInput) (json.RawMessage, error) {
	args := []string{"new", input.Name, "--json"}
	return h.Exec(ctx, args...)
}

// Run 在背景啟動 feature 的 Design-Code-Review-Test 執行迴圈。
func (h *Handlers) Run(ctx context.Context, input RunInput) (json.RawMessage, error) {
	args := []string{"run", input.FeatureID, "--json"}
	if input.Runner != "" {
		args = append(args, "--runner", input.Runner)
	}
	if input.MaxRounds > 0 {
		args = append(args, "--max-rounds", fmt.Sprintf("%d", input.MaxRounds))
	}
	return h.Exec(ctx, args...)
}

// Stop 請求停止正在執行的 feature 迴圈。
//
// 採 signal file 而非直接改寫 state.json：避免與正在跑的 run loop 並行寫入時，
// 用過時快照覆蓋掉 loop 剛寫入的 phase／round 進度。本 handler 只寫「請求停止」
// 信號，由 run loop 作為 state.json 的唯一 writer 在下一輪開頭消費信號並收斂
// Active=false。語意為「請求」：若目標 feature 已無存活 loop，信號留待既有
// ReconcileActive 校正。
func (h *Handlers) Stop(ctx context.Context, input StopInput) (json.RawMessage, error) {
	ws := &protocol.Workspace{Root: h.WorkspaceRoot}
	featureID, err := ws.ResolveFeatureID(input.FeatureID)
	if err != nil {
		return nil, fmt.Errorf("resolve feature ID %s: %w", input.FeatureID, err)
	}
	if err := ws.RequestStop(featureID); err != nil {
		return nil, fmt.Errorf("request stop for %s: %w", featureID, err)
	}
	result, _ := json.Marshal(struct {
		FeatureID string `json:"featureId"`
		Stopped   bool   `json:"stopped"`
	}{
		FeatureID: featureID,
		Stopped:   true,
	})
	return result, nil
}

// Check 執行 feature 的 guardrail 安全檢查。
func (h *Handlers) Check(ctx context.Context, input CheckInput) (json.RawMessage, error) {
	return h.Exec(ctx, "check", input.FeatureID, "--json")
}

// Transition 手動將 feature 轉換到指定的 phase。
func (h *Handlers) Transition(ctx context.Context, input TransitionInput) (json.RawMessage, error) {
	return h.Exec(ctx, "transition", input.FeatureID, "--to", input.To, "--json")
}
