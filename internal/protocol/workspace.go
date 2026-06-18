package protocol

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ggwhite/4x/internal/feature"
	"gopkg.in/yaml.v3"
)

// 編譯期斷言：Workspace 與 CachedWorkspace 必須滿足 feature.Store，
// 方法簽名一旦改動即在編譯期報錯，不延遲到呼叫端。
var (
	_ feature.Store = (*Workspace)(nil)
	_ feature.Store = (*CachedWorkspace)(nil)
)

const (
	DirName           = ".4x"
	UserConfigDir     = ".4x"
	UserConfigFile    = "settings.json"
	BacklogFile       = "feature_list.json"
	ConfigFile        = "settings.json"
	FeaturesDir       = "features"
	StateFile         = "state.json"
	EventsFile        = "events.jsonl"
	BaselineFile      = "baseline.json"
	RoundsDir         = "rounds"
	FinalReport       = "final-report.md"
	TaskBrief         = "task-brief.md"
	Criteria          = "acceptance-criteria.md"
	TestStratFile     = "test-strategy.yaml"
	ReviewReport      = "review-report.md"
	DeepReviewReport  = "deep-review-report.md"
	CoderReport       = "coder-report.md"
	TestReport        = "test-report.md"
	VerifyFile        = "verify.json"
	EscalationFile    = "escalation.json"
	StopFile          = "stop"
	BatchStopFile     = "batch-stop"
	BatchConflictFile = "batch-conflict.json"
	BatchReportFile   = "batch-report.json"
	BatchPIDFile      = "batch-pid"
)

// Workspace 管理 .4x/ 目錄的讀寫
type Workspace struct {
	Root string
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

	return WriteConfig(dotDir, cfg)
}

// DotDir 回傳 .4x/ 的完整路徑
func (w *Workspace) DotDir() string {
	return filepath.Join(w.Root, DirName)
}

// FeatureDir 回傳 .4x/{featureId}/ 的路徑
func (w *Workspace) FeatureDir(featureID string) string {
	return filepath.Join(w.DotDir(), featureID)
}

// RoundDir 回傳 .4x/{featureId}/rounds/round-{n}/ 的路徑
func (w *Workspace) RoundDir(featureID string, round int) string {
	return filepath.Join(w.FeatureDir(featureID), RoundsDir, fmt.Sprintf("round-%d", round))
}

// ReadConfig 讀取 .4x/settings.json
func (w *Workspace) ReadConfig() (Config, error) {
	return ReadConfig(w.DotDir())
}

// LoadMergedConfig 讀取 project config 並合併 user config，封裝散落各處的三行 boilerplate。
// project config 讀取失敗時回傳 error 由呼叫端決定如何處理；
// user config 讀取失敗時印 slog.Warn 但不中斷（沿用既有各站點的處理慣例）。
func (w *Workspace) LoadMergedConfig() (Config, error) {
	cfg, err := w.ReadConfig()
	if err != nil {
		return Config{}, err
	}
	if userCfg, err := ReadUserConfig(); err != nil {
		slog.Warn("failed to read user config", "error", err)
	} else {
		cfg = MergeConfig(userCfg, cfg)
	}
	return cfg, nil
}

// ResolveFeatureID 用前綴比對找出唯一 feature ID（大小寫不敏感）
func (w *Workspace) ResolveFeatureID(prefix string) (string, error) {
	dir := filepath.Join(w.DotDir(), FeaturesDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", fmt.Errorf("read features dir: %w", err)
	}
	lower := strings.ToLower(prefix)
	var matches []string
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".yaml" {
			continue
		}
		id := e.Name()[:len(e.Name())-5]
		if strings.ToLower(id) == lower {
			return id, nil
		}
		if strings.HasPrefix(strings.ToLower(id), lower) {
			matches = append(matches, id)
		}
	}
	switch len(matches) {
	case 0:
		return "", fmt.Errorf("no feature matching %q", prefix)
	case 1:
		return matches[0], nil
	default:
		return "", fmt.Errorf("ambiguous prefix %q matches: %s", prefix, strings.Join(matches, ", "))
	}
}

