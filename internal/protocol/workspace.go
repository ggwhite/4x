package protocol

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	DirName        = ".4x"
	UserConfigDir  = "4x"
	UserConfigFile = "config.yaml"
	BacklogFile    = "feature_list.json"
	ConfigFile     = "config.yaml"
	FeaturesDir    = "features"
	StateFile      = "state.json"
	EventsFile     = "events.jsonl"
	BaselineFile   = "baseline.json"
	RoundsDir      = "rounds"
	FinalReport    = "final-report.md"
	CommitPlan     = "commit-plan.md"
	TaskBrief      = "task-brief.md"
	Criteria       = "acceptance-criteria.md"
	TestStratFile  = "test-strategy.yaml"
	ReviewReport   = "review-report.md"
	CoderReport    = "coder-report.md"
	TestReport     = "test-report.md"
	VerifyFile     = "verify.json"
	EscalationFile = "escalation.json"
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

// ReadConfig 讀取 .4x/config.yaml
func (w *Workspace) ReadConfig() (Config, error) {
	return ReadConfig(w.DotDir())
}

// LoadFeature 讀取 .4x/features/{id}.yaml
func (w *Workspace) LoadFeature(id string) (Feature, error) {
	path := filepath.Join(w.DotDir(), FeaturesDir, id+".yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		return Feature{}, fmt.Errorf("read feature %s: %w", id, err)
	}
	var f Feature
	if err := yaml.Unmarshal(data, &f); err != nil {
		return Feature{}, fmt.Errorf("parse feature %s: %w", id, err)
	}
	return f, nil
}

// ListFeatures 列出所有 feature
func (w *Workspace) ListFeatures() ([]Feature, error) {
	dir := filepath.Join(w.DotDir(), FeaturesDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var features []Feature
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".yaml" {
			continue
		}
		id := e.Name()[:len(e.Name())-5]
		f, err := w.LoadFeature(id)
		if err != nil {
			continue
		}
		features = append(features, f)
	}
	sort.Slice(features, func(i, j int) bool {
		return features[i].ID < features[j].ID
	})
	return features, nil
}

// ReadBacklogMirror 讀取根目錄 feature_list.json；不存在時回傳 present=false。
func (w *Workspace) ReadBacklogMirror() (BacklogMirror, bool, error) {
	path := filepath.Join(w.Root, BacklogFile)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return BacklogMirror{}, false, nil
		}
		return BacklogMirror{}, false, fmt.Errorf("read %s: %w", BacklogFile, err)
	}

	var mirror BacklogMirror
	if err := json.Unmarshal(data, &mirror); err != nil {
		return BacklogMirror{}, true, fmt.Errorf("parse %s: %w", BacklogFile, err)
	}
	return mirror, true, nil
}

// CompareBacklogMirror 比對 canonical feature YAML 與 legacy feature_list.json mirror。
func (w *Workspace) CompareBacklogMirror() ([]BacklogDrift, error) {
	mirror, present, err := w.ReadBacklogMirror()
	if err != nil || !present {
		return nil, err
	}
	features, err := w.ListFeatures()
	if err != nil {
		return nil, err
	}
	return CompareBacklogMirror(features, mirror), nil
}

