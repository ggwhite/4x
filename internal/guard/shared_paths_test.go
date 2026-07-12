package guard

import (
	"testing"

	feat "github.com/ggwhite/4x/internal/feature"
)

// TestCheckSharedPaths_Valid_Pass 確認合法的根層 shared_paths 不被判為 violation。
func TestCheckSharedPaths_Valid_Pass(t *testing.T) {
	ws := setupDesignerYAMLWorkspace(t)
	f := feat.Feature{
		ID: "F001-test", Name: "Test", Description: "desc",
		Status:      feat.StatusInProgress,
		SharedPaths: []string{"Dockerfile", "docker-compose.yml", "dev.sh"},
	}
	ws.SaveFeature(f)

	r := CheckResult{Pass: true}
	checkSharedPaths(ws, "F001-test", &r)
	if !r.Pass {
		t.Errorf("expected pass for valid root-level shared_paths, got errors: %v", r.Errors)
	}
}

// TestCheckSharedPaths_Invalid_Fail 是 round-1 review HIGH 的對抗回歸測試：宣告會逸出
// workspace 的 shared_paths（絕對路徑、'..' traversal、nested、空值）必須被 gate 攔下，
// 避免非法路徑被灌進 Coder prompt 明示「允許改動」。
func TestCheckSharedPaths_Invalid_Fail(t *testing.T) {
	cases := map[string][]string{
		"absolute":  {"/etc/passwd"},
		"traversal": {"../other-worktree/secret"},
		"nested":    {"deploy/Dockerfile"},
		"empty":     {""},
		"mixed":     {"Dockerfile", "../escape"},
	}
	for name, paths := range cases {
		t.Run(name, func(t *testing.T) {
			ws := setupDesignerYAMLWorkspace(t)
			f := feat.Feature{
				ID: "F001-test", Name: "Test", Description: "desc",
				Status: feat.StatusInProgress, SharedPaths: paths,
			}
			ws.SaveFeature(f)

			r := CheckResult{Pass: true}
			checkSharedPaths(ws, "F001-test", &r)
			if r.Pass {
				t.Errorf("expected fail for invalid shared_paths %v", paths)
			}
			if r.RetryableErrors != 1 {
				t.Errorf("expected 1 retryable error, got %d", r.RetryableErrors)
			}
		})
	}
}
