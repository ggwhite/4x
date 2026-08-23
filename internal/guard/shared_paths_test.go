package guard

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	feat "github.com/ggwhite/4x/internal/feature"
	"github.com/ggwhite/4x/internal/gitops"
	"github.com/ggwhite/4x/internal/protocol"
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

// prepSharedPathsWorkspace 準備 checkSharedPathsPollution 需要的最小環境：
// coding phase 的 workspace（state.json + task-brief + criteria 齊全，否則 checkRequiredFiles
// 會先把 Pass 拉掉）、可讓 gitops.MainRootFor 解析出主工作區的 .worktrees/4x/<id> 目錄、
// 一個根層 shared_path 檔案，以及依 multiRepo 決定的 workspace.repos。
// 刻意不做 git init：本檢查依設計不呼叫任何 git 指令，多一個 repo 只會讓 checkScope
// 的 local git diff fallback 進場、干擾 Pass 斷言。
func prepSharedPathsWorkspace(t *testing.T, featureID string, sharedPaths []string, multiRepo bool) *protocol.Workspace {
	t.Helper()
	ws := prepCodingWorkspace(t, featureID)

	if multiRepo {
		cfg := protocol.Config{
			Project: protocol.ProjectConfig{Name: "guard-test"},
			Workspace: protocol.WorkspaceConfig{
				Repos: map[string]protocol.RepoConfig{"core": {Path: "core"}},
			},
		}
		if err := protocol.WriteConfig(ws.DotDir(), cfg); err != nil {
			t.Fatalf("WriteConfig: %v", err)
		}
	}
	if err := os.MkdirAll(gitops.Dir(ws.Root, featureID), 0o755); err != nil {
		t.Fatalf("mkdir worktree: %v", err)
	}
	writeFile(t, filepath.Join(ws.Root, "docker-compose.yml"), "services:\n  app:\n    image: base\n")
	if err := ws.SaveFeature(feat.Feature{
		ID: featureID, Name: featureID, Description: "desc",
		Status: feat.StatusInProgress, SharedPaths: sharedPaths,
	}); err != nil {
		t.Fatalf("SaveFeature: %v", err)
	}
	return ws
}

// writeSharedPathsBaseline 直接寫一份基線檔（值可為刻意不符的哨兵，用來製造 drift）。
func writeSharedPathsBaseline(t *testing.T, root, featureID string, m map[string]string) {
	t.Helper()
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal baseline: %v", err)
	}
	writeFile(t, gitops.SharedPathsBaselineFile(root, featureID), string(data))
}

