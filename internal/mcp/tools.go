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

// DoneInput 為 4x_done 工具的輸入參數。
type DoneInput struct {
	FeatureID      string `json:"featureId" jsonschema:"description=Feature ID,required"`
	ApproveSelfMod bool   `json:"approveSelfMod,omitempty" jsonschema:"description=Approve self-modification of protected paths so the feature can be merged"`
}

// VerifyInput 為 4x_verify 工具的輸入參數。
type VerifyInput struct {
	FeatureID string `json:"featureId" jsonschema:"description=Feature ID,required"`
	Round     int    `json:"round,omitempty" jsonschema:"description=Round number (default: current round from state.json)"`
}

// ApproveInput 為 4x_approve 工具的輸入參數。
type ApproveInput struct {
	FeatureID string `json:"featureId" jsonschema:"description=Feature ID,required"`
}

// RejectInput 為 4x_reject 工具的輸入參數。
type RejectInput struct {
	FeatureID string `json:"featureId" jsonschema:"description=Feature ID,required"`
}

// MineInput 為 4x_mine 工具的輸入參數。
type MineInput struct {
	MinOccurrences int    `json:"minOccurrences,omitempty" jsonschema:"description=Distinct-feature threshold to promote a fail-pattern into a candidate"`
	Output         string `json:"output,omitempty" jsonschema:"description=Candidate pool output path (default .4x/candidates.json)"`
	DryRun         bool   `json:"dryRun,omitempty" jsonschema:"description=Only print summary without writing the pool"`
}

// SubtaskInput 為 4x_subtask 工具的輸入參數。
type SubtaskInput struct {
	FeatureID string `json:"featureId" jsonschema:"description=Feature ID,required"`
	SubtaskID string `json:"subtaskId" jsonschema:"description=Subtask ID,required"`
	Status    string `json:"status" jsonschema:"description=New subtask status (not-started/in-progress/blocked/done/ready-for-review),required"`
}

// CleanInput 為 4x_clean 工具的輸入參數。
type CleanInput struct {
	FeatureID string `json:"featureId,omitempty" jsonschema:"description=Feature ID to clean (omit to consider all cleanable features)"`
	DryRun    bool   `json:"dryRun,omitempty" jsonschema:"description=List cleanable features without deleting"`
	Force     bool   `json:"force,omitempty" jsonschema:"description=Actually delete artifacts (without this only candidates are listed)"`
}

// LearnListInput 為 4x_learn_list 工具的輸入參數。
type LearnListInput struct {
	Category string `json:"category,omitempty" jsonschema:"description=Filter by learning category"`
}

// LearnPruneInput 為 4x_learn_prune 工具的輸入參數。
type LearnPruneInput struct {
	DryRun bool `json:"dryRun,omitempty" jsonschema:"description=Preview stale entries without removing"`
}

// LearnPromoteInput 為 4x_learn_promote 工具的輸入參數。
type LearnPromoteInput struct {
	ID string `json:"id" jsonschema:"description=Learning entry ID,required"`
}

// LearnRemoveInput 為 4x_learn_remove 工具的輸入參數。
type LearnRemoveInput struct {
	ID string `json:"id" jsonschema:"description=Learning entry ID,required"`
}

// EvolveInput 為 4x_evolve 工具的輸入參數。
// 注意：autoRun=true 會對排入的 feature 立即跑完整 meta-loop，屬長時間阻塞操作。
type EvolveInput struct {
	DryRun         bool   `json:"dryRun,omitempty" jsonschema:"description=Read-only analysis: print mine/dedupe summary without writing or spawning runners"`
	MinOccurrences int    `json:"minOccurrences,omitempty" jsonschema:"description=Distinct-feature threshold to promote a fail-pattern into a candidate"`
	Runner         string `json:"runner,omitempty" jsonschema:"description=Runner plugin for gate/enrich/auto-run"`
	AutoRun        bool   `json:"autoRun,omitempty" jsonschema:"description=Immediately run the meta-loop for enqueued features (long-blocking operation)"`
	Force          bool   `json:"force,omitempty" jsonschema:"description=Override anti-spin halt and force another round"`
	MaxRounds      int    `json:"maxRounds,omitempty" jsonschema:"description=Max rounds per feature during auto-run"`
	Timeout        int    `json:"timeout,omitempty" jsonschema:"description=LLM subprocess timeout in seconds"`
}

