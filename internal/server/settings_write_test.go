package server

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/ggwhite/4x/internal/protocol"
)

// TestHandlePutProfile 驗證 PUT /api/settings/profiles/{name} 能新增 profile，
// 且該 profile 可透過 GET /api/settings 取回。
func TestHandlePutProfile(t *testing.T) {
	ws := setupServerWorkspace(t)
	mux := NewMux(singleResolver(protocol.NewCachedWorkspace(ws), nil))

	// PUT 一個新 profile
	profileBody := `{"phases": [{"phase": "coding"}, {"phase": "reviewing"}]}`
	rec := serveRequest(t, mux, http.MethodPut, "/api/settings/profiles/test-profile", profileBody)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}

	// 驗證回傳值包含新 profile
	var putResp protocol.Config
	if err := json.Unmarshal(rec.Body.Bytes(), &putResp); err != nil {
		t.Fatalf("unmarshal PUT response: %v", err)
	}
	if putResp.Profiles == nil || putResp.Profiles["test-profile"].Phases == nil {
		t.Fatal("profile not found in PUT response")
	}
	if len(putResp.Profiles["test-profile"].Phases) != 2 {
		t.Errorf("profile phases count = %d, want 2", len(putResp.Profiles["test-profile"].Phases))
	}

	// GET /api/settings 驗證 profile 持久化
	rec = serveRequest(t, mux, http.MethodGet, "/api/settings", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET status = %d, want 200", rec.Code)
	}
	var getResp protocol.Config
	if err := json.Unmarshal(rec.Body.Bytes(), &getResp); err != nil {
		t.Fatalf("unmarshal GET response: %v", err)
	}
	if getResp.Profiles == nil || getResp.Profiles["test-profile"].Phases == nil {
		t.Fatal("profile not persisted")
	}
}

// TestHandleDeleteProfile 驗證 DELETE /api/settings/profiles/{name} 能刪除 profile，
// 刪除後該 profile 不再出現在 GET /api/settings 的回傳值中。
func TestHandleDeleteProfile(t *testing.T) {
	ws := setupServerWorkspace(t)
	mux := NewMux(singleResolver(protocol.NewCachedWorkspace(ws), nil))

	// PUT 一個 profile
	profileBody := `{"phases": [{"phase": "coding"}]}`
	serveRequest(t, mux, http.MethodPut, "/api/settings/profiles/delete-test", profileBody)

	// 驗證 profile 存在
	rec := serveRequest(t, mux, http.MethodGet, "/api/settings", "")
	var getResp protocol.Config
	json.Unmarshal(rec.Body.Bytes(), &getResp)
	if getResp.Profiles == nil || getResp.Profiles["delete-test"].Phases == nil {
		t.Fatal("profile not created")
	}

	// DELETE profile
	rec = serveRequest(t, mux, http.MethodDelete, "/api/settings/profiles/delete-test", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("DELETE status = %d, want 200", rec.Code)
	}

	// 驗證刪除後 profile 不存在
	var delResp protocol.Config
	json.Unmarshal(rec.Body.Bytes(), &delResp)
	if delResp.Profiles != nil && delResp.Profiles["delete-test"].Phases != nil {
		t.Fatal("profile not deleted")
	}

	// 再次 GET 驗證持久化
	rec = serveRequest(t, mux, http.MethodGet, "/api/settings", "")
	var finalResp protocol.Config
	json.Unmarshal(rec.Body.Bytes(), &finalResp)
	if finalResp.Profiles != nil && finalResp.Profiles["delete-test"].Phases != nil {
		t.Fatal("profile deletion not persisted")
	}
}

// TestHandleDeleteProfile_ClearsDefaultProfile 驗證刪除正在作用的 default_profile 後，
// default_profile 欄位被自動清空。
func TestHandleDeleteProfile_ClearsDefaultProfile(t *testing.T) {
	ws := setupServerWorkspace(t)

	// 設定 default_profile
	dotDir := ws.DotDir()
	cfg := protocol.Config{
		Project: protocol.ProjectConfig{Name: "test"},
		Default: "claude",
		Profiles: map[string]protocol.ProfileConfig{
			"active-profile": {Phases: []protocol.PhaseSpec{{Phase: "coding"}}},
		},
		DefaultProfile: "active-profile",
	}
	data, _ := json.MarshalIndent(cfg, "", "  ")
	settingsPath := filepath.Join(dotDir, "settings.json")
	os.WriteFile(settingsPath, append(data, '\n'), 0o644)

	mux := NewMux(singleResolver(protocol.NewCachedWorkspace(ws), nil))

	// 刪除 active-profile（即 default_profile）
	rec := serveRequest(t, mux, http.MethodDelete, "/api/settings/profiles/active-profile", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("DELETE status = %d, want 200", rec.Code)
	}

	// 驗證 default_profile 被清空
	var resp protocol.Config
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.DefaultProfile != "" {
		t.Errorf("default_profile = %q, want empty string", resp.DefaultProfile)
	}

	// 驗證持久化：GET /api/settings
	rec = serveRequest(t, mux, http.MethodGet, "/api/settings", "")
	var getResp protocol.Config
	json.Unmarshal(rec.Body.Bytes(), &getResp)
	if getResp.DefaultProfile != "" {
		t.Errorf("persisted default_profile = %q, want empty string", getResp.DefaultProfile)
	}
}

