package gitops

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ggwhite/4x/internal/feature"
	"github.com/ggwhite/4x/internal/protocol"
	"github.com/ggwhite/4x/internal/vcshub"
)

const composeFile = "docker-compose.yml"

// initRootRepo 對 workspace root 建 git repo，但刻意不做 `git add .`：
// setupMultiWorkspace 已在 root 下建出 core/ 與 gate/ 兩個子 git repo，
// 全量 add 會把它們當 gitlink 收進 index，污染後續的 name-only 斷言。
func initRootRepo(t *testing.T, root string) {
	t.Helper()
	runGit(t, root, "init")
	runGit(t, root, "config", "user.name", "test")
	runGit(t, root, "config", "user.email", "test@test")
}

// setupSharedPathsWorkspace 在 setupMultiWorkspace 之上補出 shared_paths 測試需要的環境：
// root 本身是 git repo 且追蹤 docker-compose.yml，feature YAML 依 sharedPaths 宣告。
// sharedPaths 傳 nil 即「未宣告」的零回歸情境。
func setupSharedPathsWorkspace(t *testing.T, featureID string, sharedPaths []string) (root string, ws *protocol.Workspace, ops Ops) {
	t.Helper()
	root, ws, ops = setupMultiWorkspace(t)

	if err := os.WriteFile(filepath.Join(root, composeFile), []byte("services:\n  app:\n    image: base\n"), 0o644); err != nil {
		t.Fatalf("write %s: %v", composeFile, err)
	}
	initRootRepo(t, root)
	runGit(t, root, "add", "--", composeFile)
	runGit(t, root, "commit", "-m", "add compose")

	saveSharedPathsFeature(t, ws, featureID, sharedPaths)
	return root, ws, ops
}

// saveSharedPathsFeature 寫一份宣告 sharedPaths 的 feature YAML。
func saveSharedPathsFeature(t *testing.T, ws *protocol.Workspace, featureID string, sharedPaths []string) {
	t.Helper()
	f := feature.Feature{
		ID:          featureID,
		Name:        "Shared Paths Feature",
		Description: "merge-back shared paths",
		Status:      feature.StatusInProgress,
		SharedPaths: sharedPaths,
	}
	if err := ws.SaveFeature(f); err != nil {
		t.Fatalf("SaveFeature: %v", err)
	}
}