// LoadFeature 讀取 .4x/features/{id}.yaml
func (w *Workspace) LoadFeature(id string) (feature.Feature, error) {
	path := filepath.Join(w.DotDir(), FeaturesDir, id+".yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		if resolved := resolveFeatureFile(filepath.Join(w.DotDir(), FeaturesDir), id); resolved != "" {
			path = resolved
			data, err = os.ReadFile(path)
		}
		if err != nil {
			return feature.Feature{}, fmt.Errorf("read feature %s: %w", id, err)
		}
	}
	var f feature.Feature
	if err := yaml.Unmarshal(data, &f); err != nil {
		return feature.Feature{}, fmt.Errorf("parse feature %s: %w", id, err)
	}
	if err := f.Validate(); err != nil {
		return feature.Feature{}, fmt.Errorf("feature %s: %w", id, err)
	}
	return f, nil
}

// resolveFeatureFile 用短 ID（如 "F078"）在 features 目錄中找對應的 YAML 檔。
// 找 prefix 為 id+"-" 的檔案，有唯一匹配就回傳完整路徑，否則回空字串。
func resolveFeatureFile(dir, id string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	prefix := id + "-"
	var match string
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, prefix) && strings.HasSuffix(name, ".yaml") {
			if match != "" {
				return ""
			}
			match = filepath.Join(dir, name)
		}
	}
	return match
}

// ReadTestStrategy 讀取 .4x/{featureId}/test-strategy.yaml 並解析為 TestStrategy。
// 檔案不存在時回傳零值 TestStrategy 與 nil error，方便呼叫端把「沒設定」與「解析失敗」分開處理。
func (w *Workspace) ReadTestStrategy(featureID string) (TestStrategy, error) {
	path := filepath.Join(w.FeatureDir(featureID), TestStratFile)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return TestStrategy{}, nil
		}
		return TestStrategy{}, fmt.Errorf("read test-strategy %s: %w", featureID, err)
	}
	var ts TestStrategy
	if err := yaml.Unmarshal(data, &ts); err != nil {
		return TestStrategy{}, fmt.Errorf("parse test-strategy %s: %w", featureID, err)
	}
	return ts, nil
}

// ListFeatures 列出所有 feature。
// 使用寬鬆驗證：格式有問題的 YAML 仍會載入並附帶 Warnings，只有完全無法解析的才跳過。
func (w *Workspace) ListFeatures() ([]feature.Feature, error) {
	dir := filepath.Join(w.DotDir(), FeaturesDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var features []feature.Feature
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".yaml" {
			continue
		}
		id := e.Name()[:len(e.Name())-5]
		f, err := w.loadFeatureLoose(id)
		if err != nil {
			slog.Warn("skipped malformed feature YAML", "id", id, "error", err)
			continue
		}
		features = append(features, f)
	}
	sort.Slice(features, func(i, j int) bool {
		return features[i].ID < features[j].ID
	})
	return features, nil
}

// loadFeatureLoose 讀取 feature YAML 並以寬鬆模式驗證。
// 格式問題記錄在 Feature.Warnings，只有無法解析或缺 id 才回傳 error。
func (w *Workspace) loadFeatureLoose(id string) (feature.Feature, error) {
	path := filepath.Join(w.DotDir(), FeaturesDir, id+".yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		return feature.Feature{}, fmt.Errorf("read feature %s: %w", id, err)
	}
	var f feature.Feature
	if err := yaml.Unmarshal(data, &f); err != nil {
		return feature.Feature{}, fmt.Errorf("parse feature %s: %w", id, err)
	}
	warnings, fatalErr := f.ValidateLoose()
	if fatalErr != nil {
		return feature.Feature{}, fmt.Errorf("feature %s: %w", id, fatalErr)
	}
	if len(warnings) > 0 {
		f.Warnings = warnings
		slog.Warn("feature YAML has format issues", "id", id, "warnings", len(warnings))
	}
	return f, nil
}

// ReadBacklogMirror 讀取根目錄 feature_list.json；不存在時回傳 present=false。
func (w *Workspace) ReadBacklogMirror() (feature.BacklogMirror, bool, error) {
	path := filepath.Join(w.Root, BacklogFile)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return feature.BacklogMirror{}, false, nil
		}
		return feature.BacklogMirror{}, false, fmt.Errorf("read %s: %w", BacklogFile, err)
	}

	var mirror feature.BacklogMirror
	if err := json.Unmarshal(data, &mirror); err != nil {
		return feature.BacklogMirror{}, true, fmt.Errorf("parse %s: %w", BacklogFile, err)
	}
	return mirror, true, nil
}