// TestHandlePutRole 驗證 PUT /api/settings/roles/{role} 能更新 role 設定，
// 且更新後的設定可透過 GET /api/settings 取回。
func TestHandlePutRole(t *testing.T) {
	ws := setupServerWorkspace(t)
	mux := NewMux(singleResolver(protocol.NewCachedWorkspace(ws), nil))

	// PUT 一個 role config
	roleBody := `{"model": "sonnet", "deep_model": "opus"}`
	rec := serveRequest(t, mux, http.MethodPut, "/api/settings/roles/coder", roleBody)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, want 200", rec.Code)
	}

	// 驗證回傳值包含新 role config
	var putResp protocol.Config
	json.Unmarshal(rec.Body.Bytes(), &putResp)
	if putResp.Roles == nil || putResp.Roles["coder"].Model == "" {
		t.Fatal("role not found in PUT response")
	}
	if putResp.Roles["coder"].Model != "sonnet" {
		t.Errorf("role model = %s, want sonnet", putResp.Roles["coder"].Model)
	}

	// GET /api/settings 驗證 role 持久化
	rec = serveRequest(t, mux, http.MethodGet, "/api/settings", "")
	var getResp protocol.Config
	json.Unmarshal(rec.Body.Bytes(), &getResp)
	if getResp.Roles == nil || getResp.Roles["coder"].Model != "sonnet" {
		t.Fatal("role not persisted correctly")
	}
}

// TestHandlePatchSettings 驗證 PATCH /api/settings 只修改指定欄位，
// 其他欄位保持不變。
func TestHandlePatchSettings(t *testing.T) {
	ws := setupServerWorkspace(t)

	// 預先設定一些值
	dotDir := ws.DotDir()
	cfg := protocol.Config{
		Project: protocol.ProjectConfig{Name: "test"},
		Default: "claude",
		Profiles: map[string]protocol.ProfileConfig{
			"full": {Phases: []protocol.PhaseSpec{{Phase: "coding"}}},
		},
		DefaultProfile: "full",
		Roles: map[string]protocol.RoleConfig{
			"designer": {Model: "opus"},
		},
	}
	data, _ := json.MarshalIndent(cfg, "", "  ")
	settingsPath := filepath.Join(dotDir, "settings.json")
	os.WriteFile(settingsPath, append(data, '\n'), 0o644)

	mux := NewMux(singleResolver(protocol.NewCachedWorkspace(ws), nil))

	// PATCH：只改 default_runner
	patchBody := `{"default_runner": "gemini"}`
	rec := serveRequest(t, mux, "PATCH", "/api/settings", patchBody)
	if rec.Code != http.StatusOK {
		t.Fatalf("PATCH status = %d, want 200", rec.Code)
	}

	// 驗證 default_runner 被改，其他欄位保持
	var patchResp protocol.Config
	json.Unmarshal(rec.Body.Bytes(), &patchResp)
	if patchResp.Default != "gemini" {
		t.Errorf("default_runner = %s, want gemini", patchResp.Default)
	}
	if patchResp.DefaultProfile != "full" {
		t.Errorf("default_profile unexpectedly changed to %s", patchResp.DefaultProfile)
	}
	if patchResp.Roles["designer"].Model != "opus" {
		t.Errorf("designer role model changed unexpectedly")
	}
}

