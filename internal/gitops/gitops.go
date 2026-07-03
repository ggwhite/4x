// Package gitops 封裝所有 git 操作，根據 workspace config 決定 monorepo 或 multi-repo 模式。
package gitops

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/ggwhite/4x/internal/protocol"
	"github.com/ggwhite/4x/internal/vcshub"
)

// Ops 封裝所有 git 操作，根據 workspace config 決定 monorepo 或 multi-repo 模式。
type Ops interface {
	SetupWorktree(featureID string, featureRepos []string) (wtRoot string, err error)
	Commit(wtRoot, featureID, msg string) error
	Merge(featureID, featureName string) MergeResult
	// PushAndOpenMR 取代 Merge，push feature branch 並對有 committed 變更的 repo 開 MR/PR，
	// 供 issue_tracker.enabled 時的 `4x done` 使用（見 MergeAndFinalize）。
	PushAndOpenMR(featureID, featureName string) MergeResult
	Cleanup(featureID string) error
	DetectChangedRepos(featureID string) []string
	DetectChangedFiles(featureID string) []protocol.ChangedFile
	CaptureBaseline(featureID string, featureRepos []string) error
	IsMultiRepo() bool
}

// vcshubNew 對應 vcshub.New，測試可覆寫以注入 fakeHub。
var vcshubNew = vcshub.New

// MergeResult 描述 Merge 操作的結果。
//
// StateChanged 與 FinalState 由 MergeAndFinalize 在 merge 後填入：StateChanged 表 merge 期間
// state.json 的 phase 已非 pending-review（未 finalize），FinalState 為成功 finalize 後寫入的最新 state
// （或 StateChanged 時偵測到的當下 state）。Merge 本身不設定這兩個欄位。
type MergeResult struct {
	Skipped      bool
	Conflict     bool
	Error        string
	Files        []string
	ConflictRepo string
	StateChanged bool
	FinalState   protocol.State
	// MRUrls 記錄 PushAndOpenMR 成功開出的 MR/PR URL，key 為 monorepo 固定 "."、multirepo 為 repo 名。
	MRUrls map[string]string
}

// New 根據 workspace config 建立對應的 Ops 實作。
func New(root string, ws *protocol.Workspace, cfg protocol.Config) Ops {
	if len(cfg.Workspace.Repos) > 0 {
		return &multiRepo{root: root, ws: ws, cfg: cfg}
	}
	return &monoRepo{root: root, ws: ws}
}

// Dir 回傳 worktree 組合目錄的路徑（兩種模式共用）。
func Dir(root, featureID string) string {
	return filepath.Join(root, ".worktrees", "4x", featureID)
}

// Branch 回傳 feature 對應的 branch 名稱（兩種模式共用）。
func Branch(featureID string) string {
	return "4x/" + featureID
}

func gitOutput(dir string, args ...string) string {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// changedFilesIn 回傳 dir（一個 git 工作目錄）內的檔案層變更清單。
// tracked 變更取自 git diff --numstat HEAD（added+deleted 行數，binary 顯示為 "-" 時計 0）；
// untracked 新檔取自 git ls-files --others --exclude-standard，行數為檔案總行數。
// pathPrefix 會被加在每個回傳 Path 之前（multi-repo 用 repo 名稱前綴；mono 傳空字串）。
// 兩條指令各自獨立容錯，單一失敗不影響另一條已找到的變更（與 DetectChangedRepos 一致）。
func changedFilesIn(dir, pathPrefix string) []protocol.ChangedFile {
	var files []protocol.ChangedFile

	numstatCmd := exec.Command("git", "diff", "--numstat", "HEAD")
	numstatCmd.Dir = dir
	if out, err := numstatCmd.Output(); err == nil {
		for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			if line == "" {
				continue
			}
			// numstat 格式：<added>\t<deleted>\t<path>；binary 檔 added/deleted 為 "-"。
			parts := strings.SplitN(line, "\t", 3)
			if len(parts) < 3 {
				continue
			}
			added := ParseNumstatCount(parts[0])
			deleted := ParseNumstatCount(parts[1])
			files = append(files, protocol.ChangedFile{Path: pathPrefix + parts[2], Lines: added + deleted})
		}
	}

	untrackedCmd := exec.Command("git", "ls-files", "--others", "--exclude-standard")
	untrackedCmd.Dir = dir
	if out, err := untrackedCmd.Output(); err == nil {
		for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			if line == "" {
				continue
			}
			lines := CountFileLines(filepath.Join(dir, line))
			files = append(files, protocol.ChangedFile{Path: pathPrefix + line, Lines: lines})
		}
	}

	return files
}

// ParseNumstatCount 解析 numstat 的 added/deleted 欄位，binary 檔的 "-" 視為 0。
func ParseNumstatCount(s string) int {
	if s == "-" {
		return 0
	}
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int(c-'0')
	}
	return n
}

// CountFileLines 回傳檔案的行數（以 '\n' 計），讀檔失敗回 0。
// 末行無換行時補計 1 行，使單行無換行的檔案計為 1 而非 0。
func CountFileLines(path string) int {
	data, err := os.ReadFile(path)
	if err != nil || len(data) == 0 {
		return 0
	}
	lines := strings.Count(string(data), "\n")
	if data[len(data)-1] != '\n' {
		lines++
	}
	return lines
}

