package protocol

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ggwhite/4x/internal/feature"
)

// 編譯期斷言：Workspace 與 CachedWorkspace 必須滿足 feature.Store，
// 方法簽名一旦改動即在編譯期報錯，不延遲到呼叫端。
var (
	_ feature.Store = (*Workspace)(nil)
	_ feature.Store = (*CachedWorkspace)(nil)
)

const (
	DirName        = ".4x"
	UserConfigDir  = ".4x"
	UserConfigFile = "settings.json"
	BacklogFile    = "feature_list.json"
	ConfigFile     = "settings.json"
	FeaturesDir    = "features"
	StateFile      = "state.json"
	EventsFile     = "events.jsonl"
	BaselineFile   = "baseline.json"
	RoundsDir      = "rounds"
	// DesignRoundsDir 存放 designer/design-reviewer 循環（design-reviewing FAIL 打回
	// designing）每一輪的 task-brief.md／acceptance-criteria.md／design-review-report.md
	// 快照，避免這些固定檔名的 artifact 被下一輪覆寫、dashboard message 區看不到歷史。
	DesignRoundsDir    = "design-rounds"
	FinalReport        = "final-report.md"
	TaskBrief          = "task-brief.md"
	Criteria           = "acceptance-criteria.md"
	TestStratFile      = "test-strategy.yaml"
	DesignReviewReport = "design-review-report.md"
	ReviewReport       = "review-report.md"
	DeepReviewReport   = "deep-review-report.md"
	CoderReport        = "coder-report.md"
	FixerReport        = "fixer-report.md"
	TestReport         = "test-report.md"
	VerifyFile         = "verify.json"
	BuildGateFile      = "build-gate.json"
	DocsGateFile       = "docs-gate.json"
	GuardFeedback      = "guard-feedback.json"
	EscalationFile     = "escalation.json"
	StopFile           = "stop"

	// CandidatesFile 是 .4x/candidates.json，history miner（4x mine）掃描歷史失敗訊號後
	// 產出的 candidate pool（候選 feature + 候選 learnings），供後續 F097 閘門決定是否升級。
	RunDir         = "run"
	CandidatesFile = "candidates.json"

	// F097 evolve 價值閘門相關檔名（皆在 .4x/ 根，CandidatePool 格式）。
	GateInputFile          = "gate-input.json"          // PRE-veto 倖存者，供 gate role 讀
	GateVerdictsFile       = "gate-verdicts.json"       // gate role 產出的價值判斷
	AcceptedCandidatesFile = "accepted-candidates.json" // POST-veto 通過者，附 value_score/why_not_hack

	// F099 evolve driver 相關檔名（皆在 .4x/ 根，非 per-feature）。
	EvolveReportFile = "evolve-report.md"  // 每輪 evolve pipeline 的摘要報告（dashboard surface）
	EvolveStateFile  = "evolve-state.json" // anti-spin 防空轉跨呼叫持久化計數

	// retro learnings 相關檔名。
	LearningsFile         = "learnings.json"          // .4x/learnings.json，跨 feature 累積的 learnings 庫
	LearningsContextFile  = "learnings-context.md"    // .4x/learnings-context.md，active learnings 的 markdown snapshot
	RetroLearningsFile    = "retro-learnings.json"    // .4x/{feature-id}/retro-learnings.json，Acceptor 產出
	RoleLearningsFileName = "role-learnings.json"     // 舊版單一固定檔名，harvester 仍讀取以相容既有 run 目錄
	RoleLearningsGlob     = "*-learnings.json"        // rounds/round-{n}/{role}-learnings.json，各角色獨立檔名的 glob
	ConsolidateInputFile  = "consolidate-input.json"  // .4x/consolidate-input.json，供 consolidate runner 讀
	ConsolidateResultFile = "consolidate-result.json" // .4x/consolidate-result.json，consolidate runner 產出

	// RoundsSummaryFile 是舊輪次摘要，在 round ≥ 3 進入 accepting 前由 round-summarizer 產出。
	// Acceptor 讀此檔取代讀取所有輪次全文，節省 context。
	RoundsSummaryFile = "rounds-summary.md"

	// DeepReviewAnglesFile 是 deep review 角度選擇結果的 artifact，記錄本次選了哪些角度及原因。
	DeepReviewAnglesFile = "deep-review-angles.json"

	// SharedPathsBaselineFile 是 shared_paths 的快照基線檔，記錄 4x 首次觀測到宣告時
	// 主工作區各宣告路徑的內容 hash，供 merge preflight 與 4x check 的反向污染偵測比對。
	// 路徑由 gitops.SharedPathsBaselineFile(mainRoot, featureID) 組出。
	SharedPathsBaselineFile = "shared-paths-baseline.json"

	// ReviewPackage 是 orchestrator 在 coding/amending → reviewing 轉換時預算的 diff 彙整檔
	// （commits/file-stat/full-diff），供 reviewer/deep-reviewer 讀檔取代自跑 git diff/log。
	ReviewPackage = "review-package.md"

	// AcceptanceSummaryFile 是 orchestrator 在進 accepting 前彙整各報告產出的檔案，
	// 供 Acceptor 讀取取代重複讀取 review/test/deep-review report 全文。
	AcceptanceSummaryFile = "acceptance-summary.md"
)