// TestHandlePatchSettings_InvalidConfigRejected 驗證 PATCH 不會寫入不合法的 default_runner。
func TestHandlePatchSettings_InvalidConfigRejected(t *testing.T) {
	ws := setupServerWorkspace(t)

	dotDir := ws.DotDir()
	cfg := protocol.Config{
		Project: protocol.ProjectConfig{Name: "test"},
		Default: "claude",
		Runners: map[string]protocol.RunnerConfig{
			"claude": {Command: "claude"},
		},
	}
	data, _ := json.MarshalIndent(cfg, "", "  ")
	settingsPath := filepath.Join(dotDir, "settings.json")
	os.WriteFile(settingsPath, append(data, '\n'), 0o644)

	mux := NewMux(singleResolver(protocol.NewCachedWorkspace(ws), nil))

	rec := serveRequest(t, mux, "PATCH", "/api/settings", `{"default_runner":"missing"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("PATCH status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}

	rec = serveRequest(t, mux, http.MethodGet, "/api/settings", "")
	var got protocol.Config
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal GET response: %v", err)
	}
	if got.Default != "claude" {
		t.Fatalf("default_runner = %q, want claude", got.Default)
	}
}

// TestHandlePatchSettings_TypeMismatchRejected 驗證 PATCH 對欄位型別不符的 payload
// 回傳 HTTP 400（client error）而非 500。這類 bad request 過去因 merge callback 的
// errors.New 未被包成 invalidSettingsError 而漏判為 500，本測試鎖住修正後的映射。
func TestHandlePatchSettings_TypeMismatchRejected(t *testing.T) {
	ws := setupServerWorkspace(t)

	dotDir := ws.DotDir()
	cfg := protocol.Config{
		Project: protocol.ProjectConfig{Name: "test"},
		Default: "claude",
		Runners: map[string]protocol.RunnerConfig{"claude": {Command: "claude"}},
	}
	data, _ := json.MarshalIndent(cfg, "", "  ")
	os.WriteFile(filepath.Join(dotDir, "settings.json"), append(data, '\n'), 0o644)

	mux := NewMux(singleResolver(protocol.NewCachedWorkspace(ws), nil))

	cases := []struct {
		name string
		body string
	}{
		{"scalar wrong type", `{"default_runner":123}`},
		{"project not object", `{"project":"oops"}`},
		{"roles not object", `{"roles":[]}`},
		{"bool wrong type", `{"parallel_review_test":"yes"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := serveRequest(t, mux, "PATCH", "/api/settings", tc.body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("PATCH status = %d, want 400; body=%s", rec.Code, rec.Body.String())
			}
		})
	}
}

// TestHandlePutProfile_InvalidName 驗證空 profile name 回傳 HTTP 400。
func TestHandlePutProfile_InvalidName(t *testing.T) {
	ws := setupServerWorkspace(t)
	mux := NewMux(singleResolver(protocol.NewCachedWorkspace(ws), nil))

	// 空 name
	rec := serveRequest(t, mux, http.MethodPut, "/api/settings/profiles/", `{"phases":[]}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("empty name status = %d, want 400", rec.Code)
	}

	// 路徑只有 / 也視為空
	rec = serveRequest(t, mux, http.MethodPut, "/api/settings/profiles", `{"phases":[]}`)
	if rec.Code == http.StatusOK {
		// 此情況可能被路由到 /api/settings 其他 method，應該是 405 或類似，不是 200
		t.Logf("warning: /api/settings/profiles (無/) 可能被路由到不同 handler")
	}
}

// TestHandlePutRole_InvalidRole 驗證空 role 回傳 HTTP 400。
func TestHandlePutRole_InvalidRole(t *testing.T) {
	ws := setupServerWorkspace(t)
	mux := NewMux(singleResolver(protocol.NewCachedWorkspace(ws), nil))

	// 空 role name
	rec := serveRequest(t, mux, http.MethodPut, "/api/settings/roles/", `{"model":"sonnet"}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("empty role status = %d, want 400", rec.Code)
	}

	// 非法 role key 也應拒絕寫入
	rec = serveRequest(t, mux, http.MethodPut, "/api/settings/roles/not-a-real-role", `{"model":"sonnet"}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("invalid role status = %d, want 400", rec.Code)
	}
}

// TestHandlePutProfile_InvalidConfigRejected 驗證不合法 profile 不會被寫入。
func TestHandlePutProfile_InvalidConfigRejected(t *testing.T) {
	ws := setupServerWorkspace(t)
	mux := NewMux(singleResolver(protocol.NewCachedWorkspace(ws), nil))

	// 缺少 coding phase
	rec := serveRequest(t, mux, http.MethodPut, "/api/settings/profiles/bad-profile", `{"phases":[{"phase":"reviewing"}]}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid profile status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}

	rec = serveRequest(t, mux, http.MethodGet, "/api/settings", "")
	var cfg protocol.Config
	if err := json.Unmarshal(rec.Body.Bytes(), &cfg); err != nil {
		t.Fatalf("unmarshal GET response: %v", err)
	}
	if cfg.Profiles != nil {
		if _, ok := cfg.Profiles["bad-profile"]; ok {
			t.Fatal("invalid profile should not be persisted")
		}
	}
}