func ensureGitignore(root, entry string) {
	path := filepath.Join(root, ".gitignore")
	data, _ := os.ReadFile(path)
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == entry {
			return
		}
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	if len(data) > 0 && data[len(data)-1] != '\n' {
		f.WriteString("\n")
	}
	f.WriteString(entry + "\n")
}

// copyFileIfExists 複製檔案；來源不存在視為靜默成功（回 nil），
// 僅讀寫失敗（如 disk full、目標不可寫）才回傳 error，讓上游能記錄真因。
func copyFileIfExists(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o644)
}

// CopyFileIfExists 複製檔案，來源不存在時靜默忽略（回 nil）；讀寫失敗回傳 error。供外部 package 使用。
func CopyFileIfExists(src, dst string) error {
	return copyFileIfExists(src, dst)
}

// CopyFileIfNewer 僅在來源比目標新（或目標不存在）時複製，用於差量化 worktree sync，
// 避免每輪 final sync 與每 2 秒 live-sync 對未變更檔案重複全量複製。
// 回傳是否實際複製、以及錯誤；來源不存在視為靜默成功（false, nil）。
// 比對採 mtime-only：dst 不存在則複製；兩者皆存在時 srcInfo.ModTime().After(dstInfo.ModTime()) 才複製。
func CopyFileIfNewer(src, dst string) (copied bool, err error) {
	srcInfo, err := os.Stat(src)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	dstInfo, err := os.Stat(dst)
	if err != nil {
		if !os.IsNotExist(err) {
			return false, err
		}
		// 目標不存在 → 複製
	} else if !srcInfo.ModTime().After(dstInfo.ModTime()) {
		// 目標存在且不比來源舊 → 跳過
		return false, nil
	}
	if err := copyFileIfExists(src, dst); err != nil {
		return false, err
	}
	return true, nil
}

// isNothingToCommit 判斷 git commit 的輸出是否表示「無東西可 commit」。
// git 在不同情境有不同訊息：clean tree 是 "nothing to commit"，
// 有 untracked files 時是 "nothing added to commit"。
func isNothingToCommit(out string) bool {
	return strings.Contains(out, "nothing to commit") ||
		strings.Contains(out, "nothing added to commit")
}

// syncDotDirContents 將 mainRoot 的 .4x/settings.json 和 plugins/ 複製到 dotDir。
func syncDotDirContents(mainRoot, dotDir string) {
	os.MkdirAll(dotDir, 0o755)

	src := filepath.Join(mainRoot, protocol.DirName, protocol.ConfigFile)
	dst := filepath.Join(dotDir, protocol.ConfigFile)
	if data, err := os.ReadFile(src); err == nil {
		os.WriteFile(dst, data, 0o644)
	}

	srcPlugins := filepath.Join(mainRoot, protocol.DirName, "plugins")
	dstPlugins := filepath.Join(dotDir, "plugins")
	if entries, err := os.ReadDir(srcPlugins); err == nil {
		os.MkdirAll(dstPlugins, 0o755)
		for _, e := range entries {
			if !e.IsDir() {
				copyFileIfExists(filepath.Join(srcPlugins, e.Name()), filepath.Join(dstPlugins, e.Name())) //nolint:errcheck // best-effort 複製 plugin 檔，缺檔可接受
			}
		}
	}
}

// loadBaseline 讀取 featureID 的 baseline.json，供 PushAndOpenMR 決定各 repo 的 MR target branch。
func loadBaseline(ws *protocol.Workspace, featureID string) (protocol.Baseline, error) {
	data, err := os.ReadFile(filepath.Join(ws.FeatureDir(featureID), protocol.BaselineFile))
	if err != nil {
		return protocol.Baseline{}, err
	}
	var baseline protocol.Baseline
	if err := json.Unmarshal(data, &baseline); err != nil {
		return protocol.Baseline{}, err
	}
	return baseline, nil
}

// captureRepoBaseline 擷取單一 repo 的 baseline 狀態，若非 git repo 則回傳 nil。
func captureRepoBaseline(fullPath, name, repoPath string) *protocol.BaselineRepo {
	if _, err := os.Stat(filepath.Join(fullPath, ".git")); err != nil {
		return nil
	}
	head := gitOutput(fullPath, "rev-parse", "HEAD")
	branch := gitOutput(fullPath, "rev-parse", "--abbrev-ref", "HEAD")
	statusOut := gitOutput(fullPath, "status", "--short")

	var dirty []string
	for _, line := range strings.Split(statusOut, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			dirty = append(dirty, line)
		}
	}
	return &protocol.BaselineRepo{
		Name:       name,
		Path:       repoPath,
		Branch:     branch,
		Head:       head,
		DirtyFiles: dirty,
	}
}

func conflictFiles(root string) []string {
	out, err := exec.Command("git", "-C", root, "diff", "--name-only", "--diff-filter=U").Output()
	if err != nil {
		return nil
	}
	var files []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line != "" {
			files = append(files, line)
		}
	}
	return files
}

// autoResolveFeatureYAML 自動解決 .4x/features/*.yaml 的 merge 衝突，
// 取 main（ours）的版本，因為 4x run 在 main 維護最新的 status 欄位。
func autoResolveFeatureYAML(root string, files []string) {
	for _, f := range files {
		if !strings.HasPrefix(f, ".4x/features/") || !strings.HasSuffix(f, ".yaml") {
			continue
		}
		exec.Command("git", "-C", root, "checkout", "--ours", "--", f).Run()
		exec.Command("git", "-C", root, "add", f).Run()
	}
}
