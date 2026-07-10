package guard

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ggwhite/4x/internal/gitops"
	"github.com/ggwhite/4x/internal/protocol"
)

// maxFanoutFileSize 是 fan-out 掃描時單一檔案的大小上限（1 MiB）；超過者跳過，
// 避免對大型產出物做全檔字串比對而拖慢 guard。
const maxFanoutFileSize = 1 << 20

// fanoutSkipDirs 是 fan-out 掃描時要整段跳過的目錄名（版本控制 / 4x 產物 / 常見
// vendor 與 build 產出），避免掃到與下游 import 無關的第三方或衍生檔案。
var fanoutSkipDirs = map[string]bool{
	".git":           true,
	protocol.DirName: true, // ".4x"
	".worktrees":     true,
	"node_modules":   true,
	"vendor":         true,
	"dist":           true,
	"build":          true,
}

// checkProtoFanoutScope 是與 checkScope 平行的非阻斷 sibling gate（F171）：當本輪變更
// 含 proto/interface 定義檔時，用單純字串比對找出 workspace 內 import 該定義 path 的 repo，
// 若該 repo 未列於 feature.Repos，就 append 一條 Warn 提醒覆核是否需擴 scope。
//
// 此 gate 刻意只走 Warn、絕不設 r.Pass=false 或 append Errors：字串比對（grep-based）本質可能
// false positive，故僅供 Designer/後續角色參考，不阻斷 transition（見 task-brief 設計裁決）。
//
// 邊界（與 checkScope 對齊）：
//   - 讀 feature / config 失敗 → append Warn 後返回（fail-open，不 fail closed）。
//   - feature.Repos 為空 → 視為未限制 repo scope，直接返回、不提 missing repo warning。
//
// 變更檔窗口沿用既有 guard 的 uncommitted `git diff HEAD` + untracked 語意（gitops.ChangedPaths），
// 不做 committed history / merge-base 分析、不做 AST 或完整依賴圖。
func checkProtoFanoutScope(ws *protocol.Workspace, featureID string, r *CheckResult) {
	feature, err := ws.LoadFeature(featureID)
	if err != nil {
		r.Warns = append(r.Warns, fmt.Sprintf("proto-fanout: cannot load feature YAML: %v", err))
		return
	}
	// feature.Repos 為空 → 未限制 repo scope，與 checkScope 空 repos 早退行為對齊。
	if len(feature.Repos) == 0 {
		return
	}

	cfg, err := ws.ReadConfig()
	if err != nil {
		r.Warns = append(r.Warns, fmt.Sprintf("proto-fanout: cannot read config: %v", err))
		return
	}

	// 掃描全部 workspace repos，而非只掃 feature.Repos——找「未 provision 但可能受變更影響的
	// 下游 repo」是本 gate 的核心。ws.Root 為 worktree 時，未 provision 進 worktree 的 sibling repo
	// 其 ResolveRepoPaths 路徑並不存在，故改用 resolveFanoutRepoPaths 以 origin(main) root 定位真實
	// 路徑；無法定位者不納入掃描而改 append「無法驗證」Warn（見 helper GoDoc）。
	repoPaths := resolveFanoutRepoPaths(cfg, ws.Root, r)

	allowed := make(map[string]bool, len(feature.Repos))
	for _, name := range feature.Repos {
		allowed[name] = true
	}

	// 依 repo 名稱排序讓變更偵測與 warning 產生順序穩定（map 迭代序不定），便於測試斷言。
	changedRepos := make([]string, 0, len(repoPaths))
	for name := range repoPaths {
		changedRepos = append(changedRepos, name)
	}
	sort.Strings(changedRepos)

	type fanoutKey struct{ repo, trigger string }
	seen := make(map[fanoutKey]bool)

	for _, changedRepo := range changedRepos {
		repoPath := repoPaths[changedRepo]
		for _, changed := range changedProtoInterfaceFiles(repoPath) {
			trigger := changed
			if changedRepo != "." {
				trigger = changedRepo + "/" + changed
			}
			terms := importSearchTerms(trigger)
			importing := scanImportingRepos(repoPaths, terms, changedRepo)

			importers := make([]string, 0, len(importing))
			for name := range importing {
				importers = append(importers, name)
			}
			sort.Strings(importers)

			for _, repo := range importers {
				if allowed[repo] {
					continue
				}
				key := fanoutKey{repo: repo, trigger: trigger}
				if seen[key] {
					continue
				}
				seen[key] = true
				r.Warns = append(r.Warns, fmt.Sprintf(
					"proto-fanout scope violation 建議覆核：repo %q 以字串比對（grep-based，可能誤判）import 了本輪變更的 proto/interface 定義 %q，但未列於 feature.repos %v——請覆核是否需擴 scope 納入該 repo",
					repo, trigger, feature.Repos))
			}
		}
	}
}