// ConfigGetInput 為 4x_config_get 工具的輸入參數。
type ConfigGetInput struct {
	Key string `json:"key" jsonschema:"description=Config key (e.g. locale, runner.claude.model, role.coder.model),required"`
}

// ConfigSetInput 為 4x_config_set 工具的輸入參數。
type ConfigSetInput struct {
	Key   string `json:"key" jsonschema:"description=Config key,required"`
	Value string `json:"value" jsonschema:"description=Config value,required"`
}

// GateInput 為 4x_gate 工具的輸入參數。
type GateInput struct {
	Phase string `json:"phase" jsonschema:"description=Gate phase: pre or post,required,enum=pre,enum=post"`
}

// MergeInput 為 4x_merge 工具的輸入參數。
type MergeInput struct {
	FeatureID      string `json:"featureId" jsonschema:"description=Feature ID,required"`
	ApproveSelfMod bool   `json:"approveSelfMod,omitempty" jsonschema:"description=Approve self-modification of protected paths so the feature can be merged"`
}

// Done 將 pending-review 的 feature 推進到 done（含自動 merge）。
func (h *Handlers) Done(ctx context.Context, input DoneInput) (json.RawMessage, error) {
	args := []string{"done", input.FeatureID}
	if input.ApproveSelfMod {
		args = append(args, "--approve-self-mod")
	}
	args = append(args, "--json")
	return h.Exec(ctx, args...)
}

// Verify 跑 feature 的 test-strategy 驗證指令。
func (h *Handlers) Verify(ctx context.Context, input VerifyInput) (json.RawMessage, error) {
	args := []string{"verify", input.FeatureID}
	if input.Round > 0 {
		args = append(args, "--round", fmt.Sprintf("%d", input.Round))
	}
	args = append(args, "--json")
	return h.Exec(ctx, args...)
}

// Approve 核可 draft feature（draft → not-started）。
func (h *Handlers) Approve(ctx context.Context, input ApproveInput) (json.RawMessage, error) {
	return h.Exec(ctx, "approve", input.FeatureID, "--json")
}

// Reject 否決 draft feature（draft → abandoned）。
func (h *Handlers) Reject(ctx context.Context, input RejectInput) (json.RawMessage, error) {
	return h.Exec(ctx, "reject", input.FeatureID, "--json")
}

// Mine 掃描 .4x/ 歷史失敗訊號，產出 candidate pool。
func (h *Handlers) Mine(ctx context.Context, input MineInput) (json.RawMessage, error) {
	args := []string{"mine"}
	if input.MinOccurrences > 0 {
		args = append(args, "--min-occurrences", fmt.Sprintf("%d", input.MinOccurrences))
	}
	if input.Output != "" {
		args = append(args, "--output", input.Output)
	}
	if input.DryRun {
		args = append(args, "--dry-run")
	}
	args = append(args, "--json")
	return h.Exec(ctx, args...)
}

// Subtask 更新 feature 內某個 subtask 的狀態。
func (h *Handlers) Subtask(ctx context.Context, input SubtaskInput) (json.RawMessage, error) {
	return h.Exec(ctx, "subtask", input.FeatureID, input.SubtaskID, "--status", input.Status, "--json")
}

// BatchNext 查詢下一個可執行的 feature。
func (h *Handlers) BatchNext(ctx context.Context, _ struct{}) (json.RawMessage, error) {
	return h.Exec(ctx, "batch", "next", "--json")
}

// BatchStop 送出 batch 停止信號。
func (h *Handlers) BatchStop(ctx context.Context, _ struct{}) (json.RawMessage, error) {
	return h.Exec(ctx, "batch", "stop", "--json")
}

// Clean 清理已完成 feature 的 workspace artifacts。未帶 force 時只列出 candidates 不刪除。
func (h *Handlers) Clean(ctx context.Context, input CleanInput) (json.RawMessage, error) {
	args := []string{"clean"}
	if input.FeatureID != "" {
		args = append(args, input.FeatureID)
	}
	if input.DryRun {
		args = append(args, "--dry-run")
	}
	if input.Force {
		args = append(args, "--force")
	}
	args = append(args, "--json")
	return h.Exec(ctx, args...)
}