// Workspace 管理 .4x/ 目錄的讀寫
type Workspace struct {
	Root string
	// SkipAutoCommit 為 true 時，SyncFeatureStatus 只寫入檔案不自動 commit。
	// worktree 模式下主 repo 不應 commit feature YAML status，避免與 branch merge 衝突。
	SkipAutoCommit bool
}

// Find 從目前目錄往上找 .4x/ 目錄
func Find(startDir string) (*Workspace, error) {
	dir := startDir
	for {
		candidate := filepath.Join(dir, DirName)
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return &Workspace{Root: dir}, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return nil, fmt.Errorf("no %s directory found (walked up from %s)", DirName, startDir)
		}
		dir = parent
	}
}

// Init 建立 .4x/ 目錄和初始 config
func Init(root string, cfg Config) error {
	dotDir := filepath.Join(root, DirName)
	if err := os.MkdirAll(filepath.Join(dotDir, FeaturesDir), 0o755); err != nil {
		return fmt.Errorf("create .4x/: %w", err)
	}

	if err := writeGitignore(dotDir); err != nil {
		return err
	}

	return WriteConfig(dotDir, cfg)
}

// writeGitignore 在 .4x/ 目錄內建立 .gitignore，排除 runtime artifacts。
func writeGitignore(dotDir string) error {
	const content = `# Runtime artifacts (per-feature state, logs, reports)
run/

# Evolve pipeline intermediate files
gate-input.json
gate-verdicts.json
evolve-state.json

# Learnings consolidation intermediate files
consolidate-input.json
consolidate-result.json
consolidate.log
consolidate.stream.jsonl
`
	return os.WriteFile(filepath.Join(dotDir, ".gitignore"), []byte(content), 0o644)
}

// DotDir 回傳 .4x/ 的完整路徑
func (w *Workspace) DotDir() string {
	return filepath.Join(w.Root, DirName)
}

// FeatureDir 回傳 .4x/run/{featureId}/ 的路徑
func (w *Workspace) FeatureDir(featureID string) string {
	return filepath.Join(w.DotDir(), RunDir, featureID)
}

// RoundDir 回傳 .4x/{featureId}/rounds/round-{n}/ 的路徑
func (w *Workspace) RoundDir(featureID string, round int) string {
	return filepath.Join(w.FeatureDir(featureID), RoundsDir, fmt.Sprintf("round-%d", round))
}

// InitFeatureDir 建立 feature 的運行時目錄
func (w *Workspace) InitFeatureDir(featureID string) error {
	dir := w.FeatureDir(featureID)
	return os.MkdirAll(filepath.Join(dir, RoundsDir), 0o755)
}

// AtomicWriteFile 以「同目錄 temp file + os.Rename」原子寫入 finalName。
//
// 直接 os.WriteFile 會先 truncate 再寫，這段空窗期內 concurrent 的讀者可能讀到
// 截斷或半寫的內容而解析失敗。改用同目錄 temp file（tmpPattern 為 os.CreateTemp
// 的 pattern）寫完再 atomic rename 覆蓋，讓讀者永遠看到完整的舊檔或完整的新檔。
//
// temp file 必須與目標同目錄以確保位於同一 filesystem，os.Rename 才保證 atomic。
// payload 的結尾換行等差異由呼叫端在 data 內自行決定。
func AtomicWriteFile(dir, finalName, tmpPattern string, data []byte, perm os.FileMode) error {
	tmp, err := os.CreateTemp(dir, tmpPattern)
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	if err := os.Chmod(tmpPath, perm); err != nil {
		os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, filepath.Join(dir, finalName)); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return nil
}

// HumanSize 將位元組數轉為人類可讀格式（如 "3.9M"、"124K"、"512B"）。
func HumanSize(bytes int64) string {
	const (
		kb = 1024
		mb = 1024 * kb
		gb = 1024 * mb
	)
	switch {
	case bytes >= gb:
		return fmt.Sprintf("%.1fG", float64(bytes)/float64(gb))
	case bytes >= mb:
		return fmt.Sprintf("%.1fM", float64(bytes)/float64(mb))
	case bytes >= kb:
		return fmt.Sprintf("%.0fK", float64(bytes)/float64(kb))
	default:
		return fmt.Sprintf("%dB", bytes)
	}
}

// dirSize 累加 path 底下所有檔案的位元組數；遇到讀取錯誤的子項目跳過不中斷。
func dirSize(path string) int64 {
	var total int64
	filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			total += info.Size()
		}
		return nil
	})
	return total
}

// readJSON 讀取 JSON 檔案並反序列化為型別 T，所有錯誤（含檔案不存在）直接回傳。
func readJSON[T any](path string) (T, error) {
	var zero T
	data, err := os.ReadFile(path)
	if err != nil {
		return zero, err
	}
	var v T
	if err := json.Unmarshal(data, &v); err != nil {
		return zero, err
	}
	return v, nil
}

// writeJSON 將值以 indented JSON 寫入指定路徑（附結尾換行）。
func writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

// clearFile 刪除指定檔案；檔案不存在時不視為錯誤。
func clearFile(path string) error {
	err := os.Remove(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