// CompareBacklogMirror 比對 canonical feature YAML 與 legacy feature_list.json mirror。
func (w *Workspace) CompareBacklogMirror() ([]feature.BacklogDrift, error) {
	mirror, present, err := w.ReadBacklogMirror()
	if err != nil || !present {
		return nil, err
	}
	features, err := w.ListFeatures()
	if err != nil {
		return nil, err
	}
	return feature.CompareBacklogMirror(features, BacklogFile, mirror), nil
}

// SaveFeature 寫入 feature YAML，採用 write-to-temp + rename 確保原子性
func (w *Workspace) SaveFeature(f feature.Feature) error {
	dir := filepath.Join(w.DotDir(), FeaturesDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := yaml.Marshal(f)
	if err != nil {
		return err
	}

	tmp, err := os.CreateTemp(dir, ".feature-*.yaml")
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
	if err := os.Chmod(tmpPath, 0o644); err != nil {
		os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, filepath.Join(dir, f.ID+".yaml")); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return nil
}

// SyncFeatureStatus 將 feature YAML 的 Status 欄位同步為對應 phase 的狀態。
// 內部依序執行 LoadFeature → PhaseToStatus → SaveFeature，供 cmd/4x 與
// internal/server 共用，避免 status 同步邏輯分散兩處。
// 若 feature YAML 被 git 追蹤，會自動 commit 狀態變更。
func (w *Workspace) SyncFeatureStatus(featureID string, phase Phase) error {
	f, err := w.LoadFeature(featureID)
	if err != nil {
		return fmt.Errorf("sync feature status: load: %w", err)
	}
	oldStatus := f.Status
	f.Status = PhaseToStatus(phase)
	if f.Status == oldStatus {
		return nil
	}
	if err := w.SaveFeature(f); err != nil {
		return fmt.Errorf("sync feature status: save: %w", err)
	}
	w.autoCommitFeatureYAML(featureID, f.Status)
	return nil
}

// autoCommitFeatureYAML 在 feature YAML 被 git 追蹤時自動 commit 狀態變更。
// Best-effort：git 不存在、檔案未追蹤、或 commit 失敗都靜默略過。
func (w *Workspace) autoCommitFeatureYAML(featureID string, status feature.Status) {
	yamlPath := filepath.Join(FeaturesDir, featureID+".yaml")
	absPath := filepath.Join(w.DotDir(), yamlPath)

	out, err := exec.Command("git", "-C", w.Root, "ls-files", absPath).Output()
	if err != nil || len(strings.TrimSpace(string(out))) == 0 {
		return
	}

	if err := exec.Command("git", "-C", w.Root, "add", absPath).Run(); err != nil {
		return
	}
	msg := fmt.Sprintf("chore(%s): %s", featureID, status)
	_ = exec.Command("git", "-C", w.Root, "commit", "-m", msg, "--", absPath).Run()
}

// InitFeatureDir 建立 feature 的運行時目錄
func (w *Workspace) InitFeatureDir(featureID string) error {
	dir := w.FeatureDir(featureID)
	return os.MkdirAll(filepath.Join(dir, RoundsDir), 0o755)
}

// CleanCandidate 描述一個可清理的 feature workspace。
type CleanCandidate struct {
	FeatureID string
	Size      int64
}

// CleanableFeatures 列出所有可清理的 feature workspace。
// 僅納入 done 或 abandoned、有 workspace 目錄、且目前非 active 的 feature，
// 供 4x clean 與 POST /api/clean 在實際刪除前向使用者預覽清理對象與預估空間。
func (w *Workspace) CleanableFeatures() ([]CleanCandidate, error) {
	features, err := w.ListFeatures()
	if err != nil {
		return nil, err
	}

	var candidates []CleanCandidate
	for _, f := range features {
		if f.Status != feature.StatusDone && f.Status != feature.StatusAbandoned {
			continue
		}
		dir := w.FeatureDir(f.ID)
		info, err := os.Stat(dir)
		if err != nil || !info.IsDir() {
			continue
		}

		// 依規格：無 state.json（從未跑過）→ 跳過，不納入清理候選。
		s, err := w.ReadState(f.ID)
		if err != nil {
			continue
		}
		if err := w.ReconcileActive(f.ID, &s); err != nil {
			slog.Warn("failed to reconcile active state", "feature", f.ID, "error", err)
		}
		if s.Active {
			continue
		}

		candidates = append(candidates, CleanCandidate{FeatureID: f.ID, Size: dirSize(dir)})
	}
	return candidates, nil
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

// CleanFeature 刪除指定 feature 的 workspace 目錄，回傳釋放的位元組數。
// 僅允許 done 或 abandoned 且非 active 的 feature；非 terminal 或仍在執行中皆回 error。
// feature 定義（.4x/features/{id}.yaml）不受影響。
func (w *Workspace) CleanFeature(featureID string) (int64, error) {
	f, err := w.LoadFeature(featureID)
	if err != nil {
		return 0, fmt.Errorf("load feature %s: %w", featureID, err)
	}
	if f.Status != feature.StatusDone && f.Status != feature.StatusAbandoned {
		return 0, fmt.Errorf("feature %s is %s, only done/abandoned can be cleaned", featureID, f.Status)
	}

	dir := w.FeatureDir(featureID)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return 0, fmt.Errorf("no workspace directory for %s", featureID)
	}

	// 依規格：無 state.json 時無法確認 active 狀態，拒絕清理。
	s, err := w.ReadState(featureID)
	if err != nil {
		return 0, fmt.Errorf("cannot read state for %s, refusing to clean: %w", featureID, err)
	}
	if err := w.ReconcileActive(featureID, &s); err != nil {
		slog.Warn("failed to reconcile active state", "feature", featureID, "error", err)
	}
	if s.Active {
		return 0, fmt.Errorf("feature %s is still active (pid %d)", featureID, s.Pid)
	}

	size := dirSize(dir)
	if err := os.RemoveAll(dir); err != nil {
		return 0, fmt.Errorf("remove %s: %w", dir, err)
	}
	return size, nil
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

// ReadState 讀取 feature 的 state.json
func (w *Workspace) ReadState(featureID string) (State, error) {
	path := filepath.Join(w.FeatureDir(featureID), StateFile)
	data, err := os.ReadFile(path)
	if err != nil {
		return State{}, err
	}
	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return State{}, err
	}
	return s, nil
}

// ReconcileActive 以 process 存在為權威來源校正 Active 欄位。
// 若 state 記錄 Active=true 但 PID 已不存在，自動將 Active 設為 false 並回寫。
func (w *Workspace) ReconcileActive(featureID string, s *State) error {
	if !s.Active {
		return nil
	}
	if ProcessAlive(s.Pid) {
		return nil
	}
	s.Active = false
	if s.StopReason == "" {
		s.StopReason = "process-gone"
	}
	s.Pid = 0
	return w.WriteState(featureID, *s)
}

// WriteState 寫入 feature 的 state.json。
//
// 採用 write-to-temp + rename 而非直接 os.WriteFile：後者會先 truncate 再寫，
// 在這段時間內 concurrent 的 ReadState 可能讀到截斷或半寫的 JSON 而 Unmarshal 失敗。
// 改用同目錄 temp file 寫完再 atomic rename 覆蓋，讓讀者永遠看到完整的舊檔或完整的新檔。
func (w *Workspace) WriteState(featureID string, s State) error {
	s.UpdatedAt = time.Now()
	// SubPhase 僅在 deep-reviewing phase 內有意義；離開該 phase 時一律清空，
	// 作為各退出路徑的單一收斂點，避免殘留的 subPhase 誤導 dashboard 或 resume 推斷。
	if s.Phase != PhaseDeepReviewing {
		s.SubPhase = ""
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return atomicWriteFile(w.FeatureDir(featureID), StateFile, ".state-*.json", data, 0o644)
}

// atomicWriteFile 以「同目錄 temp file + os.Rename」原子寫入 finalName。
//
// 直接 os.WriteFile 會先 truncate 再寫，這段空窗期內 concurrent 的讀者可能讀到
// 截斷或半寫的內容而解析失敗。改用同目錄 temp file（tmpPattern 為 os.CreateTemp
// 的 pattern）寫完再 atomic rename 覆蓋，讓讀者永遠看到完整的舊檔或完整的新檔。
//
// temp file 必須與目標同目錄以確保位於同一 filesystem，os.Rename 才保證 atomic。
// payload 的結尾換行等差異由呼叫端在 data 內自行決定。
func atomicWriteFile(dir, finalName, tmpPattern string, data []byte, perm os.FileMode) error {
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

// AppendEvent 追加一行到 events.jsonl
func (w *Workspace) AppendEvent(featureID string, evt Event) error {
	if evt.Timestamp == "" {
		evt.Timestamp = time.Now().UTC().Format(time.RFC3339)
	}
	data, err := json.Marshal(evt)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(
		filepath.Join(w.FeatureDir(featureID), EventsFile),
		os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644,
	)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(data, '\n'))
	return err
}

// RequestStop 在 feature dir 下原子寫入 stop signal 檔，請求 run loop 停止該 feature。
//
// 採 signal file（對齊既有 BatchStopFile 機制）而非直接改寫 state.json：state.json
// 的唯一 writer 收斂為 run loop，外部（如 MCP stop）只下「請求停止」信號，避免兩個
// writer 競寫整份 state.json 而用過時快照覆蓋掉 loop 剛寫入的 phase／round 進度。
//
// 語意上 stop 為「請求」：若目標 feature 已無存活 loop，signal 不會被消費，
// 留待既有 ReconcileActive 在下次 ReadState 校正 Active。
func (w *Workspace) RequestStop(featureID string) error {
	return atomicWriteFile(w.FeatureDir(featureID), StopFile, ".stop-*", []byte("mcp-stop\n"), 0o644)
}

// StopRequested 回傳 feature dir 下是否存在 stop signal 檔。
func (w *Workspace) StopRequested(featureID string) bool {
	_, err := os.Stat(filepath.Join(w.FeatureDir(featureID), StopFile))
	return err == nil
}

// ClearStopSignal 刪除 feature 的 stop signal 檔；檔案不存在時不視為錯誤（比照 ClearBatchConflict）。
func (w *Workspace) ClearStopSignal(featureID string) error {
	err := os.Remove(filepath.Join(w.FeatureDir(featureID), StopFile))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// WriteBatchConflict 將 batch auto-merge 衝突信號寫入 .4x/batch-conflict.json，
// 供 dashboard 顯示衝突細節並提供 Continue Batch 操作。
func (w *Workspace) WriteBatchConflict(c BatchConflict) error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	// 原子寫入：dashboard 可能在寫入過程中 ReadBatchConflict，非原子的 os.WriteFile
	// 會讓讀者撞上截斷／半寫的 JSON 而 Unmarshal 失敗。
	return atomicWriteFile(w.DotDir(), BatchConflictFile, ".batch-conflict-*.json", append(data, '\n'), 0o644)
}

// ReadBatchConflict 讀取 .4x/batch-conflict.json；檔案不存在時回 (nil, nil) 代表目前無衝突。
func (w *Workspace) ReadBatchConflict() (*BatchConflict, error) {
	data, err := os.ReadFile(filepath.Join(w.DotDir(), BatchConflictFile))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var c BatchConflict
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, err
	}
	return &c, nil
}

// ClearBatchConflict 刪除 .4x/batch-conflict.json；檔案不存在時不視為錯誤。
func (w *Workspace) ClearBatchConflict() error {
	err := os.Remove(filepath.Join(w.DotDir(), BatchConflictFile))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// WriteBatchReport 原子寫入 .4x/batch-report.json（同目錄 temp file + os.Rename，仿 WriteState），
// 確保 dashboard 在 batch run 收尾／中斷的同時讀到的永遠是完整 JSON，不會撞上半寫狀態。
func (w *Workspace) WriteBatchReport(r BatchReport) error {
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return atomicWriteFile(w.DotDir(), BatchReportFile, ".batch-report-*.json", append(data, '\n'), 0o644)
}

// ReadBatchReport 讀取 .4x/batch-report.json；檔案不存在時回 (nil, nil) 代表尚無 batch 報告。
func (w *Workspace) ReadBatchReport() (*BatchReport, error) {
	data, err := os.ReadFile(filepath.Join(w.DotDir(), BatchReportFile))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var r BatchReport
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// WriteBatchPID 將 batch run 子程序的 PID 以十進位字串寫入 .4x/batch-pid，
// 供 server 重啟後辨識並 adopt 仍存活的孤兒子程序。
func (w *Workspace) WriteBatchPID(pid int) error {
	return os.WriteFile(filepath.Join(w.DotDir(), BatchPIDFile), []byte(strconv.Itoa(pid)), 0o644)
}

// ReadBatchPID 讀取 .4x/batch-pid 並解析為 PID；檔案不存在時回 (0, nil) 代表目前無 batch run 紀錄，
// 內容無法解析時回 (0, error)。
func (w *Workspace) ReadBatchPID() (int, error) {
	data, err := os.ReadFile(filepath.Join(w.DotDir(), BatchPIDFile))
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0, err
	}
	return pid, nil
}

// ClearBatchPID 刪除 .4x/batch-pid；檔案不存在時不視為錯誤。
func (w *Workspace) ClearBatchPID() error {
	err := os.Remove(filepath.Join(w.DotDir(), BatchPIDFile))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// ReadConfig 讀取 .4x/settings.json
func ReadConfig(dotDir string) (Config, error) {
	data, err := os.ReadFile(filepath.Join(dotDir, ConfigFile))
	if err != nil {
		return Config{}, err
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, err
	}
	if err := ValidateConfig(cfg); err != nil {
		return Config{}, fmt.Errorf("settings.json: %w", err)
	}
	return cfg, nil
}

// WriteConfig 寫入 .4x/settings.json
func WriteConfig(dotDir string, cfg Config) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dotDir, ConfigFile), append(data, '\n'), 0o644)
}