func readFileString(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func containsSub(list []string, substr string) bool {
	for _, s := range list {
		if strings.Contains(s, substr) {
			return true
		}
	}
	return false
}

// TestMultiRepo_SharedPaths_CopiedIntoWorktree 確認宣告的根層 shared_path 被複製進 worktree，
// Coder 才有檔案可改（copyWorkspaceFiles 的通用迴圈本來就涵蓋根層檔案）。
func TestMultiRepo_SharedPaths_CopiedIntoWorktree(t *testing.T) {
	featureID := "feat-sp-copy"
	root, _, ops := setupSharedPathsWorkspace(t, featureID, []string{composeFile})

	wtPath, err := ops.SetupWorktree(featureID, nil)
	if err != nil {
		t.Fatalf("SetupWorktree: %v", err)
	}
	if got, want := readFileString(t, filepath.Join(wtPath, composeFile)), readFileString(t, filepath.Join(root, composeFile)); got != want {
		t.Errorf("worktree %s = %q, want %q", composeFile, got, want)
	}
}

// TestMultiRepo_SharedPaths_BaselineWrittenWithoutPreexistingRunDir 鎖住真實執行順序：
// cmd/4x/run.go 的 setupWorktree 早於 ws.InitFeatureDir，.4x/run/<id>/ 此時尚不存在。
// UpsertSharedPathsBaseline 少了 MkdirAll 會 ENOENT，drift 偵測整段降級成 no-op。
func TestMultiRepo_SharedPaths_BaselineWrittenWithoutPreexistingRunDir(t *testing.T) {
	featureID := "feat-sp-baseline-mkdir"
	root, _, ops := setupSharedPathsWorkspace(t, featureID, []string{composeFile})

	runDir := filepath.Join(root, protocol.DirName, protocol.RunDir, featureID)
	if _, err := os.Stat(runDir); !os.IsNotExist(err) {
		t.Fatalf("run dir must not exist before SetupWorktree: %v", err)
	}

	if _, err := ops.SetupWorktree(featureID, nil); err != nil {
		t.Fatalf("SetupWorktree: %v", err)
	}

	data, err := os.ReadFile(SharedPathsBaselineFile(root, featureID))
	if err != nil {
		t.Fatalf("baseline should exist after SetupWorktree: %v", err)
	}
	var baseline map[string]string
	if err := json.Unmarshal(data, &baseline); err != nil {
		t.Fatalf("unmarshal baseline: %v", err)
	}
	if _, ok := baseline[composeFile]; !ok {
		t.Errorf("baseline missing key %q: %v", composeFile, baseline)
	}
}

// TestMultiRepo_SharedPaths_RejectsDirectoryAndGoWork 驗證第二道防線：目錄與 go.work 型宣告
// 讓 SetupWorktree 直接回錯。新建情境不得留下半成品 worktree；resume 情境（worktree 已存在）
// 必須原封保留既有內容——刪掉等於抹除 Coder 未 commit 的工作。
func TestMultiRepo_SharedPaths_RejectsDirectoryAndGoWork(t *testing.T) {
	// featureID 另列一欄：feature ID 只允許 [A-Za-z0-9-]，不能直接拿子測試名稱（含 "."）當 ID，
	// 否則 LoadFeature 會先失敗、宣告根本讀不到，測試就驗不到攔截邏輯。
	cases := []struct {
		name      string
		featureID string
		declare   string
		prep      func(t *testing.T, root string)
	}{
		{"directory", "feat-sp-reject-dir", "deployments", func(t *testing.T, root string) {
			if err := os.MkdirAll(filepath.Join(root, "deployments"), 0o755); err != nil {
				t.Fatalf("mkdir: %v", err)
			}
		}},
		{"go.work", "feat-sp-reject-gowork", "go.work", nil},
		{"go.work.sum", "feat-sp-reject-goworksum", "go.work.sum", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			featureID := tc.featureID
			root, _, ops := setupSharedPathsWorkspace(t, featureID, []string{tc.declare})
			if tc.prep != nil {
				tc.prep(t, root)
			}

			if _, err := ops.SetupWorktree(featureID, nil); err == nil {
				t.Fatalf("SetupWorktree should reject %q", tc.declare)
			}
			if _, err := os.Stat(Dir(root, featureID)); !os.IsNotExist(err) {
				t.Errorf("no half-built worktree should remain: %v", err)
			}
		})
	}

	t.Run("resume-existing-worktree", func(t *testing.T) {
		featureID := "feat-sp-reject-resume"
		root, ws, ops := setupSharedPathsWorkspace(t, featureID, []string{composeFile})

		wtPath, err := ops.SetupWorktree(featureID, nil)
		if err != nil {
			t.Fatalf("SetupWorktree: %v", err)
		}
		wip := filepath.Join(wtPath, "core", "wip.go")
		writeFile(t, wip, "package core // uncommitted\n")

		// Designer 事後把宣告改成目錄：resume 的 SetupWorktree 必須擋下且不動既有 worktree。
		if err := os.MkdirAll(filepath.Join(root, "deployments"), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		saveSharedPathsFeature(t, ws, featureID, []string{"deployments"})

		if _, err := ops.SetupWorktree(featureID, nil); err == nil {
			t.Fatal("resume SetupWorktree should reject directory shared_path")
		}
		if _, err := os.Stat(wtPath); err != nil {
			t.Fatalf("existing worktree must be preserved: %v", err)
		}
		if got := readFileString(t, wip); got != "package core // uncommitted\n" {
			t.Errorf("uncommitted worktree file changed: %q", got)
		}
	})
}

// TestMultiRepo_Merge_SharedPathsMergedBackAndCommitted 是本 feature 的主路徑：worktree 內
// 改過的根層 shared_path 必須在 Cleanup 之前複製回主工作區並進 root repo 的 commit，
// 且 merge-back 成功後刪除基線檔（否則後續 guard.Check() 會恆判 drift）。
func TestMultiRepo_Merge_SharedPathsMergedBackAndCommitted(t *testing.T) {
	featureID := "feat-sp-mergeback"
	root, _, ops := setupSharedPathsWorkspace(t, featureID, []string{composeFile})

	wtPath, err := ops.SetupWorktree(featureID, nil)
	if err != nil {
		t.Fatalf("SetupWorktree: %v", err)
	}
	baselineFile := SharedPathsBaselineFile(root, featureID)
	if _, err := os.Stat(baselineFile); err != nil {
		t.Fatalf("baseline must exist before Merge: %v", err)
	}

	const coderVersion = "services:\n  app:\n    image: coder-edit\n"
	writeFile(t, filepath.Join(wtPath, composeFile), coderVersion)
	writeFile(t, filepath.Join(wtPath, "core", "feature.go"), "package core\n")
	if err := ops.Commit(wtPath, featureID, "wip"); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	result := ops.Merge(featureID, "Shared Paths Feature")
	if result.Error != "" {
		t.Fatalf("merge error: %s", result.Error)
	}
	if len(result.SharedPathsMerged) != 1 || result.SharedPathsMerged[0] != composeFile {
		t.Fatalf("SharedPathsMerged = %v, want [%s]", result.SharedPathsMerged, composeFile)
	}
	if got := readFileString(t, filepath.Join(root, composeFile)); got != coderVersion {
		t.Errorf("main workspace %s = %q, want %q", composeFile, got, coderVersion)
	}
	names := gitOutput(root, "show", "--name-only", "--format=", "HEAD")
	if !strings.Contains(names, composeFile) {
		t.Errorf("root HEAD commit should contain %s, got %q", composeFile, names)
	}
	if _, err := os.Stat(wtPath); !os.IsNotExist(err) {
		t.Errorf("worktree should be removed after merge: %v", err)
	}
	if _, err := os.Stat(baselineFile); !os.IsNotExist(err) {
		t.Errorf("baseline should be removed after a successful merge-back: %v", err)
	}
}

// TestMultiRepo_Merge_AbortsWhenMainSharedPathDirty 驗證需求 1：主工作區的 shared_path 相對
// 快照基線有 drift 時中止且不觸碰任何 repo；反向不變式是從未被 git 追蹤、內容也沒變的宣告
// （.env 這類）不得誤觸發——用「相對 HEAD」判準會讓它恆為 dirty。
func TestMultiRepo_Merge_AbortsWhenMainSharedPathDirty(t *testing.T) {
	t.Run("tracked-modified", func(t *testing.T) {
		featureID := "feat-sp-dirty"
		root, _, ops := setupSharedPathsWorkspace(t, featureID, []string{composeFile})

		wtPath, err := ops.SetupWorktree(featureID, nil)
		if err != nil {
			t.Fatalf("SetupWorktree: %v", err)
		}
		writeFile(t, filepath.Join(wtPath, composeFile), "services:\n  app:\n    image: coder\n")
		writeFile(t, filepath.Join(root, composeFile), "services:\n  app:\n    image: someone-else\n")

		preHeads := map[string]string{}
		for _, name := range []string{"core", "gate"} {
			preHeads[name] = gitOutput(filepath.Join(root, name), "rev-parse", "HEAD")
		}

		result := ops.Merge(featureID, "Shared Paths Feature")
		if !strings.Contains(result.Error, "shared_paths dirty in main workspace") {
			t.Fatalf("Error = %q, want shared_paths dirty abort", result.Error)
		}
		if !strings.Contains(result.Error, "first merge those main-workspace changes into the worktree copy") {
			t.Errorf("Error must tell the user to merge into the worktree copy first: %q", result.Error)
		}
		if !strings.Contains(result.Error, "re-baseline") {
			t.Errorf("Error must mention re-baseline: %q", result.Error)
		}
		if strings.Contains(result.Error, "revert") {
			t.Errorf("Error must not suggest reverting the main workspace: %q", result.Error)
		}
		for _, name := range []string{"core", "gate"} {
			if got := gitOutput(filepath.Join(root, name), "rev-parse", "HEAD"); got != preHeads[name] {
				t.Errorf("%s HEAD changed despite abort: %s -> %s", name, preHeads[name], got)
			}
		}
		if _, err := os.Stat(wtPath); err != nil {
			t.Errorf("worktree must be preserved on abort: %v", err)
		}
	})

	t.Run("untracked-baseline-not-triggered", func(t *testing.T) {
		featureID := "feat-sp-untracked"
		root, _, ops := setupSharedPathsWorkspace(t, featureID, []string{".env"})
		// .env 在 SetupWorktree 之前就存在於主工作區，且從未 git add。
		writeFile(t, filepath.Join(root, ".env"), "TOKEN=abc\n")

		if _, err := ops.SetupWorktree(featureID, nil); err != nil {
			t.Fatalf("SetupWorktree: %v", err)
		}

		result := ops.Merge(featureID, "Shared Paths Feature")
		if result.Error != "" {
			t.Fatalf("untracked-but-unchanged shared_path must not abort: %q", result.Error)
		}
	})
}

// TestMultiRepo_PushAndOpenMR_SharedPathsMergedBack 驗證 MR 路徑與 Merge 有相同保護：
// 正常與 !anyAhead 兩條路徑都會 Cleanup，故都要 merge-back，且都要明講那筆 commit 不在 MR 裡；
// push 失敗時不 merge-back（worktree 保留供重試）。
func TestMultiRepo_PushAndOpenMR_SharedPathsMergedBack(t *testing.T) {
	prep := func(t *testing.T, featureID string) (root string, ops Ops, wtPath string) {
		t.Helper()
		root, ws, ops := setupSharedPathsWorkspace(t, featureID, []string{composeFile})
		if err := ws.InitFeatureDir(featureID); err != nil {
			t.Fatalf("InitFeatureDir: %v", err)
		}
		if err := ops.CaptureBaseline(featureID, nil); err != nil {
			t.Fatalf("CaptureBaseline: %v", err)
		}
		wtPath, err := ops.SetupWorktree(featureID, nil)
		if err != nil {
			t.Fatalf("SetupWorktree: %v", err)
		}
		return root, ops, wtPath
	}
	useFakeHub := func(t *testing.T) {
		t.Helper()
		orig := vcshubNew
		t.Cleanup(func() { vcshubNew = orig })
		vcshubNew = func(string) vcshub.Hub { return &fakeHub{} }
	}
	const coderVersion = "services:\n  app:\n    image: mr-edit\n"

	t.Run("main-dirty-aborts", func(t *testing.T) {
		featureID := "feat-sp-mr-dirty"
		root, ops, wtPath := prep(t, featureID)
		useFakeHub(t)

		writeFile(t, filepath.Join(wtPath, composeFile), coderVersion)
		writeFile(t, filepath.Join(root, composeFile), "services:\n  app:\n    image: someone-else\n")

		result := ops.PushAndOpenMR(featureID, "Shared Paths Feature")
		if !strings.Contains(result.Error, "shared_paths dirty in main workspace") {
			t.Fatalf("Error = %q, want shared_paths dirty abort", result.Error)
		}
		if len(result.MRUrls) != 0 {
			t.Errorf("no MR should be opened on abort: %v", result.MRUrls)
		}
		if _, err := os.Stat(wtPath); err != nil {
			t.Errorf("worktree must be preserved on abort: %v", err)
		}
	})

	t.Run("normal-path-merges-back", func(t *testing.T) {
		featureID := "feat-sp-mr-normal"
		root, ops, wtPath := prep(t, featureID)
		useFakeHub(t)

		for _, name := range []string{"core", "gate"} {
			writeFile(t, filepath.Join(wtPath, name, "new.go"), "package "+name+"\n")
			addBareRemote(t, filepath.Join(root, name))
		}
		if err := ops.Commit(wtPath, featureID, "wip"); err != nil {
			t.Fatalf("Commit: %v", err)
		}
		writeFile(t, filepath.Join(wtPath, composeFile), coderVersion)

		result := ops.PushAndOpenMR(featureID, "Shared Paths Feature")
		if result.Error != "" {
			t.Fatalf("unexpected error: %s", result.Error)
		}
		if len(result.SharedPathsMerged) == 0 {
			t.Fatal("SharedPathsMerged should be non-empty")
		}
		if !containsSub(result.SharedPathsNotes, "not part of any MR") {
			t.Errorf("notes must warn the commit is not in any MR: %v", result.SharedPathsNotes)
		}
		if got := readFileString(t, filepath.Join(root, composeFile)); got != coderVersion {
			t.Errorf("main workspace %s = %q, want %q", composeFile, got, coderVersion)
		}
	})

	t.Run("no-ahead-merges-back", func(t *testing.T) {
		featureID := "feat-sp-mr-noahead"
		root, ops, wtPath := prep(t, featureID)
		useFakeHub(t)

		// 沒有任何 repo commit → anyAhead == false，但 Cleanup 照樣執行。
		writeFile(t, filepath.Join(wtPath, composeFile), coderVersion)

		result := ops.PushAndOpenMR(featureID, "Shared Paths Feature")
		if result.Error != "" {
			t.Fatalf("unexpected error: %s", result.Error)
		}
		if !result.Skipped {
			t.Fatalf("expected Skipped result, got %+v", result)
		}
		if len(result.SharedPathsMerged) == 0 {
			t.Fatal("SharedPathsMerged should be non-empty on the !anyAhead path")
		}
		if !containsSub(result.SharedPathsNotes, "not part of any MR") {
			t.Errorf("notes must warn the commit is not in any MR: %v", result.SharedPathsNotes)
		}
		if got := readFileString(t, filepath.Join(root, composeFile)); got != coderVersion {
			t.Errorf("main workspace %s = %q, want %q", composeFile, got, coderVersion)
		}
	})

	t.Run("push-failure-no-merge-back", func(t *testing.T) {
		featureID := "feat-sp-mr-pushfail"
		root, ops, wtPath := prep(t, featureID)
		useFakeHub(t)

		before := readFileString(t, filepath.Join(root, composeFile))
		for _, name := range []string{"core", "gate"} {
			writeFile(t, filepath.Join(wtPath, name, "new.go"), "package "+name+"\n")
		}
		if err := ops.Commit(wtPath, featureID, "wip"); err != nil {
			t.Fatalf("Commit: %v", err)
		}
		writeFile(t, filepath.Join(wtPath, composeFile), coderVersion)

		// 沒有任何 remote → push 全數失敗。
		result := ops.PushAndOpenMR(featureID, "Shared Paths Feature")
		if result.Error == "" {
			t.Fatal("expected push failure")
		}
		if len(result.SharedPathsMerged) != 0 {
			t.Errorf("no merge-back on push failure, got %v", result.SharedPathsMerged)
		}
		if got := readFileString(t, filepath.Join(root, composeFile)); got != before {
			t.Errorf("main workspace %s must be untouched, got %q", composeFile, got)
		}
		if _, err := os.Stat(wtPath); err != nil {
			t.Errorf("worktree must be preserved for retry: %v", err)
		}
	})
}

// TestMultiRepo_Merge_SharedPathsExplicitNotes 窮盡「無法納入 commit / 無法傳播 / 偵測被跳過」
// 的五種情況，每一種都必須有 note，不得靜默跳過。
func TestMultiRepo_Merge_SharedPathsExplicitNotes(t *testing.T) {
	t.Run("deleted-in-worktree", func(t *testing.T) {
		featureID := "feat-sp-note-deleted"
		root, _, ops := setupSharedPathsWorkspace(t, featureID, []string{composeFile})
		wtPath, err := ops.SetupWorktree(featureID, nil)
		if err != nil {
			t.Fatalf("SetupWorktree: %v", err)
		}
		if err := os.Remove(filepath.Join(wtPath, composeFile)); err != nil {
			t.Fatalf("remove worktree copy: %v", err)
		}

		result := ops.Merge(featureID, "Shared Paths Feature")
		if !containsSub(result.SharedPathsNotes, "missing in worktree, deletion not propagated") {
			t.Fatalf("notes = %v, want deletion note", result.SharedPathsNotes)
		}
		if len(result.SharedPathsMerged) != 0 {
			t.Errorf("deleted path must not be listed as merged: %v", result.SharedPathsMerged)
		}
		if _, err := os.Stat(filepath.Join(root, composeFile)); err != nil {
			t.Errorf("main workspace copy must be preserved: %v", err)
		}
	})

	t.Run("never-created", func(t *testing.T) {
		featureID := "feat-sp-note-never"
		_, _, ops := setupSharedPathsWorkspace(t, featureID, []string{"never-created.yml"})
		if _, err := ops.SetupWorktree(featureID, nil); err != nil {
			t.Fatalf("SetupWorktree: %v", err)
		}

		result := ops.Merge(featureID, "Shared Paths Feature")
		if !containsSub(result.SharedPathsNotes, "declared but never created in either workspace") {
			t.Fatalf("notes = %v, want never-created note", result.SharedPathsNotes)
		}
		if containsSub(result.SharedPathsNotes, "deletion not propagated") {
			t.Errorf("never-created must not reuse the deletion wording: %v", result.SharedPathsNotes)
		}
	})

	t.Run("non-git-root", func(t *testing.T) {
		featureID := "feat-sp-note-nongit"
		root, ws, ops := setupMultiWorkspace(t)
		writeFile(t, filepath.Join(root, composeFile), "services:\n  app:\n    image: base\n")
		saveSharedPathsFeature(t, ws, featureID, []string{composeFile})

		wtPath, err := ops.SetupWorktree(featureID, nil)
		if err != nil {
			t.Fatalf("SetupWorktree: %v", err)
		}
		const coderVersion = "services:\n  app:\n    image: coder\n"
		writeFile(t, filepath.Join(wtPath, composeFile), coderVersion)

		result := ops.Merge(featureID, "Shared Paths Feature")
		if !containsSub(result.SharedPathsNotes, "not committed (workspace root is not a git repo)") {
			t.Fatalf("notes = %v, want non-git-root note", result.SharedPathsNotes)
		}
		if got := readFileString(t, filepath.Join(root, composeFile)); got != coderVersion {
			t.Errorf("file must still be copied back: %q", got)
		}
	})

	t.Run("gitignored-shared-path", func(t *testing.T) {
		featureID := "feat-sp-note-ignored"
		root, ws, ops := setupMultiWorkspace(t)
		initRootRepo(t, root)
		writeFile(t, filepath.Join(root, ".gitignore"), composeFile+"\n")
		runGit(t, root, "add", "--", ".gitignore")
		runGit(t, root, "commit", "-m", "ignore compose")
		writeFile(t, filepath.Join(root, composeFile), "services:\n  app:\n    image: base\n")
		saveSharedPathsFeature(t, ws, featureID, []string{composeFile})

		wtPath, err := ops.SetupWorktree(featureID, nil)
		if err != nil {
			t.Fatalf("SetupWorktree: %v", err)
		}
		const coderVersion = "services:\n  app:\n    image: coder\n"
		writeFile(t, filepath.Join(wtPath, composeFile), coderVersion)
		preHead := gitOutput(root, "rev-parse", "HEAD")

		result := ops.Merge(featureID, "Shared Paths Feature")
		if !containsSub(result.SharedPathsNotes, "git add failed") {
			t.Fatalf("notes = %v, want git add failure note", result.SharedPathsNotes)
		}
		if got := readFileString(t, filepath.Join(root, composeFile)); got != coderVersion {
			t.Errorf("file must still be copied back: %q", got)
		}
		if got := gitOutput(root, "rev-parse", "HEAD"); got != preHead {
			t.Errorf("no commit should be produced: %s -> %s", preHead, got)
		}
	})

	t.Run("no-baseline-fail-open", func(t *testing.T) {
		featureID := "feat-sp-note-nobaseline"
		root, _, ops := setupSharedPathsWorkspace(t, featureID, []string{composeFile})
		wtPath, err := ops.SetupWorktree(featureID, nil)
		if err != nil {
			t.Fatalf("SetupWorktree: %v", err)
		}
		// 模擬使用者依文件刪掉基線 re-baseline 之後直接重跑 4x done。
		if err := os.Remove(SharedPathsBaselineFile(root, featureID)); err != nil {
			t.Fatalf("remove baseline: %v", err)
		}
		const coderVersion = "services:\n  app:\n    image: coder\n"
		writeFile(t, filepath.Join(wtPath, composeFile), coderVersion)

		result := ops.Merge(featureID, "Shared Paths Feature")
		if result.Error != "" {
			t.Fatalf("missing baseline must fail open, got error %q", result.Error)
		}
		if len(result.SharedPathsMerged) == 0 {
			t.Fatal("merge-back should still run when the baseline is missing")
		}
		if !containsSub(result.SharedPathsNotes, "drift detection skipped") {
			t.Errorf("notes = %v, want drift-detection-skipped note", result.SharedPathsNotes)
		}
		if !containsSub(result.SharedPathsNotes, "overwritten with the worktree version") {
			t.Errorf("notes = %v, want overwrite warning", result.SharedPathsNotes)
		}
	})
}

// TestMultiRepo_Merge_ConflictPreservesSharedPathChanges 驗證衝突路徑不遺失改動：
// 不觸及 Cleanup，故 worktree 與其中的 shared_path 改動都留著，主工作區也不被覆寫。
func TestMultiRepo_Merge_ConflictPreservesSharedPathChanges(t *testing.T) {
	featureID := "feat-sp-conflict"
	root, _, ops := setupSharedPathsWorkspace(t, featureID, []string{composeFile})

	for _, name := range []string{"core", "gate"} {
		repoPath := filepath.Join(root, name)
		writeFile(t, filepath.Join(repoPath, "conflict.go"), "package "+name+"\n// original\n")
		runGit(t, repoPath, "add", ".")
		runGit(t, repoPath, "commit", "-m", "add conflict.go")
	}

	wtPath, err := ops.SetupWorktree(featureID, nil)
	if err != nil {
		t.Fatalf("SetupWorktree: %v", err)
	}
	mainBefore := readFileString(t, filepath.Join(root, composeFile))
	const coderVersion = "services:\n  app:\n    image: coder\n"
	writeFile(t, filepath.Join(wtPath, composeFile), coderVersion)
	for _, name := range []string{"core", "gate"} {
		writeFile(t, filepath.Join(wtPath, name, "conflict.go"), "package "+name+"\n// worktree version\n")
	}
	if err := ops.Commit(wtPath, featureID, "wip"); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	for _, name := range []string{"core", "gate"} {
		repoPath := filepath.Join(root, name)
		writeFile(t, filepath.Join(repoPath, "conflict.go"), "package "+name+"\n// main different\n")
		runGit(t, repoPath, "add", ".")
		runGit(t, repoPath, "commit", "-m", "update conflict.go")
	}
	preHead := gitOutput(root, "rev-parse", "HEAD")

	result := ops.Merge(featureID, "Shared Paths Feature")
	if !result.Conflict {
		t.Fatalf("expected conflict, got %+v", result)
	}
	if _, err := os.Stat(wtPath); err != nil {
		t.Fatalf("worktree must be preserved on conflict: %v", err)
	}
	if got := readFileString(t, filepath.Join(wtPath, composeFile)); got != coderVersion {
		t.Errorf("worktree shared_path changed: %q", got)
	}
	if got := readFileString(t, filepath.Join(root, composeFile)); got != mainBefore {
		t.Errorf("main workspace must not be overwritten on conflict: %q", got)
	}
	if got := gitOutput(root, "rev-parse", "HEAD"); got != preHead {
		t.Errorf("root repo HEAD changed on conflict: %s -> %s", preHead, got)
	}
}

// TestMultiRepo_Merge_SharedPathsCommitSeparateFromSelfManaged 鎖住 DR-16：merge-back 的
// commit 與 commitSelfManaged 的 chore commit 是兩筆彼此路徑不重疊的 path-scoped commit，
// 不得互相吞併——後者的 GoDoc 明講「內容只有 4x 自身的 pipeline 狀態」。
func TestMultiRepo_Merge_SharedPathsCommitSeparateFromSelfManaged(t *testing.T) {
	featureID := "feat-sp-separate"
	root, ws, ops := setupSharedPathsWorkspace(t, featureID, []string{composeFile})

	featYAML := filepath.Join(protocol.DirName, protocol.FeaturesDir, featureID+".yaml")
	runGit(t, root, "add", "--", featYAML)
	runGit(t, root, "commit", "-m", "track feature yaml")

	wtPath, err := ops.SetupWorktree(featureID, nil)
	if err != nil {
		t.Fatalf("SetupWorktree: %v", err)
	}
	const coderVersion = "services:\n  app:\n    image: coder\n"
	writeFile(t, filepath.Join(wtPath, composeFile), coderVersion)

	// 主工作區的 4x 自管路徑同時是髒的（pipeline 期間 4x 自己寫入的狀態）。
	f, err := ws.LoadFeature(featureID)
	if err != nil {
		t.Fatalf("LoadFeature: %v", err)
	}
	f.Description = "updated by pipeline"
	if err := ws.SaveFeature(f); err != nil {
		t.Fatalf("SaveFeature: %v", err)
	}

	result := ops.Merge(featureID, "Shared Paths Feature")
	if result.Error != "" {
		t.Fatalf("merge error: %s", result.Error)
	}

	subjects := gitOutput(root, "log", "--format=%H %s")
	var choreSHA, featSHA string
	for _, line := range strings.Split(subjects, "\n") {
		sha, subject, ok := strings.Cut(line, " ")
		if !ok {
			continue
		}
		switch {
		case strings.HasPrefix(subject, "chore("+featureID+"): 4x pipeline state"):
			choreSHA = sha
		case strings.HasPrefix(subject, "feat("+featureID+"): "):
			featSHA = sha
		}
	}
	if choreSHA == "" || featSHA == "" {
		t.Fatalf("expected both commits, log:\n%s", subjects)
	}
	choreFiles := gitOutput(root, "show", "--name-only", "--format=", choreSHA)
	if strings.Contains(choreFiles, composeFile) {
		t.Errorf("self-managed commit must not contain the shared_path: %q", choreFiles)
	}
	featFiles := gitOutput(root, "show", "--name-only", "--format=", featSHA)
	if !strings.Contains(featFiles, composeFile) {
		t.Errorf("shared-path commit should contain %s: %q", composeFile, featFiles)
	}
	if strings.Contains(featFiles, protocol.DirName+"/") {
		t.Errorf("shared-path commit must not contain any .4x/ path: %q", featFiles)
	}
}

// TestMultiRepo_SharedPaths_NoDeclaration_ZeroRegression 驗證未宣告 shared_paths 時行為不變：
// 兩個新欄位為 nil、主工作區 root repo 不多出任何 commit、基線檔不被建立。
func TestMultiRepo_SharedPaths_NoDeclaration_ZeroRegression(t *testing.T) {
	featureID := "feat-sp-none"
	root, _, ops := setupSharedPathsWorkspace(t, featureID, nil)

	wtPath, err := ops.SetupWorktree(featureID, nil)
	if err != nil {
		t.Fatalf("SetupWorktree: %v", err)
	}
	if _, err := os.Stat(SharedPathsBaselineFile(root, featureID)); !os.IsNotExist(err) {
		t.Errorf("baseline must not be written without a declaration: %v", err)
	}

	preCount := gitOutput(root, "rev-list", "--count", "HEAD")
	writeFile(t, filepath.Join(wtPath, "core", "feature.go"), "package core\n")
	if err := ops.Commit(wtPath, featureID, "wip"); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	result := ops.Merge(featureID, "Shared Paths Feature")
	if result.Error != "" {
		t.Fatalf("merge error: %s", result.Error)
	}
	if result.SharedPathsMerged != nil || result.SharedPathsNotes != nil {
		t.Errorf("shared-path fields must stay nil: merged=%v notes=%v", result.SharedPathsMerged, result.SharedPathsNotes)
	}
	if got := gitOutput(root, "rev-list", "--count", "HEAD"); got != preCount {
		t.Errorf("root repo commit count changed: %s -> %s", preCount, got)
	}
}

// TestMultiRepo_SharedPaths_EndToEndSurvivesDone 是存活驗證：repo 內與根層的改動一起走完
// Commit → Merge（含 Cleanup），兩者都必須留在各自的 HEAD 裡。
func TestMultiRepo_SharedPaths_EndToEndSurvivesDone(t *testing.T) {
	featureID := "feat-sp-e2e"
	root, _, ops := setupSharedPathsWorkspace(t, featureID, []string{composeFile})

	wtPath, err := ops.SetupWorktree(featureID, nil)
	if err != nil {
		t.Fatalf("SetupWorktree: %v", err)
	}
	const coderVersion = "services:\n  app:\n    image: e2e\n"
	writeFile(t, filepath.Join(wtPath, composeFile), coderVersion)
	writeFile(t, filepath.Join(wtPath, "core", "feature.go"), "package core\n// e2e\n")
	if err := ops.Commit(wtPath, featureID, "wip"); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	result := ops.Merge(featureID, "Shared Paths Feature")
	if result.Error != "" {
		t.Fatalf("merge error: %s", result.Error)
	}
	if _, err := os.Stat(wtPath); !os.IsNotExist(err) {
		t.Fatalf("worktree should be removed: %v", err)
	}
	if got := gitOutput(filepath.Join(root, "core"), "show", "HEAD:feature.go"); got != "package core\n// e2e" {
		t.Errorf("repo change lost, core HEAD:feature.go = %q", got)
	}
	if got := gitOutput(root, "show", "HEAD:"+composeFile); got != strings.TrimRight(coderVersion, "\n") {
		t.Errorf("shared_path change lost, root HEAD:%s = %q", composeFile, got)
	}
}

// TestUpsertSharedPathsBaseline 覆蓋補齊型語意的五條規則：建檔、只補缺 key、既有值不覆寫、
// 全部存在時不重寫檔案、空清單 no-op。
func TestUpsertSharedPathsBaseline(t *testing.T) {
	const featureID = "feat-upsert"

	writeBaseline := func(t *testing.T, root string, m map[string]string) string {
		t.Helper()
		file := SharedPathsBaselineFile(root, featureID)
		if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		data, err := json.Marshal(m)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if err := os.WriteFile(file, data, 0o644); err != nil {
			t.Fatalf("write baseline: %v", err)
		}
		return file
	}
	loadBaselineMap := func(t *testing.T, root string) map[string]string {
		t.Helper()
		data, err := os.ReadFile(SharedPathsBaselineFile(root, featureID))
		if err != nil {
			t.Fatalf("read baseline: %v", err)
		}
		var m map[string]string
		if err := json.Unmarshal(data, &m); err != nil {
			t.Fatalf("unmarshal baseline: %v", err)
		}
		return m
	}

	t.Run("creates-when-absent", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, filepath.Join(root, "a.txt"), "A\n")
		if err := UpsertSharedPathsBaseline(root, featureID, []string{"a.txt"}); err != nil {
			t.Fatalf("Upsert: %v", err)
		}
		got := loadBaselineMap(t, root)
		if !strings.HasPrefix(got["a.txt"], "sha256:") {
			t.Errorf("baseline[a.txt] = %q, want sha256 hash", got["a.txt"])
		}
	})

	t.Run("fills-missing-key-only", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, filepath.Join(root, "a.txt"), "A\n")
		writeFile(t, filepath.Join(root, "b.txt"), "B\n")
		writeBaseline(t, root, map[string]string{"a.txt": "sha256:sentinel"})

		if err := UpsertSharedPathsBaseline(root, featureID, []string{"a.txt", "b.txt"}); err != nil {
			t.Fatalf("Upsert: %v", err)
		}
		got := loadBaselineMap(t, root)
		if got["a.txt"] != "sha256:sentinel" {
			t.Errorf("existing key rewritten: %q", got["a.txt"])
		}
		if !strings.HasPrefix(got["b.txt"], "sha256:") {
			t.Errorf("missing key not filled: %q", got["b.txt"])
		}
	})

	t.Run("keeps-existing-value", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, filepath.Join(root, "a.txt"), "A\n")
		writeBaseline(t, root, map[string]string{"a.txt": "sha256:sentinel"})
		// 現況內容改變也不得覆寫既有基線值（保住 resume 與 run 期間的原始基線）。
		writeFile(t, filepath.Join(root, "a.txt"), "CHANGED\n")

		if err := UpsertSharedPathsBaseline(root, featureID, []string{"a.txt"}); err != nil {
			t.Fatalf("Upsert: %v", err)
		}
		if got := loadBaselineMap(t, root)["a.txt"]; got != "sha256:sentinel" {
			t.Errorf("baseline[a.txt] = %q, want the original sentinel", got)
		}
	})

	t.Run("no-write-when-complete", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, filepath.Join(root, "a.txt"), "A\n")
		file := writeBaseline(t, root, map[string]string{"a.txt": "sha256:sentinel"})
		old := time.Now().Add(-2 * time.Hour)
		if err := os.Chtimes(file, old, old); err != nil {
			t.Fatalf("chtimes: %v", err)
		}
		before, err := os.Stat(file)
		if err != nil {
			t.Fatalf("stat: %v", err)
		}

		if err := UpsertSharedPathsBaseline(root, featureID, []string{"a.txt"}); err != nil {
			t.Fatalf("Upsert: %v", err)
		}
		after, err := os.Stat(file)
		if err != nil {
			t.Fatalf("stat: %v", err)
		}
		if !after.ModTime().Equal(before.ModTime()) {
			t.Errorf("baseline rewritten though every key already existed: %v -> %v", before.ModTime(), after.ModTime())
		}
	})

	t.Run("empty-paths-noop", func(t *testing.T) {
		root := t.TempDir()
		if err := UpsertSharedPathsBaseline(root, featureID, nil); err != nil {
			t.Fatalf("Upsert: %v", err)
		}
		if _, err := os.Stat(SharedPathsBaselineFile(root, featureID)); !os.IsNotExist(err) {
			t.Errorf("no baseline should be written for an empty declaration: %v", err)
		}
	})
}

