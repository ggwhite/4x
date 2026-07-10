package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// runGitAt 在 dir 執行 git 子命令，失敗時 t.Fatalf。供本檔的 worktree fixture 使用。
func runGitAt(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=tester", "GIT_AUTHOR_EMAIL=test@test.com",
		"GIT_COMMITTER_NAME=tester", "GIT_COMMITTER_EMAIL=test@test.com",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v in %s: %s", args, dir, out)
	}
}

// writeFileAt 寫檔並自動建立父目錄。
func writeFileAt(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// write4xConfig 在 root 內寫入最小合法 .4x 設定（settings/feature/state），供 protocol.Find 解析。
// state 設 active=true 讓 resolvePathFeatureID 無位置參數也能命中唯一 active feature。
func write4xConfig(t *testing.T, root, id string) {
	t.Helper()
	cfg := `{"project":{"name":"t"},"isolation":"worktree","commit":"per-round"}`
	writeFileAt(t, filepath.Join(root, ".4x", "settings.json"), cfg)
	writeFileAt(t, filepath.Join(root, ".4x", "features", id+".yaml"), "id: "+id+"\nname: t\n")
	writeFileAt(t, filepath.Join(root, ".4x", "run", id, "state.json"),
		`{"featureId":"`+id+`","phase":"coding","role":"coder","active":true}`)
}

// mainWorkspaceFixture 建立真實 git 主工作區與其 linked worktree。
// 回傳 mainRoot（主工作區 root）與 wtPath（linked worktree 路徑，供子程序 cwd）。
// 主工作區內含已提交的 main.go 作為越界寫入目標；.4x 設定直接寫入 worktree 檔案系統
// （.4x/run 被 gitignore，無法靠 checkout 帶入），確保 protocol.Find 能在 worktree 解析。
func mainWorkspaceFixture(t *testing.T, id string) (mainRoot, wtPath string) {
	t.Helper()
	mainRoot = t.TempDir()
	runGitAt(t, mainRoot, "init", "-b", "main")
	runGitAt(t, mainRoot, "config", "user.name", "test")
	runGitAt(t, mainRoot, "config", "user.email", "test@test")
	writeFileAt(t, filepath.Join(mainRoot, "main.go"), "package main\n")
	runGitAt(t, mainRoot, "add", "main.go")
	runGitAt(t, mainRoot, "commit", "-m", "init")

	wtPath = filepath.Join(t.TempDir(), "wt")
	runGitAt(t, mainRoot, "worktree", "add", "-b", "4x/"+id, wtPath)
	write4xConfig(t, wtPath, id)
	return mainRoot, wtPath
}

// noGitPathEnv 回傳一組把 PATH 指向空目錄的環境變數，讓子程序內的 `git` 無法被 exec.LookPath
// 找到，模擬「git 不在 PATH / rev-parse subprocess 失敗」情境（4x 二進位以絕對路徑啟動不受影響）。
func noGitPathEnv(t *testing.T) []string {
	t.Helper()
	return []string{"PATH=" + t.TempDir()}
}

// AC-2：linked worktree 內 `4x check --path <main-root>/main.go` → exit 1，
// stderr 含穩定文字 "main workspace" 與 "outside current worktree"。
func TestCheckPath_MainWorkspaceDeny(t *testing.T) {
	id := "F900-mwdeny"
	mainRoot, wtPath := mainWorkspaceFixture(t, id)
	target := filepath.Join(mainRoot, "main.go")

	_, stderr, code := run4xIO(t, wtPath, nil, "", "check", "--path", target)
	if code != 1 {
		t.Fatalf("want exit 1 deny, got %d (stderr=%s)", code, stderr)
	}
	if !strings.Contains(stderr, "main workspace") || !strings.Contains(stderr, "outside current worktree") {
		t.Errorf("stderr should contain main-workspace deny reason, got: %s", stderr)
	}
}

// AC-4：check --path 在 workspace 外路徑的 fail-open——非 git repo 與 git 不可執行兩情境，
// 即使目標落在主工作區 root 內也仍 exit 0 放行。
func TestCheckPath_MainWorkspaceFailOpen(t *testing.T) {
	t.Run("non-git dir fails open", func(t *testing.T) {
		id := "F900-mwfo-nogit"
		// pathFixture 建立 .4x 但非 git repo；目標為 workspace 外的絕對路徑。
		root := pathFixture(t, id, "id: "+id+"\nname: t\n",
			`{"featureId":"`+id+`","phase":"coding","role":"coder","active":true}`)
		outside := filepath.Join(t.TempDir(), "other.go")
		_, stderr, code := run4xIO(t, root, nil, "", "check", "--path", outside)
		if code != 0 {
			t.Fatalf("non-git workspace-outside path should fail open (exit 0), got %d (stderr=%s)", code, stderr)
		}
	})

	t.Run("git unavailable fails open even inside main root", func(t *testing.T) {
		id := "F900-mwfo-nopath"
		mainRoot, wtPath := mainWorkspaceFixture(t, id)
		target := filepath.Join(mainRoot, "main.go")
		// 目標確實落在主工作區 root 內，但 git 不在 PATH → 無法證明 → 仍放行。
		_, stderr, code := run4xIO(t, wtPath, noGitPathEnv(t), "", "check", "--path", target)
		if code != 0 {
			t.Fatalf("git-unavailable should fail open (exit 0), got %d (stderr=%s)", code, stderr)
		}
	})
}

// AC-3：linked worktree 內 guard-tool 收到指向主工作區 root 內檔案的 Edit/Write file_path →
// 命令 exit 0，stdout JSON 含 permissionDecision":"deny"，reason 含 "main workspace" 與 "outside current worktree"。
func TestGuardTool_MainWorkspaceDeny(t *testing.T) {
	id := "F900-gtmw"
	mainRoot, wtPath := mainWorkspaceFixture(t, id)
	target := filepath.Join(mainRoot, "main.go")

	for _, tool := range []string{"Edit", "Write", "MultiEdit"} {
		t.Run(tool, func(t *testing.T) {
			stdin := `{"tool_name":"` + tool + `","tool_input":{"file_path":"` + target + `"}}`
			stdout, _, code := run4xIO(t, wtPath, nil, stdin, "guard-tool")
			if code != 0 {
				t.Fatalf("guard-tool should always exit 0, got %d", code)
			}
			if !strings.Contains(stdout, `"permissionDecision":"deny"`) {
				t.Errorf("expected deny decision in stdout, got: %s", stdout)
			}
			if !strings.Contains(stdout, "main workspace") || !strings.Contains(stdout, "outside current worktree") {
				t.Errorf("deny reason should mention main workspace boundary, got: %s", stdout)
			}
		})
	}
}

// AC-4：guard-tool 在 workspace 外路徑的 fail-open——非 git repo 與 git 不可執行兩情境都不輸出 deny。
func TestGuardTool_MainWorkspaceFailOpen(t *testing.T) {
	t.Run("non-git dir no deny", func(t *testing.T) {
		id := "F900-gtfo-nogit"
		root := pathFixture(t, id, "id: "+id+"\nname: t\n",
			`{"featureId":"`+id+`","phase":"coding","role":"coder","active":true}`)
		outside := filepath.Join(t.TempDir(), "other.go")
		stdin := `{"tool_name":"Edit","tool_input":{"file_path":"` + outside + `"}}`
		stdout, _, code := run4xIO(t, root, nil, stdin, "guard-tool")
		if code != 0 {
			t.Fatalf("guard-tool should exit 0, got %d", code)
		}
		if strings.Contains(stdout, `"permissionDecision":"deny"`) {
			t.Errorf("non-git workspace-outside path should not deny, got: %s", stdout)
		}
	})

	t.Run("git unavailable no deny even inside main root", func(t *testing.T) {
		id := "F900-gtfo-nopath"
		mainRoot, wtPath := mainWorkspaceFixture(t, id)
		target := filepath.Join(mainRoot, "main.go")
		stdin := `{"tool_name":"Edit","tool_input":{"file_path":"` + target + `"}}`
		stdout, _, code := run4xIO(t, wtPath, noGitPathEnv(t), stdin, "guard-tool")
		if code != 0 {
			t.Fatalf("guard-tool should exit 0, got %d", code)
		}
		if strings.Contains(stdout, `"permissionDecision":"deny"`) {
			t.Errorf("git-unavailable should not deny, got: %s", stdout)
		}
	})
}
