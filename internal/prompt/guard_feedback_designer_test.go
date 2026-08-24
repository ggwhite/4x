package prompt

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	feat "github.com/ggwhite/4x/internal/feature"
	"github.com/ggwhite/4x/internal/protocol"
)

// TestGenerate_DesignerReceivesGuardFeedback 驗證 F188 designing 出口閘門在 precheck 失敗時
// 寫入的 guard-feedback.json 會被注入 Designer 的重試 prompt——反面案例：F188 上線時
// GuardFeedback 只對 RoleTester 填，Designer 重試讀不到自己被打回的原因，等於盲跑，多半
// 重產出同一份會再次觸發 precheck 失敗的 test-strategy.yaml（F188 gap）。
func TestGenerate_DesignerReceivesGuardFeedback(t *testing.T) {
	ws := newTestWorkspace(t)
	featureID := "F188-guardfb"
	if err := ws.InitFeatureDir(featureID); err != nil {
		t.Fatal(err)
	}
	feature := feat.Feature{ID: featureID, Name: "Test"}

	const errMsg = "AC-2: verify command 'go test ./... | grep -q PASS' swallows exit code"

	roundDir := ws.RoundDir(featureID, 0)
	if err := os.MkdirAll(roundDir, 0o755); err != nil {
		t.Fatal(err)
	}
	fb, err := json.Marshal(struct {
		Errors []string `json:"errors"`
	}{Errors: []string{errMsg}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(roundDir, protocol.GuardFeedback), fb, 0o644); err != nil {
		t.Fatal(err)
	}

	ctx := &Context{Ws: ws, RunnerWs: ws, Feature: feature, Cfg: protocol.Config{}}
	got, err := Generate(ctx, protocol.RoleDesigner, 0, 0, "")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if !strings.Contains(got, errMsg) {
		t.Errorf("designer prompt should contain guard feedback error %q, got:\n%s", errMsg, got)
	}
	if !strings.Contains(got, "GUARD RETRY") {
		t.Errorf("designer prompt should contain the GUARD RETRY header, got:\n%s", got)
	}
}

// TestGenerate_DesignerNoGuardFeedback_NoRetryBanner 驗證第一輪（無 guard-feedback.json）
// 不會誤渲染 GUARD RETRY 區塊，正反兩面都要驗證，避免只驗證正例讓渲染條件寬鬆到永遠 true。
func TestGenerate_DesignerNoGuardFeedback_NoRetryBanner(t *testing.T) {
	ws := newTestWorkspace(t)
	featureID := "F188-noguardfb"
	if err := ws.InitFeatureDir(featureID); err != nil {
		t.Fatal(err)
	}
	feature := feat.Feature{ID: featureID, Name: "Test"}

	ctx := &Context{Ws: ws, RunnerWs: ws, Feature: feature, Cfg: protocol.Config{}}
	got, err := Generate(ctx, protocol.RoleDesigner, 0, 0, "")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if strings.Contains(got, "GUARD RETRY") {
		t.Errorf("designer prompt should NOT contain GUARD RETRY banner without guard-feedback.json, got:\n%s", got)
	}
}