// Doctor 跑專案環境健檢。
func (h *Handlers) Doctor(ctx context.Context, _ struct{}) (json.RawMessage, error) {
	return h.Exec(ctx, "doctor", "--json")
}

// LearnList 列出所有 retro learnings。
func (h *Handlers) LearnList(ctx context.Context, input LearnListInput) (json.RawMessage, error) {
	args := []string{"learn", "list"}
	if input.Category != "" {
		args = append(args, "--category", input.Category)
	}
	args = append(args, "--json")
	return h.Exec(ctx, args...)
}

// LearnPrune 移除過期的 learnings。
func (h *Handlers) LearnPrune(ctx context.Context, input LearnPruneInput) (json.RawMessage, error) {
	args := []string{"learn", "prune"}
	if input.DryRun {
		args = append(args, "--dry-run")
	}
	args = append(args, "--json")
	return h.Exec(ctx, args...)
}

// LearnPromote 將 learning 標記為 promoted。
func (h *Handlers) LearnPromote(ctx context.Context, input LearnPromoteInput) (json.RawMessage, error) {
	return h.Exec(ctx, "learn", "promote", input.ID, "--json")
}

// LearnRemove 移除指定的 learning 條目。
func (h *Handlers) LearnRemove(ctx context.Context, input LearnRemoveInput) (json.RawMessage, error) {
	return h.Exec(ctx, "learn", "remove", input.ID, "--json")
}

// Evolve 跑一輪自我進化 pipeline（mine → gate → enrich → enqueue，可選 auto-run）。
func (h *Handlers) Evolve(ctx context.Context, input EvolveInput) (json.RawMessage, error) {
	args := []string{"evolve"}
	if input.DryRun {
		args = append(args, "--dry-run")
	}
	if input.MinOccurrences > 0 {
		args = append(args, "--min-occurrences", fmt.Sprintf("%d", input.MinOccurrences))
	}
	if input.Runner != "" {
		args = append(args, "--runner", input.Runner)
	}
	if input.AutoRun {
		args = append(args, "--auto-run")
	}
	if input.Force {
		args = append(args, "--force")
	}
	if input.MaxRounds > 0 {
		args = append(args, "--max-rounds", fmt.Sprintf("%d", input.MaxRounds))
	}
	if input.Timeout > 0 {
		args = append(args, "--timeout", fmt.Sprintf("%d", input.Timeout))
	}
	args = append(args, "--json")
	return h.Exec(ctx, args...)
}

// ConfigGet 讀取單一 user config 值。
func (h *Handlers) ConfigGet(ctx context.Context, input ConfigGetInput) (json.RawMessage, error) {
	return h.Exec(ctx, "config", "get", input.Key, "--json")
}

// ConfigSet 設定單一 user config 值。
func (h *Handlers) ConfigSet(ctx context.Context, input ConfigSetInput) (json.RawMessage, error) {
	return h.Exec(ctx, "config", "set", input.Key, input.Value, "--json")
}

// ConfigList 列出完整 user config。
func (h *Handlers) ConfigList(ctx context.Context, _ struct{}) (json.RawMessage, error) {
	return h.Exec(ctx, "config", "list", "--json")
}

// Gate 對 candidate pool 套用價值閘門 veto。phase 須為 pre 或 post。
func (h *Handlers) Gate(ctx context.Context, input GateInput) (json.RawMessage, error) {
	switch input.Phase {
	case "pre":
		return h.Exec(ctx, "gate", "--pre", "--json")
	case "post":
		return h.Exec(ctx, "gate", "--post", "--json")
	default:
		return nil, fmt.Errorf("invalid gate phase %q: must be pre or post", input.Phase)
	}
}

// Merge 在解決衝突後完成 feature 的 merge。
func (h *Handlers) Merge(ctx context.Context, input MergeInput) (json.RawMessage, error) {
	args := []string{"merge", input.FeatureID}
	if input.ApproveSelfMod {
		args = append(args, "--approve-self-mod")
	}
	args = append(args, "--json")
	return h.Exec(ctx, args...)
}