// UserConfigPath 回傳 ~/.4x/settings.json 的路徑
func UserConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, UserConfigDir, UserConfigFile), nil
}

// ReadUserConfig 讀取 ~/.4x/settings.json，不存在時回傳零值
func ReadUserConfig() (UserConfig, error) {
	path, err := UserConfigPath()
	if err != nil {
		return UserConfig{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return UserConfig{}, nil
		}
		return UserConfig{}, fmt.Errorf("read user config: %w", err)
	}
	var cfg UserConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return UserConfig{}, fmt.Errorf("parse user config: %w", err)
	}
	return cfg, nil
}

// DiscoverScreenshots 會合併 verify.json 與截圖目錄掃描結果，並按 round 分組回傳。
func (w *Workspace) DiscoverScreenshots(featureID, screenshotDir string) ([]feature.ScreenshotGroup, error) {
	if screenshotDir == "" {
		screenshotDir = feature.DefaultScreenshotDir
	}

	byRound := make(map[int][]feature.Screenshot)
	seenPath := make(map[string]struct{})

	rounds, err := w.discoverFromVerify(featureID, byRound, seenPath)
	if err != nil {
		return nil, err
	}
	if err := w.discoverFromDir(featureID, screenshotDir, rounds, byRound, seenPath); err != nil {
		return nil, err
	}
	if wtRoot := w.WorktreePath(featureID); wtRoot != "" && wtRoot != w.Root {
		w.discoverFromWorktree(wtRoot, featureID, screenshotDir, rounds, byRound, seenPath)
	}

	keys := make([]int, 0, len(byRound))
	for round, shots := range byRound {
		if len(shots) == 0 {
			continue
		}
		feature.SortScreenshots(shots)
		byRound[round] = shots
		keys = append(keys, round)
	}
	sort.Ints(keys)

	groups := make([]feature.ScreenshotGroup, 0, len(keys))
	for _, round := range keys {
		groups = append(groups, feature.ScreenshotGroup{Round: round, Screenshots: byRound[round]})
	}
	return groups, nil
}