// TestMainRootFor 驗證主工作區解析的三種情形；用真實目錄結構而非路徑字串假設。
func TestMainRootFor(t *testing.T) {
	const featureID = "feat-mainroot"

	t.Run("root-is-main", func(t *testing.T) {
		main := t.TempDir()
		if err := os.MkdirAll(Dir(main, featureID), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if got := MainRootFor(main, featureID); got != main {
			t.Errorf("MainRootFor = %q, want %q", got, main)
		}
	})

	t.Run("root-is-worktree", func(t *testing.T) {
		main := t.TempDir()
		wt := Dir(main, featureID)
		if err := os.MkdirAll(wt, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if got := MainRootFor(wt, featureID); got != main {
			t.Errorf("MainRootFor = %q, want %q", got, main)
		}
	})

	t.Run("unresolvable", func(t *testing.T) {
		if got := MainRootFor(t.TempDir(), featureID); got != "" {
			t.Errorf("MainRootFor = %q, want empty", got)
		}
	})
}

// TestMergeBackSharedPaths_UnreadableWorktreeCopy 鎖住「讀不到 worktree 副本」與「worktree 側
// 不存在」是兩條不同的 note：前者的內容仍在 worktree 內、隨後會被 Cleanup 刪掉，套用刪除文案
// 會讓使用者以為沒東西可救。
func TestMergeBackSharedPaths_UnreadableWorktreeCopy(t *testing.T) {
	mainRoot := t.TempDir()
	wtDir := t.TempDir()
	writeFile(t, filepath.Join(mainRoot, "dev.sh"), "main version\n")
	// worktree 側同名路徑做成目錄，讓 os.ReadFile 回 EISDIR 而非 ENOENT。
	if err := os.MkdirAll(filepath.Join(wtDir, "dev.sh"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	merged, notes := mergeBackSharedPaths(mainRoot, wtDir, "F186-x", "msg", []string{"dev.sh"})

	if len(merged) != 0 {
		t.Errorf("merged = %v, want empty", merged)
	}
	if !containsSub(notes, "cannot read the worktree copy") {
		t.Errorf("notes = %v, want one containing %q", notes, "cannot read the worktree copy")
	}
	if containsSub(notes, "deletion not propagated") {
		t.Errorf("notes = %v, must not claim the file was deleted", notes)
	}
	if got := readFileString(t, filepath.Join(mainRoot, "dev.sh")); got != "main version\n" {
		t.Errorf("main workspace copy = %q, want untouched", got)
	}
}

// TestSharedPathsPreflight_UnbaselinedPathReported 鎖住「基線檔存在但缺某個 path 的 key」不是
// 靜默跳過：那條路徑的 drift 偵測全程失效，必須有 note 說明它會被 worktree 版覆寫。
func TestSharedPathsPreflight_UnbaselinedPathReported(t *testing.T) {
	mainRoot := t.TempDir()
	wtDir := t.TempDir()
	featureID := "F186-x"
	writeFile(t, filepath.Join(mainRoot, "docker-compose.yml"), "compose\n")
	writeFile(t, filepath.Join(mainRoot, "Dockerfile"), "FROM scratch\n")
	// 基線只含 docker-compose.yml：模擬 Dockerfile 是基線建立之後才被加進宣告。
	if err := UpsertSharedPathsBaseline(mainRoot, featureID, []string{"docker-compose.yml"}); err != nil {
		t.Fatalf("UpsertSharedPathsBaseline: %v", err)
	}

	notes, abortErr := sharedPathsPreflight(mainRoot, wtDir, featureID, []string{"docker-compose.yml", "Dockerfile"})

	if abortErr != "" {
		t.Errorf("abortErr = %q, want empty", abortErr)
	}
	if !containsSub(notes, "Dockerfile: not in the shared-paths baseline") {
		t.Errorf("notes = %v, want one naming the unbaselined path", notes)
	}
	if containsSub(notes, "docker-compose.yml: not in the shared-paths baseline") {
		t.Errorf("notes = %v, must not report the baselined path", notes)
	}
}
