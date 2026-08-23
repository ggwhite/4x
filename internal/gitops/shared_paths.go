package gitops

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ggwhite/4x/internal/protocol"
)

// sharedPathsFor 讀 feature YAML 回傳宣告的 shared_paths；載入失敗時回 nil。
// 載入失敗屬 fail-open（維持既有行為，不阻擋 merge），但一律先 slog.Warn：
// 否則 YAML 壞掉時 merge-back 會靜默 no-op、無任何跡象可循。
func sharedPathsFor(ws *protocol.Workspace, featureID string) []string {
	feat, err := ws.LoadFeature(featureID)
	if err != nil {
		slog.Warn("shared_paths: load feature failed", "feature", featureID, "err", err)
		return nil
	}
	return feat.SharedPaths
}

// MainRootFor 回傳 featureID 對應的主工作區根目錄。
//
// 三種情形：root 本身即主工作區（Dir(root, featureID) 存在）→ 回 root；
// root 是 worktree 組合目錄 <main>/.worktrees/4x/<featureID> → 回 <main>；
// 兩者皆不符（非 worktree 隔離模式或路徑無法判定）→ 回空字串。
//
// 不能改用既有的 MainWorkspaceRoot：那個走 git（DetectWorktree），而 multi-repo 的 worktree
// 組合目錄本身不是 git repo（各 repo 的 worktree 在它的子目錄下），對它回 IsLinked=false。
// 代價是本函式用純路徑比對（Dir(candidate, featureID) == root），對未 Clean 或含 symlink 的
// root 會判不出來，而 MainWorkspaceRoot 走的 resolveGitPath 已處理過那類差異。
// 需要「取主工作區 root」時的選用準則：multi-repo 用本函式，單一 git repo 用 MainWorkspaceRoot。
func MainRootFor(root, featureID string) string {
	if _, err := os.Stat(Dir(root, featureID)); err == nil {
		return root
	}
	candidate := filepath.Dir(filepath.Dir(filepath.Dir(root)))
	if Dir(candidate, featureID) == root {
		return candidate
	}
	return ""
}

// ValidateSharedPathsInRoot 檢查 shared_paths 宣告值在主工作區 root 下是否可被 merge-back 處理。
//
// feature.ValidateSharedPaths 只擋路徑分隔符，擋不掉「根層目錄名」——宣告 deployments 這類
// 真實存在的根層目錄能通過該檢查，卻會被 copyWorkspaceFiles 的 IsDir 跳過，worktree 內根本
// 不存在該路徑。本函式補上兩條規則：
//   - 宣告值在 root 下解析為目錄 → 拒絕（目錄型 shared_paths 不支援）
//   - 宣告值為 go.work / go.work.sum → 拒絕；worktree 內的 go.work 是 copyGoWork 產出的裁切版，
//     merge-back 會用它覆蓋主工作區的完整 go.work，砍掉其他 module 的 use 行
//
// 兩種 workspace 模式皆適用，刻意不比照 checkSharedPathsPollution 加上 multi-repo 限縮：
// 本函式管的是宣告值的格式（必須是根層檔案），不是 merge-back 的執行條件，而 shared_paths
// 的語意在兩種模式下相同——目錄型宣告在 monorepo 下同樣沒有任何消費端支援。
// monorepo feature 若已宣告根層目錄名，升級後 4x check 會開始擋，需改宣告。
//
// 不存在的 path 不算錯（Coder 可能要新建）。錯誤訊息列出所有違規項。
func ValidateSharedPathsInRoot(root string, paths []string) error {
	var issues []string
	for _, p := range paths {
		tp := strings.TrimSpace(p)
		if tp == "" {
			continue
		}
		if isGoWorkFile(tp) {
			issues = append(issues, fmt.Sprintf("shared_paths %q is managed by copyGoWork and must not be declared", p))
			continue
		}
		if info, err := os.Stat(filepath.Join(root, tp)); err == nil && info.IsDir() {
			issues = append(issues, fmt.Sprintf("shared_paths %q is a directory in the workspace root; only root-level files are supported", p))
		}
	}
	if len(issues) == 0 {
		return nil
	}
	return fmt.Errorf("%s", strings.Join(issues, "; "))
}

// SharedPathsBaselineFile 回傳 featureID 的 shared_paths 快照基線檔路徑。
func SharedPathsBaselineFile(mainRoot, featureID string) string {
	return filepath.Join(mainRoot, protocol.DirName, protocol.RunDir, featureID, protocol.SharedPathsBaselineFile)
}