// TestCheck_SharedPathsPollution 驗證反向污染 guard：主工作區的 shared_path 在 run 期間被改動
// （相對快照基線）時擋下，並在基線缺席時先建出基線而非靜默 fail-open。
func TestCheck_SharedPathsPollution(t *testing.T) {
	const composeFile = "docker-compose.yml"

	t.Run("drift-fails", func(t *testing.T) {
		featureID := "feat-sp-drift"
		ws := prepSharedPathsWorkspace(t, featureID, []string{composeFile}, true)
		writeSharedPathsBaseline(t, ws.Root, featureID, map[string]string{composeFile: "sha256:sentinel"})

		r := Check(ws, featureID, nil)
		if r.Pass {
			t.Fatalf("drift must fail the check, errors: %v", r.Errors)
		}
		if !hasError(r.Errors, "shared_paths modified in main workspace during run") {
			t.Fatalf("errors = %v, want drift error", r.Errors)
		}
		if !hasError(r.Errors, "first merge those main-workspace changes into the worktree copy") {
			t.Errorf("error must tell the user to merge into the worktree copy first: %v", r.Errors)
		}
		if !hasError(r.Errors, "re-baseline") {
			t.Errorf("error must mention re-baseline: %v", r.Errors)
		}
		if hasError(r.Errors, "revert") {
			t.Errorf("error must not suggest reverting the main workspace: %v", r.Errors)
		}

		// 單獨呼叫確認不累加 RetryableErrors：重跑同一 role 修不了這種錯。
		only := CheckResult{Pass: true}
		checkSharedPathsPollution(ws, featureID, &only)
		if only.RetryableErrors != 0 {
			t.Errorf("RetryableErrors = %d, want 0", only.RetryableErrors)
		}
	})

	t.Run("clean-passes", func(t *testing.T) {
		featureID := "feat-sp-clean"
		ws := prepSharedPathsWorkspace(t, featureID, []string{composeFile}, true)
		if err := gitops.UpsertSharedPathsBaseline(ws.Root, featureID, []string{composeFile}); err != nil {
			t.Fatalf("Upsert: %v", err)
		}

		if r := Check(ws, featureID, nil); !r.Pass {
			t.Errorf("unchanged shared_path must pass, errors: %v", r.Errors)
		}
	})

	t.Run("untracked-baseline-not-triggered", func(t *testing.T) {
		featureID := "feat-sp-untracked"
		ws := prepSharedPathsWorkspace(t, featureID, []string{".env"}, true)
		writeFile(t, filepath.Join(ws.Root, ".env"), "TOKEN=abc\n")
		if err := gitops.UpsertSharedPathsBaseline(ws.Root, featureID, []string{".env"}); err != nil {
			t.Fatalf("Upsert: %v", err)
		}

		if r := Check(ws, featureID, nil); !r.Pass {
			t.Errorf("never-tracked but unchanged shared_path must pass, errors: %v", r.Errors)
		}
	})

	t.Run("no-baseline-upserts-then-clean", func(t *testing.T) {
		featureID := "feat-sp-nobaseline"
		ws := prepSharedPathsWorkspace(t, featureID, []string{composeFile}, true)
		baselineFile := gitops.SharedPathsBaselineFile(ws.Root, featureID)
		if _, err := os.Stat(baselineFile); !os.IsNotExist(err) {
			t.Fatalf("baseline must not exist yet: %v", err)
		}

		if r := Check(ws, featureID, nil); !r.Pass {
			t.Fatalf("first check must pass while establishing the baseline, errors: %v", r.Errors)
		}
		data, err := os.ReadFile(baselineFile)
		if err != nil {
			t.Fatalf("baseline must be created by the check: %v", err)
		}
		var baseline map[string]string
		if err := json.Unmarshal(data, &baseline); err != nil {
			t.Fatalf("unmarshal baseline: %v", err)
		}
		if _, ok := baseline[composeFile]; !ok {
			t.Errorf("baseline missing key %q: %v", composeFile, baseline)
		}
	})

	t.Run("monorepo-not-triggered", func(t *testing.T) {
		featureID := "feat-sp-mono"
		ws := prepSharedPathsWorkspace(t, featureID, []string{composeFile}, false)
		writeSharedPathsBaseline(t, ws.Root, featureID, map[string]string{composeFile: "sha256:sentinel"})

		if r := Check(ws, featureID, nil); !r.Pass {
			t.Errorf("monorepo must not trigger the pollution check, errors: %v", r.Errors)
		}
	})

	t.Run("no-declaration-not-triggered", func(t *testing.T) {
		featureID := "feat-sp-nodecl"
		ws := prepSharedPathsWorkspace(t, featureID, nil, true)
		writeSharedPathsBaseline(t, ws.Root, featureID, map[string]string{composeFile: "sha256:sentinel"})

		if r := Check(ws, featureID, nil); !r.Pass {
			t.Errorf("feature without shared_paths must not trigger the check, errors: %v", r.Errors)
		}
	})
}