func (w *Workspace) discoverFromVerify(
	featureID string,
	byRound map[int][]feature.Screenshot,
	seenPath map[string]struct{},
) ([]int, error) {
	roundsDir := filepath.Join(w.FeatureDir(featureID), RoundsDir)
	entries, err := os.ReadDir(roundsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read rounds dir: %w", err)
	}

	roundSet := make(map[int]struct{})
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), "round-") {
			continue
		}
		roundNum, err := strconv.Atoi(strings.TrimPrefix(e.Name(), "round-"))
		if err != nil || roundNum <= 0 {
			continue
		}
		roundSet[roundNum] = struct{}{}

		verifyPath := filepath.Join(roundsDir, e.Name(), VerifyFile)
		data, err := os.ReadFile(verifyPath)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("read %s: %w", verifyPath, err)
		}

		var evidence VerifyEvidence
		if err := json.Unmarshal(data, &evidence); err != nil {
			return nil, fmt.Errorf("parse %s: %w", verifyPath, err)
		}
		for _, raw := range evidence.Screenshots {
			path := feature.NormalizeScreenshotPath(raw.Path)
			if path == "" {
				continue
			}
			if strings.HasPrefix(path, "../") || path == ".." {
				continue
			}
			if !feature.IsScreenshotFile(filepath.Base(path)) {
				continue
			}
			if _, ok := seenPath[path]; ok {
				continue
			}
			shot := raw
			shot.Path = path
			if shot.Step == "" || shot.Description == "" {
				step, desc := feature.ParseScreenshotFilename(filepath.Base(path))
				if shot.Step == "" {
					shot.Step = step
				}
				if shot.Description == "" {
					shot.Description = desc
				}
			}

			byRound[roundNum] = append(byRound[roundNum], shot)
			seenPath[path] = struct{}{}
		}
	}

	rounds := make([]int, 0, len(roundSet))
	for round := range roundSet {
		rounds = append(rounds, round)
	}
	sort.Ints(rounds)
	return rounds, nil
}