// sharedPathHash 回傳主工作區該檔內容的 "sha256:<hex>"；檔案不存在時回空字串，
// 作為「取樣當下不存在」的哨兵值。
func sharedPathHash(mainRoot, path string) string {
	data, err := os.ReadFile(filepath.Join(mainRoot, path))
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// loadSharedPathsBaseline 讀基線檔；不存在或無法 parse 時回 (nil, false)。
func loadSharedPathsBaseline(mainRoot, featureID string) (map[string]string, bool) {
	data, err := os.ReadFile(SharedPathsBaselineFile(mainRoot, featureID))
	if err != nil {
		return nil, false
	}
	var baseline map[string]string
	if err := json.Unmarshal(data, &baseline); err != nil {
		return nil, false
	}
	return baseline, true
}

// UpsertSharedPathsBaseline 補齊 featureID 的 shared_paths 快照基線。
//
// 語意刻意是「補齊」而非「覆寫」：既有 key 的值一律不動，只為 map 中尚不存在的 key 補寫
// 現況 hash（檔案不存在時寫空字串哨兵）。SetupWorktree 在 Designer 宣告之前就執行，那時
// len(paths) 通常為 0；真正建出基線的時點是 designing 收尾那次 4x check（見 guard 的
// checkSharedPathsPollution）。兩條寫入路徑共用本函式，「既有 key 不動」讓它們互不衝突，
// 也保住 resume 與 run 期間的原始基線。
//
// len(paths) == 0 時不寫檔直接回 nil（未宣告 shared_paths 的 feature 零回歸）；
// 所有 key 都已存在時同樣不寫檔，避免每輪 4x check 無謂改寫 mtime。
// 宣告被移除的 path 其 key 保留不刪：留著無害，刪掉會讓「移除後又加回」重取基線。
func UpsertSharedPathsBaseline(mainRoot, featureID string, paths []string) error {
	if len(paths) == 0 {
		return nil
	}
	baseline, found := loadSharedPathsBaseline(mainRoot, featureID)
	if !found {
		baseline = make(map[string]string, len(paths))
	}
	changed := !found
	for _, p := range paths {
		if _, ok := baseline[p]; ok {
			continue
		}
		baseline[p] = sharedPathHash(mainRoot, p)
		changed = true
	}
	if !changed {
		return nil
	}
	data, err := json.MarshalIndent(baseline, "", "  ")
	if err != nil {
		return err
	}
	file := SharedPathsBaselineFile(mainRoot, featureID)
	// SetupWorktree 早於 ws.InitFeatureDir 執行，.4x/run/<id>/ 此時可能還不存在。
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return err
	}
	return os.WriteFile(file, data, 0o644)
}

// DriftedSharedPaths 回傳主工作區內容已偏離快照基線的 shared_paths（已排序）、
// 基線檔內沒有對應 key 因而無從比對的 shared_paths（已排序），以及基線檔是否存在。
//
// 判準只比對「主工作區現況 hash vs 基線 hash」，不呼叫任何 git 指令：主工作區可能存在
// 從未被 root repo 追蹤的合法宣告（如 .env），用「相對 HEAD」判準會讓它恆為 dirty。
//
// 基線 map 缺該 key（feature YAML 在執行期間才新增宣告，中間沒有任何 4x check 補基線）→
// 不判 drift，改由 unbaselined 回報。單靠 baselineFound 蓋不住這種情形——它只回報「整份基線檔
// 缺席」，部分 key 缺席時它仍是 true，那些 path 的 drift 偵測會全程失效且不產生任何提示。
func DriftedSharedPaths(mainRoot, featureID string, paths []string) (drifted, unbaselined []string, baselineFound bool) {
	baseline, found := loadSharedPathsBaseline(mainRoot, featureID)
	if !found {
		return nil, nil, false
	}
	for _, p := range paths {
		want, ok := baseline[p]
		if !ok {
			unbaselined = append(unbaselined, p)
			continue
		}
		if sharedPathHash(mainRoot, p) != want {
			drifted = append(drifted, p)
		}
	}
	sort.Strings(drifted)
	sort.Strings(unbaselined)
	return drifted, unbaselined, true
}

