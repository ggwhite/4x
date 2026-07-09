package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// F157 post-merge 缺陷 5：resolvePathFeatureID 在有 >1 個 active feature 時無法唯一決定
// feature id，過去會整體 fail-open（gate 形同關閉）。驗證：當 FOURX_FEATURE_ID 明確指定其中
// 一個 feature 時，即使有多個 active feature 並存，gate 仍應正常生效（不因 active 數量退化）。
func TestCheckPath_MultipleActiveFeatures_FOURXFeatureIDDisambiguates(t *testing.T) {
	root := t.TempDir()
	featuresDir := filepath.Join(root, ".4x", "features")
	if err := os.MkdirAll(featuresDir, 0o755); err != nil {
		t.Fatalf("mkdir features: %v", err)
	}
	cfg := `{"project":{"name":"t"},"isolation":"worktree","commit":"per-round"}`
	if err := os.WriteFile(filepath.Join(root, ".4x", "settings.json"), []byte(cfg), 0o644); err != nil {
		t.Fatalf("write settings: %v", err)
	}

	// 兩個 feature 同時 active=true（模擬多 session 並行跑不同 feature）。
	ids := []string{"F900-multi-a", "F900-multi-b"}
	roles := map[string]string{"F900-multi-a": "reviewer", "F900-multi-b": "coder"}
	for _, id := range ids {
		runDir := filepath.Join(root, ".4x", "run", id)
		if err := os.MkdirAll(runDir, 0o755); err != nil {
			t.Fatalf("mkdir run: %v", err)
		}
		if err := os.WriteFile(filepath.Join(featuresDir, id+".yaml"), []byte("id: "+id+"\nname: t\n"), 0o644); err != nil {
			t.Fatalf("write feature: %v", err)
		}
		stateJSON := `{"featureId":"` + id + `","phase":"reviewing","role":"` + roles[id] + `","active":true}`
		if err := os.WriteFile(filepath.Join(runDir, "state.json"), []byte(stateJSON), 0o644); err != nil {
			t.Fatalf("write state: %v", err)
		}
	}

	// 未給 FOURX_FEATURE_ID：>1 個 active feature 無法唯一決定 → fail-open（既有行為，維持）。
	t.Run("no FOURX_FEATURE_ID falls back to fail-open", func(t *testing.T) {
		_, _, code := run4xIO(t, root, nil, "", "check", "--path", "cmd/4x/foo.go")
		if code != 0 {
			t.Errorf("without FOURX_FEATURE_ID, ambiguous active features should fail-open, got exit %d", code)
		}
	})

	// FOURX_FEATURE_ID=F900-multi-a（role=reviewer）明確指定 → gate 仍應對 reviewer 寫 source 生效。
	t.Run("FOURX_FEATURE_ID set still denies role violation", func(t *testing.T) {
		env := []string{"FOURX_FEATURE_ID=F900-multi-a"}
		_, stderr, code := run4xIO(t, root, env, "", "check", "--path", "cmd/4x/foo.go")
		if code != 1 {
			t.Fatalf("want exit 1 (reviewer cannot write source), got %d (stderr=%s)", code, stderr)
		}
		if !strings.Contains(stderr, "reviewer") {
			t.Errorf("stderr should mention reviewer role, got: %s", stderr)
		}
	})

	// FOURX_FEATURE_ID=F900-multi-b（role=coder）明確指定 → coder 寫 source 應放行。
	t.Run("FOURX_FEATURE_ID set allows in-scope role", func(t *testing.T) {
		env := []string{"FOURX_FEATURE_ID=F900-multi-b"}
		_, _, code := run4xIO(t, root, env, "", "check", "--path", "cmd/4x/foo.go")
		if code != 0 {
			t.Errorf("coder writing source should allow, got exit %d", code)
		}
	})
}