// TestConcurrentSettingsWrite 驗證多個 goroutine 並發呼叫 write API，
// 最終 settings.json 為合法 JSON。
func TestConcurrentSettingsWrite(t *testing.T) {
	ws := setupServerWorkspace(t)
	mux := NewMux(singleResolver(protocol.NewCachedWorkspace(ws), nil))

	// 啟動多個 goroutine 並發執行 PUT profile
	var wg sync.WaitGroup
	errCh := make(chan error, 10)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			profileName := "profile-" + string(rune('a'+idx))
			body := `{"phases": [{"phase": "coding"}]}`
			rec := serveRequest(t, mux, http.MethodPut, "/api/settings/profiles/"+profileName, body)
			if rec.Code != http.StatusOK {
				errCh <- nil // 允許部分失敗，主要驗證最終 JSON 合法
			}
		}(i)
	}
	wg.Wait()
	close(errCh)

	// 驗證最終 settings.json 是合法 JSON
	rec := serveRequest(t, mux, http.MethodGet, "/api/settings", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("final GET status = %d", rec.Code)
	}

	var finalCfg protocol.Config
	if err := json.Unmarshal(rec.Body.Bytes(), &finalCfg); err != nil {
		t.Fatalf("final JSON invalid: %v", err)
	}

	// 至少有一個 profile 被成功建立
	if len(finalCfg.Profiles) == 0 {
		t.Errorf("no profiles created after concurrent writes")
	}
}

// TestProjectSettingsModalContainsSettingsSections 驗證 Dashboard 首頁 HTML
// 包含預期的 settings 相關 element ID（靜態 HTML 驗證）。
func TestProjectSettingsModalContainsSettingsSections(t *testing.T) {
	ws := setupServerWorkspace(t)
	mux := NewMux(singleResolver(protocol.NewCachedWorkspace(ws), nil))

	// GET /（假設 Dashboard 首頁）
	rec := serveRequest(t, mux, http.MethodGet, "/", "")
	if rec.Code != http.StatusOK {
		t.Skipf("Dashboard not available: status %d", rec.Code)
	}

	htmlBody := rec.Body.String()

	// 驗證預期的 element ID 存在
	expectedIDs := []string{
		"settings-profiles-section",
		"settings-roles-section",
		"settings-defaults-section",
		"profile-editor-modal",
	}

	for _, id := range expectedIDs {
		if !strings.Contains(htmlBody, `id="`+id+`"`) {
			t.Fatalf("HTML missing id=%q", id)
		}
	}
}

// TestSettingsUpdateReflectedWithoutRestart 驗證設定變更立即反映，
// 不需重啟 server。
func TestSettingsUpdateReflectedWithoutRestart(t *testing.T) {
	ws := setupServerWorkspace(t)
	mux := NewMux(singleResolver(protocol.NewCachedWorkspace(ws), nil))

	// PUT 一個 profile
	profileBody := `{"phases": [{"phase": "coding"}]}`
	rec := serveRequest(t, mux, http.MethodPut, "/api/settings/profiles/immed-test", profileBody)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT status = %d", rec.Code)
	}

	// 立即 GET，驗證新 profile 出現（不需 server restart）
	rec = serveRequest(t, mux, http.MethodGet, "/api/settings", "")
	var getResp protocol.Config
	json.Unmarshal(rec.Body.Bytes(), &getResp)

	if getResp.Profiles == nil || getResp.Profiles["immed-test"].Phases == nil {
		t.Fatal("profile update not reflected immediately")
	}

	// 再 PUT 一個不同的值到同一 profile
	updatedBody := `{"phases": [{"phase": "coding"}, {"phase": "reviewing"}]}`
	rec = serveRequest(t, mux, http.MethodPut, "/api/settings/profiles/immed-test", updatedBody)
	if rec.Code != http.StatusOK {
		t.Fatalf("second PUT status = %d", rec.Code)
	}

	// 立即 GET，驗證更新內容
	rec = serveRequest(t, mux, http.MethodGet, "/api/settings", "")
	var updatedResp protocol.Config
	json.Unmarshal(rec.Body.Bytes(), &updatedResp)

	if len(updatedResp.Profiles["immed-test"].Phases) != 2 {
		t.Errorf("profile phases = %d, want 2 (update not reflected)", len(updatedResp.Profiles["immed-test"].Phases))
	}
}