// sharedPathsPreflight 是 Merge 與 PushAndOpenMR 共用的 shared_paths 前置檢查：判 drift、
// 組中止訊息、基線缺席時產生 fail-open note。
//
// abortErr 非空時呼叫端必須立刻回 MergeResult{Error: abortErr} 且不觸碰任何 repo；
// notes 要接進 MergeResult.SharedPathsNotes。兩個呼叫端曾各寫一份逐字相同的複本，
// 日後調整 fail-open 條件或新增 note 只改一邊，就會讓 merge 路徑與 MR 路徑行為分歧。
//
// paths 由呼叫端傳入而不在此載入：PushAndOpenMR 已有 LoadFeature 的結果，
// 在這裡再讀一次 YAML 除了多一次 parse，還會讓載入失敗時的 log 指錯根因。
func sharedPathsPreflight(mainRoot, wtDir, featureID string, paths []string) (notes []string, abortErr string) {
	drifted, unbaselined, baselineFound := DriftedSharedPaths(mainRoot, featureID, paths)
	if len(drifted) > 0 {
		return nil, sharedPathsDriftError(drifted, wtDir, SharedPathsBaselineFile(mainRoot, featureID))
	}
	warn := func(note string) {
		notes = append(notes, note)
		slog.Warn("shared_paths merge-back", "feature", featureID, "note", note)
	}
	if !baselineFound && len(paths) > 0 {
		warn(sharedPathsNoBaselineNote)
	}
	for _, p := range unbaselined {
		warn(fmt.Sprintf(sharedPathsUnbaselinedNoteFormat, p))
	}
	return notes, ""
}

// isGitRepo 回報 dir 是否位於一個 git 工作目錄內。
func isGitRepo(dir string) bool {
	return exec.Command("git", "-C", dir, "rev-parse", "--git-dir").Run() == nil
}

// mergeBackSharedPaths 把 worktree 內被改動的 shared_paths 複製回主工作區並 path-scoped commit，
// 回傳實際合併的路徑（已排序）與所有無法納入 commit／無法傳播情況的說明 note。
//
// 以下 note 分支互斥且窮盡「無法納入 commit」的全部情況，不留靜默路徑：worktree 側檔案不存在
// （再分為被 Coder 刪除與兩側都未建立）、worktree 側檔案讀不到（非 ENOENT 的讀取失敗）、
// 複製到主工作區失敗、主工作區根層非 git repo、git add 失敗（多半被 .gitignore 排除）、commit 失敗。
// 刻意不寫死條數：新增分支時只要對照本清單，不必回頭改數字，也就不會像上一版那樣列舉與程式碼脫節。
// 所有 note 同時以 slog.Warn 輸出（stderr），不得走 stdout——
// 那會污染 4x done --json / 4x merge --json 的 JSON。
//
// commit 一律 path-scoped（add/commit 都帶 "--" 與明確路徑清單），不吞掉主工作區其他
// dirty 檔案；與 commitSelfManaged 的 chore commit 是兩筆彼此獨立的 commit，路徑不重疊。
// 只有 commit 成功才刪基線檔：不刪的話基線會停在 merge-back 前的舊 hash，此後任何對同一
// featureID 的 guard.Check() 都恆判 drift。刪除條件刻意不是「主工作區已被改寫」——複製發生在
// 迴圈內（見下方 copyFileIfExists），非 git repo／git add 失敗／commit 失敗三條 return 路徑
// 都已經改寫過主工作區卻仍保留基線。那是刻意的：那三條路徑的改動沒有落成 commit，保留舊 hash
// 讓後續 guard.Check() 判出的 drift 正好對應「主工作區被動過但未入 commit」這個事實。
func mergeBackSharedPaths(mainRoot, wtDir, featureID, msg string, paths []string) (merged, notes []string) {
	addNote := func(format string, args ...any) {
		n := fmt.Sprintf(format, args...)
		notes = append(notes, n)
		slog.Warn("shared_paths merge-back", "feature", featureID, "note", n)
	}

	for _, p := range paths {
		wtFile := filepath.Join(wtDir, p)
		mainFile := filepath.Join(mainRoot, p)

		wtData, err := os.ReadFile(wtFile)
		if err != nil && !os.IsNotExist(err) {
			// 讀取失敗（EACCES、EISDIR、I/O error）不等於「沒有內容可搬」：worktree 側那份改動
			// 仍在，而呼叫端接著就 Cleanup 掉整個 worktree。套用下方的 deleted / never-created
			// 文案會讓使用者相信沒東西可救，正是本 feature 要消滅的靜默遺失，故獨立一條 note。
			addNote("%s: cannot read the worktree copy, not merged back: %v", p, err)
			continue
		}
		if err != nil {
			if _, mainErr := os.Stat(mainFile); mainErr == nil {
				addNote("%s: missing in worktree, deletion not propagated", p)
			} else {
				addNote("%s: declared but never created in either workspace", p)
			}
			continue
		}
		if mainData, err := os.ReadFile(mainFile); err == nil && string(mainData) == string(wtData) {
			continue
		}
		if err := copyFileIfExists(wtFile, mainFile); err != nil {
			addNote("%s: copy to main workspace failed: %v", p, err)
			continue
		}
		merged = append(merged, p)
	}
	sort.Strings(merged)

	if len(merged) == 0 {
		return merged, notes
	}

	joined := strings.Join(merged, ", ")
	if !isGitRepo(mainRoot) {
		addNote("%s: copied to main workspace but not committed (workspace root is not a git repo)", joined)
		return merged, notes
	}

	addArgs := append([]string{"-C", mainRoot, "add", "--"}, merged...)
	if out, err := exec.Command("git", addArgs...).CombinedOutput(); err != nil {
		addNote("%s: copied to main workspace but git add failed (path may be gitignored): %s", joined, strings.TrimSpace(string(out)))
		return merged, notes
	}

	commitArgs := append([]string{"-C", mainRoot, "commit", "-m", msg, "--"}, merged...)
	if out, err := exec.Command("git", commitArgs...).CombinedOutput(); err != nil && !isNothingToCommit(string(out)) {
		addNote("%s: copied to main workspace but commit failed: %s", joined, strings.TrimSpace(string(out)))
		return merged, notes
	}

	if err := os.Remove(SharedPathsBaselineFile(mainRoot, featureID)); err != nil && !os.IsNotExist(err) {
		slog.Warn("shared_paths: remove baseline failed", "feature", featureID, "err", err)
	}
	return merged, notes
}