// CompareBacklogMirror 比對 feature YAML 清單與 legacy backlog mirror，並以 feature ID 穩定排序。
func CompareBacklogMirror(features []Feature, mirror BacklogMirror) []BacklogDrift {
	canonical := make(map[string]Feature, len(features))
	for _, f := range features {
		canonical[f.ID] = f
	}

	backlog := make(map[string]BacklogFeature, len(mirror.Features))
	for _, f := range mirror.Features {
		backlog[f.ID] = f
	}

	var drift []BacklogDrift
	for _, f := range features {
		entry, ok := backlog[f.ID]
		if !ok {
			drift = append(drift, BacklogDrift{
				Kind:      BacklogDriftMissing,
				FeatureID: f.ID,
				Message:   fmt.Sprintf("%s missing entry for feature %q", BacklogFile, f.ID),
			})
			continue
		}
		drift = appendFieldDrift(drift, f.ID, "name", f.Name, entry.Name)
		drift = appendFieldDrift(drift, f.ID, "description", f.Description, entry.Description)
		drift = appendFieldDrift(drift, f.ID, "status", f.Status, entry.Status)
		drift = appendPriorityDrift(drift, f, entry)
	}

	for _, entry := range mirror.Features {
		if _, ok := canonical[entry.ID]; !ok {
			drift = append(drift, BacklogDrift{
				Kind:      BacklogDriftExtra,
				FeatureID: entry.ID,
				Message:   fmt.Sprintf("%s has extra entry %q", BacklogFile, entry.ID),
			})
		}
	}

	sort.Slice(drift, func(i, j int) bool {
		if drift[i].FeatureID != drift[j].FeatureID {
			return drift[i].FeatureID < drift[j].FeatureID
		}
		if drift[i].Kind != drift[j].Kind {
			return drift[i].Kind < drift[j].Kind
		}
		return drift[i].Field < drift[j].Field
	})
	return drift
}

func appendFieldDrift(drift []BacklogDrift, featureID, field, canonical, mirror string) []BacklogDrift {
	if canonical == mirror {
		return drift
	}
	return append(drift, BacklogDrift{
		Kind:      BacklogDriftMismatch,
		FeatureID: featureID,
		Field:     field,
		Canonical: canonical,
		Mirror:    mirror,
		Message: fmt.Sprintf(
			"%s mismatch for feature %q field %q: canonical %q, mirror %q",
			BacklogFile,
			featureID,
			field,
			canonical,
			mirror,
		),
	})
}

func appendPriorityDrift(drift []BacklogDrift, feature Feature, mirror BacklogFeature) []BacklogDrift {
	if feature.Priority == 0 && mirror.Priority == nil {
		return drift
	}
	canonical := strconv.Itoa(feature.Priority)
	if mirror.Priority == nil {
		return appendFieldDrift(drift, feature.ID, "priority", canonical, "")
	}
	return appendFieldDrift(drift, feature.ID, "priority", canonical, strconv.Itoa(*mirror.Priority))
}

// SaveFeature 寫入 feature YAML
func (w *Workspace) SaveFeature(f Feature) error {
	dir := filepath.Join(w.DotDir(), FeaturesDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := yaml.Marshal(f)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, f.ID+".yaml"), data, 0o644)
}

// InitFeatureDir 建立 feature 的運行時目錄
func (w *Workspace) InitFeatureDir(featureID string) error {
	dir := w.FeatureDir(featureID)
	return os.MkdirAll(filepath.Join(dir, RoundsDir), 0o755)
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

// WriteState 寫入 feature 的 state.json
func (w *Workspace) WriteState(featureID string, s State) error {
	s.UpdatedAt = time.Now()
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(w.FeatureDir(featureID), StateFile), data, 0o644)
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

// ReadConfig 讀取 config.yaml
func ReadConfig(dotDir string) (Config, error) {
	data, err := os.ReadFile(filepath.Join(dotDir, ConfigFile))
	if err != nil {
		return Config{}, err
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// WriteConfig 寫入 config.yaml
func WriteConfig(dotDir string, cfg Config) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dotDir, ConfigFile), data, 0o644)
}

// UserConfigPath 回傳 ~/.config/4x/config.yaml 的路徑
func UserConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", UserConfigDir, UserConfigFile), nil
}

// ReadUserConfig 讀取 ~/.config/4x/config.yaml，不存在時回傳零值
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
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return UserConfig{}, fmt.Errorf("parse user config: %w", err)
	}
	return cfg, nil
}

// WriteUserConfig 寫入 ~/.config/4x/config.yaml
func WriteUserConfig(cfg UserConfig) error {
	path, err := UserConfigPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
