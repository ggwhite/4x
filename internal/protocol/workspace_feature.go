package protocol

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ggwhite/4x/internal/feature"
	"gopkg.in/yaml.v3"
)

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
		slog.Warn("feature YAML has format issues", "id", id, "warnings", strings.Join(warnings, "; "))
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
	if w.SkipAutoCommit {
		return
	}
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
	if err := exec.Command("git", "-C", w.Root, "commit", "-m", msg, "--", absPath).Run(); err != nil {
		slog.Warn("git auto-commit failed", "feature", featureID, "path", absPath, "error", err)
	}
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