// sharedPathsDriftError 組出 preflight 中止時的錯誤訊息，含解除指引。
//
// 指引的第一步必須是「先把主工作區的改動併進 worktree 的同名檔案」，且刻意不提供
// 「revert 主工作區」這條路：drift 的唯一實際成因是平行 feature 已 merge-back 過同一個檔案，
// 此時 revert 或直接 re-baseline 都會讓下一次 merge-back 以 worktree 版覆寫主工作區，
// 把對方剛落地的內容再度抹掉——正是本 feature 要消滅的靜默覆蓋。
func sharedPathsDriftError(drifted []string, wtDir, baselineFile string) string {
	return fmt.Sprintf("shared_paths dirty in main workspace, aborting merge: %s; %s and retry",
		strings.Join(drifted, ", "), SharedPathsDriftHint(wtDir, baselineFile))
}

// SharedPathsDriftHint 回傳 drift 的解除指引，是這段文案的唯一來源。
//
// 兩個消費端共用：gitops 的 merge preflight（sharedPathsDriftError）與 guard 的反向污染檢查
// （checkSharedPathsPollution）。兩者前綴不同是刻意的（一個講「中止 merge」、一個講「執行期間被改」），
// 但解除步驟必須逐字一致——同一個 drift 在 4x check 與 4x done 拿到互相矛盾的指引，
// 使用者就無法判斷該信哪一個。
func SharedPathsDriftHint(wtDir, baselineFile string) string {
	return fmt.Sprintf(
		"first merge those main-workspace changes into the worktree copy under %s (merge-back overwrites the main workspace with the worktree version), then delete %s to re-baseline",
		wtDir, baselineFile)
}

// sharedPathsNoBaselineNote 是基線缺席時的 fail-open note。
// 後半句明講主工作區的差異檔案將被 worktree 版覆寫：使用者依文件刪掉基線檔 re-baseline 後會走進
// 這條，必須讓他能自行核對平行 feature 的內容有沒有被蓋掉。
//
// 用未來式而非過去式：這則 note 在兩個呼叫端都是 preflight 階段 append，早於 mergeBackSharedPaths
// 得知是否真的複製過任何檔案；兩側內容相同（shared_paths.go 的 continue）與 worktree 側檔案不存在
// 兩條路徑都不會覆寫任何東西，用過去式會讓使用者去追一場沒發生的資料遺失。
const sharedPathsNoBaselineNote = "shared-paths baseline missing, drift detection skipped; any differing main workspace copies will be overwritten with the worktree version"

// sharedPathsUnbaselinedNoteFormat 是「基線檔存在但缺該 path 的 key」時的 per-path fail-open note。
// 觸發形態：feature YAML 在基線建立之後才新增宣告，且中間沒有任何 4x check 補上該 key。
// 語氣比照 sharedPathsNoBaselineNote，同樣用未來式（append 時點在 preflight，早於實際覆寫）。
const sharedPathsUnbaselinedNoteFormat = "%s: not in the shared-paths baseline, drift detection skipped for it; a differing main workspace copy will be overwritten with the worktree version"