// TestCheck_SharedPathsBaselineUpsertThenDetectsDrift 鎖住基線的建立時點：SetupWorktree 執行時
// Designer 尚未宣告，基線必須由「首次觀測到宣告」的那次 4x check 建出，之後才擋得住 drift。
// 少了這一步，需求 1 與需求 4 全程 fail-open。
func TestCheck_SharedPathsBaselineUpsertThenDetectsDrift(t *testing.T) {
	const composeFile = "docker-compose.yml"
	featureID := "feat-sp-upsert-drift"

	// 階段一：尚未宣告 → 基線不存在。
	ws := prepSharedPathsWorkspace(t, featureID, nil, true)
	baselineFile := gitops.SharedPathsBaselineFile(ws.Root, featureID)
	if _, err := os.Stat(baselineFile); !os.IsNotExist(err) {
		t.Fatalf("baseline must not exist before any declaration: %v", err)
	}

	// 階段二：Designer 寫下宣告 → 第一次 check 建出基線且通過。
	if err := ws.SaveFeature(feat.Feature{
		ID: featureID, Name: featureID, Description: "desc",
		Status: feat.StatusInProgress, SharedPaths: []string{composeFile},
	}); err != nil {
		t.Fatalf("SaveFeature: %v", err)
	}
	if r := Check(ws, featureID, nil); !r.Pass {
		t.Fatalf("first check after the declaration must pass, errors: %v", r.Errors)
	}
	data, err := os.ReadFile(baselineFile)
	if err != nil {
		t.Fatalf("baseline must be created by the first check: %v", err)
	}
	var baseline map[string]string
	if err := json.Unmarshal(data, &baseline); err != nil {
		t.Fatalf("unmarshal baseline: %v", err)
	}
	if _, ok := baseline[composeFile]; !ok {
		t.Fatalf("baseline missing key %q: %v", composeFile, baseline)
	}

	// 階段三：有人改了主工作區的同一個檔 → 第二次 check 判 drift。
	writeFile(t, filepath.Join(ws.Root, composeFile), "services:\n  app:\n    image: someone-else\n")
	r := Check(ws, featureID, nil)
	if r.Pass {
		t.Fatal("main-workspace drift must fail the check")
	}
	if !hasError(r.Errors, "shared_paths modified in main workspace during run") {
		t.Errorf("errors = %v, want drift error", r.Errors)
	}
}

// TestCheck_SharedPathsRejectsDirectoryInRoot 驗證主 gate：目錄型與 go.work 型宣告在 4x check
// 就被攔下（而不是等到下一次 git worktree add 前才炸，白費一輪 designing），且屬可重試錯誤。
func TestCheck_SharedPathsRejectsDirectoryInRoot(t *testing.T) {
	cases := []struct {
		name      string
		featureID string
		declare   string
		wantErr   string
		prep      func(t *testing.T, root string)
	}{
		{"directory", "feat-sp-gate-dir", "deployments", "is a directory in the workspace root", func(t *testing.T, root string) {
			if err := os.MkdirAll(filepath.Join(root, "deployments"), 0o755); err != nil {
				t.Fatalf("mkdir: %v", err)
			}
		}},
		{"go.work", "feat-sp-gate-gowork", "go.work", "managed by copyGoWork", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ws := prepSharedPathsWorkspace(t, tc.featureID, []string{tc.declare}, false)
			if tc.prep != nil {
				tc.prep(t, ws.Root)
			}

			r := Check(ws, tc.featureID, nil)
			if r.Pass {
				t.Fatalf("declaring %q must fail the check", tc.declare)
			}
			if !hasError(r.Errors, tc.wantErr) {
				t.Fatalf("errors = %v, want %q", r.Errors, tc.wantErr)
			}

			// 單獨呼叫確認累加 RetryableErrors：Designer 改掉宣告值就能修。
			only := CheckResult{Pass: true}
			checkSharedPaths(ws, tc.featureID, &only)
			if only.RetryableErrors != 1 {
				t.Errorf("RetryableErrors = %d, want 1", only.RetryableErrors)
			}
		})
	}

	t.Run("valid-file-passes", func(t *testing.T) {
		featureID := "feat-sp-gate-ok"
		ws := prepSharedPathsWorkspace(t, featureID, []string{"docker-compose.yml"}, false)
		if r := Check(ws, featureID, nil); !r.Pass {
			t.Errorf("valid root-level file must pass, errors: %v", r.Errors)
		}
	})
}