func (w *Workspace) discoverFromDir(
	featureID, screenshotDir string,
	rounds []int,
	byRound map[int][]feature.Screenshot,
	seenPath map[string]struct{},
) error {
	targets := resolveScreenshotDirs(w.Root, featureID, screenshotDir, rounds)
	for _, target := range targets {
		dirPath := target.Dir
		if !filepath.IsAbs(dirPath) {
			dirPath = filepath.Join(w.Root, dirPath)
		}
		entries, err := os.ReadDir(dirPath)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return fmt.Errorf("read screenshot dir %s: %w", dirPath, err)
		}

		for _, e := range entries {
			if e.IsDir() || !feature.IsScreenshotFile(e.Name()) {
				continue
			}
			absPath := filepath.Join(dirPath, e.Name())
			rel, err := filepath.Rel(w.Root, absPath)
			if err != nil {
				continue
			}
			rel = filepath.ToSlash(rel)
			if strings.HasPrefix(rel, "../") || rel == ".." {
				continue
			}
			rel = feature.NormalizeScreenshotPath(rel)
			if _, ok := seenPath[rel]; ok {
				continue
			}
			step, desc := feature.ParseScreenshotFilename(e.Name())
			shot := feature.Screenshot{Path: rel, Step: step, Description: desc}
			byRound[target.Round] = append(byRound[target.Round], shot)
			seenPath[rel] = struct{}{}
		}
	}
	return nil
}

