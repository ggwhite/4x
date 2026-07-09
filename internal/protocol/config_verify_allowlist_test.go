package protocol

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestProjectConfig_VerifyCommandAllowlist_RoundTrip 驗證 verify_command_allowlist 欄位
// 可 JSON unmarshal→marshal round-trip 保值（AC-1）。
func TestProjectConfig_VerifyCommandAllowlist_RoundTrip(t *testing.T) {
	raw := `{"name":"demo","verify_command_allowlist":["make","go test"]}`

	var pc ProjectConfig
	if err := json.Unmarshal([]byte(raw), &pc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	want := []string{"make", "go test"}
	if len(pc.VerifyCommandAllowlist) != len(want) {
		t.Fatalf("len = %d, want %d (%v)", len(pc.VerifyCommandAllowlist), len(want), pc.VerifyCommandAllowlist)
	}
	for i, w := range want {
		if pc.VerifyCommandAllowlist[i] != w {
			t.Errorf("[%d] = %q, want %q", i, pc.VerifyCommandAllowlist[i], w)
		}
	}

	out, err := json.Marshal(pc)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got := string(out)
	if !strings.Contains(got, `"verify_command_allowlist"`) {
		t.Errorf("marshaled JSON missing verify_command_allowlist: %s", got)
	}
	if !strings.Contains(got, `["make","go test"]`) {
		t.Errorf("marshaled JSON missing expected values: %s", got)
	}
}

// TestProjectConfig_VerifyCommandAllowlist_OmitEmpty 驗證未設定時因 omitempty 不出現於序列化。
func TestProjectConfig_VerifyCommandAllowlist_OmitEmpty(t *testing.T) {
	out, err := json.Marshal(ProjectConfig{Name: "demo"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(out), "verify_command_allowlist") {
		t.Errorf("empty allowlist should be omitted, got %s", out)
	}
}