// originWorkspaceRoot 定位 wsRoot 對應的 origin(main) workspace root，供 worktree 情境下解析
// 「未 provision 進 worktree 的 sibling repo」在 main working tree 內的真實路徑。回傳的 root 語意為
// 「workspace 容器根」，使呼叫端能以 filepath.Join(root, rc.Path) 還原任一 sibling repo 的 main 路徑。
//
// 定位順序：
//   - 先試 gitops.MainWorkspaceRoot(wsRoot)：涵蓋 mono-repo worktree（ws.Root 自身即 linked worktree，
//     MainWorkspaceRoot 直接回其 main working tree root，即容器根）。
//   - 失敗則對 basePaths 中「目錄實際存在」的 repo 路徑逐一試 MainWorkspaceRoot，取第一個成功者：涵蓋
//     multi-repo container（container 本身非 linked worktree，但其 scoped sub-repo 子目錄是），與
//     gitops.ScopeRoots 掃子目錄推導的策略一致。為讓迭代順序穩定、便於測試，先排序 keys 再迭代。
//     注意 MainWorkspaceRoot(<container>/<name>) 回的是該 sub-repo 在 main 內的 root（<main>/<rc.Path>），
//     非容器根；故需依該 sub-repo 相對 wsRoot 的層數往上剝除，還原容器根 <main>。
//
// 皆失敗回 ("", false)（非 git worktree / git 失敗 / 無可推導的存在路徑）；呼叫端據此走「無法驗證」分支。
func originWorkspaceRoot(wsRoot string, basePaths map[string]string) (string, bool) {
	if root, ok := gitops.MainWorkspaceRoot(wsRoot); ok {
		return root, true
	}
	names := make([]string, 0, len(basePaths))
	for name := range basePaths {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		p := basePaths[name]
		if info, err := os.Stat(p); err != nil || !info.IsDir() {
			continue
		}
		subRoot, ok := gitops.MainWorkspaceRoot(p)
		if !ok {
			continue
		}
		rel, err := filepath.Rel(wsRoot, p)
		if err != nil {
			continue
		}
		return trimPathSuffix(subRoot, rel), true
	}
	return "", false
}

// trimPathSuffix 從 path 尾端剝除 rel 所含的路徑層數，還原 sub-repo main root 對應的 workspace 容器根。
// rel 為空或 "." 時原樣回傳（無層可剝）。
func trimPathSuffix(path, rel string) string {
	rel = filepath.ToSlash(rel)
	if rel == "" || rel == "." {
		return path
	}
	for range strings.Split(rel, "/") {
		path = filepath.Dir(path)
	}
	return path
}

// resolveFanoutRepoPaths 解析可供 fan-out 掃描的 repo name → 真實路徑 map，處理「ws.Root 為
// worktree、部分 repo 未 provision 進 worktree」的可見度落差（見 F174）。
//
// 對 protocol.ResolveRepoPaths(cfg, wsRoot) 的每個 repo（依名稱排序穩定迭代）：
//   - 路徑在 wsRoot 下實際存在 → 直接納入（scoped repo 在 worktree，或 main-root / mono-repo 路徑）。
//   - 不存在但可經 origin(main) root 定位到存在目錄 → 以 origin 內路徑納入。
//   - 兩者皆不成立（未 provision 進 worktree 且無法定位 origin，或 origin 內也無此目錄）→ 不納入，
//     改 append 一條「無法驗證」Warn，提醒此 repo 需人工覆核，取代原本靜默略過、回零命中的行為。
//
// 回傳僅含可掃描的 repo。mono-repo（cfg.Workspace.Repos 為空）時 base 為 {".": wsRoot}，wsRoot 恆存在，
// 永遠走第一分支、不會觸發 origin 定位或 Warn。
func resolveFanoutRepoPaths(cfg protocol.Config, wsRoot string, r *CheckResult) map[string]string {
	base := protocol.ResolveRepoPaths(cfg, wsRoot)
	originRoot, originOK := originWorkspaceRoot(wsRoot, base)

	names := make([]string, 0, len(base))
	for name := range base {
		names = append(names, name)
	}
	sort.Strings(names)

	resolved := make(map[string]string, len(base))
	for _, name := range names {
		p := base[name]
		if info, err := os.Stat(p); err == nil && info.IsDir() {
			resolved[name] = p
			continue
		}
		if originOK {
			op := filepath.Join(originRoot, cfg.Workspace.Repos[name].Path)
			if info, err := os.Stat(op); err == nil && info.IsDir() {
				resolved[name] = op
				continue
			}
		}
		r.Warns = append(r.Warns, fmt.Sprintf(
			"proto-fanout 無法驗證：repo %q 未 provision 進 worktree 且無法定位 origin(main) workspace root，grep-based 掃描已略過此 repo——建議人工覆核是否有下游 import 本輪變更的 proto/interface 定義",
			name))
	}
	return resolved
}