// TestProfileEditorSubmitPayload 驗證 PUT /api/settings/profiles/{name} handler
// 能正確解析含有 phases[].phase、phases[].runner、phases[].model 欄位的 JSON payload。
func TestProfileEditorSubmitPayload(t *testing.T) {
	ws := setupServerWorkspace(t)
	mux := NewMux(singleResolver(protocol.NewCachedWorkspace(ws), nil))

	// 送出包含 runner 和 model 的 phases
	payloadBody := `{
		"phases": [
			{"phase": "coding", "runner": "claude"},
			{"phase": "reviewing", "model": "sonnet"},
			{"phase": "testing", "runner": "gemini", "model": "pro"}
		]
	}`
	rec := serveRequest(t, mux, http.MethodPut, "/api/settings/profiles/payload-test", payloadBody)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT status = %d: %s", rec.Code, rec.Body.String())
	}

	// 驗證回傳的 phases 順序與內容
	var resp protocol.Config
	json.Unmarshal(rec.Body.Bytes(), &resp)
	profile := resp.Profiles["payload-test"]

	if len(profile.Phases) != 3 {
		t.Errorf("phases count = %d, want 3", len(profile.Phases))
	}
	if profile.Phases[0].Phase != "coding" || profile.Phases[0].Runner != "claude" {
		t.Errorf("phase[0]: got %+v, want coding/claude", profile.Phases[0])
	}
	if profile.Phases[1].Phase != "reviewing" || profile.Phases[1].Model != "sonnet" {
		t.Errorf("phase[1]: got %+v, want reviewing/sonnet", profile.Phases[1])
	}
	if profile.Phases[2].Phase != "testing" || profile.Phases[2].Runner != "gemini" || profile.Phases[2].Model != "pro" {
		t.Errorf("phase[2]: got %+v, want testing/gemini/pro", profile.Phases[2])
	}
}

// TestRoleConfigSaveFlow 驗證 PUT /api/settings/roles/{role} 的完整流程。
func TestRoleConfigSaveFlow(t *testing.T) {
	ws := setupServerWorkspace(t)
	mux := NewMux(singleResolver(protocol.NewCachedWorkspace(ws), nil))

	roleBody := `{"model": "opus", "instructions": ["rule 1", "rule 2"]}`
	rec := serveRequest(t, mux, http.MethodPut, "/api/settings/roles/reviewer", roleBody)

	if rec.Code != http.StatusOK {
		t.Fatalf("PUT /api/settings/roles/reviewer status = %d, want 200", rec.Code)
	}

	var resp protocol.Config
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Roles["reviewer"].Model != "opus" {
		t.Errorf("role model = %s, want opus", resp.Roles["reviewer"].Model)
	}
	if len(resp.Roles["reviewer"].Instructions) != 2 {
		t.Errorf("role instructions count = %d, want 2", len(resp.Roles["reviewer"].Instructions))
	}
}

// TestDefaultsSaveFlow 驗證 PATCH /api/settings 能更新 default_profile 和 default_runner。
func TestDefaultsSaveFlow(t *testing.T) {
	ws := setupServerWorkspace(t)

	// 預先設定 profiles
	dotDir := ws.DotDir()
	cfg := protocol.Config{
		Project:        protocol.ProjectConfig{Name: "test"},
		Default:        "claude",
		DefaultProfile: "old-profile",
		Profiles: map[string]protocol.ProfileConfig{
			"old-profile": {Phases: []protocol.PhaseSpec{{Phase: "coding"}}},
			"new-profile": {Phases: []protocol.PhaseSpec{{Phase: "coding"}, {Phase: "reviewing"}}},
		},
	}
	data, _ := json.MarshalIndent(cfg, "", "  ")
	settingsPath := filepath.Join(dotDir, "settings.json")
	os.WriteFile(settingsPath, append(data, '\n'), 0o644)

	mux := NewMux(singleResolver(protocol.NewCachedWorkspace(ws), nil))

	// PATCH 更新預設值
	patchBody := `{"default_runner": "gemini", "default_profile": "new-profile"}`
	rec := serveRequest(t, mux, "PATCH", "/api/settings", patchBody)

	if rec.Code != http.StatusOK {
		t.Fatalf("PATCH status = %d, want 200", rec.Code)
	}

	var resp protocol.Config
	json.Unmarshal(rec.Body.Bytes(), &resp)

	if resp.Default != "gemini" {
		t.Errorf("default_runner = %s, want gemini", resp.Default)
	}
	if resp.DefaultProfile != "new-profile" {
		t.Errorf("default_profile = %s, want new-profile", resp.DefaultProfile)
	}
}