type screenshotScanTarget struct {
	Round int
	Dir   string
}

func resolveScreenshotDirs(workspaceRoot, featureID, screenshotDir string, rounds []int) []screenshotScanTarget {
	template := strings.ReplaceAll(screenshotDir, "{feature-id}", featureID)
	if strings.Contains(template, "{round}") {
		// 合併 verify.json rounds 與目錄掃描 rounds（聯集），避免任一來源不完整時漏掃
		roundSet := make(map[int]struct{})
		for _, r := range rounds {
			if r > 0 {
				roundSet[r] = struct{}{}
			}
		}
		for _, r := range discoverRoundsFromTemplate(workspaceRoot, template) {
			roundSet[r] = struct{}{}
		}
		candidates := make([]int, 0, len(roundSet))
		for r := range roundSet {
			candidates = append(candidates, r)
		}
		if len(candidates) == 0 {
			candidates = []int{1}
		}
		sort.Ints(candidates)
		targets := make([]screenshotScanTarget, 0, len(candidates))
		for _, round := range candidates {
			dir := strings.ReplaceAll(template, "{round}", strconv.Itoa(round))
			targets = append(targets, screenshotScanTarget{Round: round, Dir: dir})
		}
		return targets
	}
	round := 1
	if len(rounds) > 0 {
		round = rounds[len(rounds)-1]
	}
	return []screenshotScanTarget{{Round: round, Dir: template}}
}