// changedProtoInterfaceFiles 從 gitops.ChangedPaths(root)（uncommitted diff + untracked）
// 過濾出 proto/interface 定義檔的相對路徑清單。
func changedProtoInterfaceFiles(root string) []string {
	var out []string
	for _, p := range gitops.ChangedPaths(root) {
		if isProtoInterfaceDefinition(p) {
			out = append(out, p)
		}
	}
	return out
}

// isProtoInterfaceDefinition 判斷 path 是否為 proto/interface 定義檔（低風險路徑慣例，非 AST）：
//   - 副檔名為 `.proto` → 一律命中。
//   - 路徑任一 segment 含 `interface`（含 `interfaces`），且副檔名為
//     `.go` / `.ts` / `.tsx` / `.java` / `.kt` / `.json` / `.yaml` / `.yml` 之一 → 命中。
//
// 一般 `internal/foo/foo.go`（無 interface segment）不命中。
func isProtoInterfaceDefinition(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	if ext == ".proto" {
		return true
	}
	switch ext {
	case ".go", ".ts", ".tsx", ".java", ".kt", ".json", ".yaml", ".yml":
	default:
		return false
	}
	for _, seg := range strings.Split(filepath.ToSlash(path), "/") {
		if strings.Contains(strings.ToLower(seg), "interface") {
			return true
		}
	}
	return false
}

// importSearchTerms 以變更檔相對 repo/root 的 path 產生 grep 字串：
//   - 一律包含原始 path。
//   - 若 path 以 `<repo>/` 開頭，也包含去掉第一層前綴後的 path，讓 multi-repo 下游可
//     import `proto/foo.proto` 而不必寫 `shared/proto/foo.proto`。
func importSearchTerms(changedPath string) []string {
	p := filepath.ToSlash(changedPath)
	terms := []string{p}
	if i := strings.IndexByte(p, '/'); i >= 0 && i < len(p)-1 {
		if stripped := p[i+1:]; stripped != "" && stripped != p {
			terms = append(terms, stripped)
		}
	}
	return terms
}

// scanImportingRepos 掃描 workspace repo 內文字檔，找出內容含任一 term 的 repo。
// 回傳 repo 名稱 → 命中的 term 清單。刻意排除變更定義檔所在 repo 本身（changedRepo），
// 並跳過 .git / .4x / .worktrees 與常見 vendor/build 目錄（見 fanoutSkipDirs）。
func scanImportingRepos(repoPaths map[string]string, terms []string, changedRepo string) map[string][]string {
	result := make(map[string][]string)
	if len(terms) == 0 {
		return result
	}
	for repoName, repoPath := range repoPaths {
		if repoName == changedRepo {
			continue
		}
		if matched := scanRepoForTerms(repoPath, terms); len(matched) > 0 {
			result[repoName] = matched
		}
	}
	return result
}

// scanRepoForTerms 遞迴掃描 repoPath 內的文字檔，回傳命中的 term（已排序去重）。
// 跳過 fanoutSkipDirs 目錄、過大檔案與疑似二進位檔；任何讀取錯誤都靜默略過（best-effort）。
func scanRepoForTerms(repoPath string, terms []string) []string {
	matchedSet := make(map[string]bool)
	_ = filepath.WalkDir(repoPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if fanoutSkipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := d.Info()
		if err != nil || info.Size() > maxFanoutFileSize {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil || isProbablyBinary(data) {
			return nil
		}
		content := string(data)
		for _, t := range terms {
			if !matchedSet[t] && strings.Contains(content, t) {
				matchedSet[t] = true
			}
		}
		return nil
	})
	if len(matchedSet) == 0 {
		return nil
	}
	matched := make([]string, 0, len(matchedSet))
	for t := range matchedSet {
		matched = append(matched, t)
	}
	sort.Strings(matched)
	return matched
}

// isProbablyBinary 以前 8000 bytes 內是否含 NUL byte 粗略判斷檔案是否為二進位，
// 避免對 binary 做字串比對（既無意義又可能命中巧合位元組）。
func isProbablyBinary(data []byte) bool {
	n := len(data)
	if n > 8000 {
		n = 8000
	}
	for i := 0; i < n; i++ {
		if data[i] == 0 {
			return true
		}
	}
	return false
}
