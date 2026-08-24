package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ggwhite/4x/internal/gitops"
	"github.com/ggwhite/4x/internal/guard"
	"github.com/ggwhite/4x/internal/pathutil"
	"github.com/ggwhite/4x/internal/protocol"
	"github.com/spf13/cobra"
)

func newCheckCmd() *cobra.Command {
	var jsonOutput bool
	var pathMode string

	cmd := &cobra.Command{
		Use:   "check <feature-id>",
		Short: "Run guardrail checks (scope, baseline, required files, verify evidence)",
		Args: func(cmd *cobra.Command, args []string) error {
			// --path 模式下 feature-id 選填（可由 env / active feature 解析）；否則沿用既有必填語意。
			if pathMode != "" {
				return cobra.MaximumNArgs(1)(cmd, args)
			}
			return cobra.ExactArgs(1)(cmd, args)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if pathMode != "" {
				return runCheckPath(pathMode, args)
			}

			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			ws, err := protocol.Find(cwd)
			if err != nil {
				return err
			}

			featureID, err := ws.ResolveFeatureID(args[0])
			if err != nil {
				return err
			}

			cfg, _ := ws.LoadMergedConfig()
			ops := gitops.New(ws.Root, ws, cfg)

			result := guard.Check(ws, featureID, ops)

			if jsonOutput {
				data, _ := json.MarshalIndent(result, "", "  ")
				fmt.Println(string(data))
			} else {
				if result.Pass {
					fmt.Println("✅ All checks passed")
				} else {
					fmt.Println("❌ Check failed")
				}
				for _, e := range result.Errors {
					fmt.Printf("  ERROR: %s\n", e)
				}
				for _, w := range result.Warns {
					fmt.Printf("  WARN:  %s\n", w)
				}
			}

			if !result.Pass {
				os.Exit(1)
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output as JSON")
	cmd.Flags().StringVar(&pathMode, "path", "", "single-file write-permission check for the current role (fast, read-only)")
	return cmd
}

// runCheckPath 執行 `4x check --path` 的快速單檔寫入判定，完全獨立於 guard.Check：
// 毫秒級、唯讀、不寫任何檔（不 WriteState / 不寫 events / 不 heartbeat）。
//
// fail-open 邊界：讀取 4x 專案 / state / config / feature 或 git 偵測失敗一律 os.Exit(0) 放行；
// 唯有明確判定 deny 時（role-scope 越界，或目標在目前 worktree root 外但可證明落在主工作區
// root 內）才 os.Exit(1) 並印 reason 至 stderr。
func runCheckPath(pathVal string, args []string) error {
	cwd, err := os.Getwd()
	if err != nil {
		os.Exit(0)
	}
	ws, err := protocol.Find(cwd)
	if err != nil {
		os.Exit(0) // 非 4x 專案 → 放行
	}

	featureID, ok := resolvePathFeatureID(ws, args)
	if !ok {
		os.Exit(0) // 無法解析出唯一 feature → 放行
	}

	if _, err := ws.ReadState(featureID); err != nil {
		os.Exit(0) // 無 state.json（非 run 情境）→ 放行
	}

	relPath, ok := relWithinWorkspace(ws.Root, cwd, pathVal)
	if !ok {
		// 路徑落在目前 worktree root 外：不再無條件放行。若可由 git 證明目標落在
		// linked worktree 對應的主工作區 root 內，即時 deny，避免 role agent 誤寫主
		// 工作區同名 repo；git 偵測失敗或目標不在主工作區 root 內時仍 fail-open 放行。
		if deny, reason := mainWorkspaceDeny(cwd, pathVal); deny {
			fmt.Fprintln(os.Stderr, reason)
			os.Exit(1)
		}
		os.Exit(0)
	}

	deny, reason := guard.EvaluateWritePathForRun(ws, featureID, relPath)
	if deny {
		fmt.Fprintln(os.Stderr, reason)
		os.Exit(1)
	}
	os.Exit(0)
	return nil
}

// mainWorkspaceDeny 判定「已由 relWithinWorkspace 確認落在目前 worktree root 外」的目標路徑，
// 是否可證明落在 linked worktree 對應的主工作區 root 內。runCheckPath 與 evaluateWriteHook 共用
// 此 helper，避免兩處對 workspace 外路徑的 fail-open/fail-closed 判定漂移。
//
// AC-6 效能約束：本 helper 只在 relWithinWorkspace 判定目標不在目前 workspace root 內後才被呼叫，
// 故一般 worktree 內寫入完全不觸發 git subprocess。
//
// fail-open 邊界：gitops.MainWorkspaceRoot 回 ok=false（非 git repo、git 不在 PATH、
// git rev-parse --git-common-dir 子程序失敗、或非 linked worktree），或目標不在主工作區 root 內時，
// 回傳 (false, "") 維持放行；唯有目標可證明落在主工作區 root 內時回傳 (true, reason)，reason 穩定
// 包含 "main workspace" 與 "outside current worktree" 供上層匹配。
func mainWorkspaceDeny(cwd, rawPath string) (bool, string) {
	mainRoot, ok := gitops.MainWorkspaceRoot(cwd)
	if !ok {
		return false, ""
	}
	abs := rawPath
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(cwd, rawPath)
	}
	if !pathWithinRoot(mainRoot, abs) {
		return false, ""
	}
	reason := fmt.Sprintf("refusing write to %q: inside main workspace root %q but outside current worktree — a 4x role agent may only write within its own worktree, not the main workspace's same-named repo",
		filepath.Clean(abs), mainRoot)
	return true, reason
}

// pathWithinRoot 判定 target 是否落在 root 內部（不含 root 自身）。
// 兩邊先以 realpathBestEffort 解析 symlink，消除 macOS /var → /private/var 等字面差異，
// 避免因 root（已 EvalSymlinks）與 target（可能是 raw 絕對路徑）表面不同而漏判。
func pathWithinRoot(root, target string) bool {
	root = pathutil.RealpathBestEffort(root)
	target = pathutil.RealpathBestEffort(target)
	if root == target {
		return false // target == root 本身，不是內部檔案
	}
	return pathutil.WithinRoot(root, target)
}

// resolvePathFeatureID 依序解析目標 feature-id：位置參數 → FOURX_FEATURE_ID env →
// 唯一 active feature。任一步驟無法解析出有效 feature 回傳 ("", false)。
// 供 `4x check --path` 與 `4x guard-tool` 的 Edit/Write 分支共用。
func resolvePathFeatureID(ws *protocol.Workspace, args []string) (string, bool) {
	if len(args) == 1 && args[0] != "" {
		if id, err := ws.ResolveFeatureID(args[0]); err == nil {
			return id, true
		}
		return "", false
	}
	if env := os.Getenv("FOURX_FEATURE_ID"); env != "" {
		if id, err := ws.ResolveFeatureID(env); err == nil {
			return id, true
		}
		return "", false
	}
	return resolveActiveFeatureID(ws)
}

// resolveActiveFeatureID 回傳唯一 State.Active==true 的 feature-id；0 個或多於 1 個 → ("", false)。
func resolveActiveFeatureID(ws *protocol.Workspace) (string, bool) {
	features, err := ws.ListFeatures()
	if err != nil {
		return "", false
	}
	found := ""
	count := 0
	for _, f := range features {
		st, err := ws.ReadState(f.ID)
		if err != nil {
			continue
		}
		if st.Active {
			found = f.ID
			count++
		}
	}
	if count == 1 {
		return found, true
	}
	return "", false
}

// relWithinWorkspace 把 raw 路徑（絕對或相對 cwd）正規化為相對 root 的乾淨路徑（"/" 分隔）。
// 若正規化後落在 root 之外，回傳 ("", false)。
func relWithinWorkspace(root, cwd, raw string) (string, bool) {
	abs := raw
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(cwd, raw)
	}
	abs = filepath.Clean(abs)
	rel, err := filepath.Rel(root, abs)
	if err != nil {
		return "", false
	}
	rel = filepath.ToSlash(rel)
	if rel == ".." || strings.HasPrefix(rel, "../") {
		return "", false
	}
	return rel, true
}