func discoverRoundsFromTemplate(workspaceRoot, template string) []int {
	absTemplate := strings.TrimSuffix(filepath.ToSlash(template), "/")
	if !filepath.IsAbs(absTemplate) {
		absTemplate = filepath.ToSlash(filepath.Join(workspaceRoot, absTemplate))
	}
	idx := strings.Index(absTemplate, "{round}")
	if idx < 0 {
		return nil
	}

	pattern := strings.ReplaceAll(absTemplate, "{round}", "*")
	matches, err := filepath.Glob(filepath.FromSlash(pattern))
	if err != nil {
		return nil
	}

	prefix := absTemplate[:idx]
	suffix := absTemplate[idx+len("{round}"):]
	roundSet := make(map[int]struct{})
	for _, match := range matches {
		candidate := strings.TrimSuffix(filepath.ToSlash(match), "/")
		if !strings.HasPrefix(candidate, prefix) || !strings.HasSuffix(candidate, suffix) {
			continue
		}
		middle := strings.TrimSuffix(strings.TrimPrefix(candidate, prefix), suffix)
		if round, err := strconv.Atoi(middle); err == nil && round > 0 {
			roundSet[round] = struct{}{}
		}
	}
	if len(roundSet) == 0 {
		return nil
	}

	rounds := make([]int, 0, len(roundSet))
	for round := range roundSet {
		rounds = append(rounds, round)
	}
	sort.Ints(rounds)
	return rounds
}

// WorktreePath 從 events.jsonl 解析 feature 的 worktree 路徑。
// 若 feature 未使用 worktree 或 events.jsonl 不存在，回傳空字串。
func (w *Workspace) WorktreePath(featureID string) string {
	eventsPath := filepath.Join(w.FeatureDir(featureID), EventsFile)
	data, err := os.ReadFile(eventsPath)
	if err != nil {
		return ""
	}
	for _, line := range strings.SplitN(string(data), "\n", 5) {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var ev struct {
			Type   string `json:"type"`
			Detail string `json:"detail"`
		}
		if json.Unmarshal([]byte(line), &ev) != nil {
			continue
		}
		if ev.Type == "run-output" && strings.HasPrefix(ev.Detail, "worktree: ") {
			return strings.TrimPrefix(ev.Detail, "worktree: ")
		}
	}
	return ""
}

func (w *Workspace) discoverFromWorktree(
	wtRoot, featureID, screenshotDir string,
	rounds []int,
	byRound map[int][]feature.Screenshot,
	seenPath map[string]struct{},
) {
	targets := resolveScreenshotDirs(wtRoot, featureID, screenshotDir, rounds)
	for _, target := range targets {
		dirPath := target.Dir
		if !filepath.IsAbs(dirPath) {
			dirPath = filepath.Join(wtRoot, dirPath)
		}
		entries, err := os.ReadDir(dirPath)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || !feature.IsScreenshotFile(e.Name()) {
				continue
			}
			absPath := filepath.Join(dirPath, e.Name())
			rel, err := filepath.Rel(w.Root, absPath)
			if err != nil || strings.HasPrefix(rel, "..") {
				continue
			}
			rel = filepath.ToSlash(rel)
			normalized := feature.NormalizeScreenshotPath(rel)
			if _, seen := seenPath[normalized]; seen {
				continue
			}
			seenPath[normalized] = struct{}{}
			step, desc := feature.ParseScreenshotFilename(e.Name())
			byRound[target.Round] = append(byRound[target.Round], feature.Screenshot{
				Path:        rel,
				Step:        step,
				Description: desc,
			})
		}
	}
}

// ScreenshotDir 從 config 取得 tester 的截圖目錄，fallback 到 feature.DefaultScreenshotDir。
func ScreenshotDir(cfg Config) string {
	if tester, ok := cfg.Roles[string(RoleTester)]; ok && strings.TrimSpace(tester.ScreenshotDir) != "" {
		return tester.ScreenshotDir
	}
	return feature.DefaultScreenshotDir
}

// WriteUserConfig 寫入 ~/.4x/settings.json
func WriteUserConfig(cfg UserConfig) error {
	path, err := UserConfigPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}
