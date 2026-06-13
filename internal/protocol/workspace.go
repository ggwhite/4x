package protocol

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	DirName          = ".4x"
	UserConfigDir    = ".4x"
	UserConfigFile   = "settings.json"
	BacklogFile      = "feature_list.json"
	ConfigFile       = "settings.json"
	FeaturesDir      = "features"
	StateFile        = "state.json"
	EventsFile       = "events.jsonl"
	BaselineFile     = "baseline.json"
	RoundsDir        = "rounds"
	FinalReport      = "final-report.md"
	CommitPlan       = "commit-plan.md"
	TaskBrief        = "task-brief.md"
	Criteria         = "acceptance-criteria.md"
	TestStratFile    = "test-strategy.yaml"
	ReviewReport     = "review-report.md"
	DeepReviewReport = "deep-review-report.md"
	CoderReport      = "coder-report.md"
	TestReport       = "test-report.md"
	VerifyFile       = "verify.json"
	EscalationFile   = "escalation.json"
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

// ProcessAlive 檢查 PID 是否仍在執行中
func ProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}

// ReconcileActive 以 process 存在為權威來源校正 Active 欄位。
// 若 state 記錄 Active=true 但 PID 已不存在，自動將 Active 設為 false 並回寫。
func (w *Workspace) ReconcileActive(featureID string, s *State) {
	if !s.Active {
		return
	}
	if ProcessAlive(s.Pid) {
		return
	}
	s.Active = false
	if s.StopReason == "" {
		s.StopReason = "process-gone"
	}
	s.Pid = 0
	_ = w.WriteState(featureID, *s)
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

// ScreenshotGroup 表示同一 round 的截圖清單。
type ScreenshotGroup struct {
	Round       int          `json:"round"`
	Screenshots []Screenshot `json:"screenshots"`
}

// DiscoverScreenshots 會合併 verify.json 與截圖目錄掃描結果，並按 round 分組回傳。
func (w *Workspace) DiscoverScreenshots(featureID, screenshotDir string) ([]ScreenshotGroup, error) {
	if screenshotDir == "" {
		screenshotDir = DefaultScreenshotDir
	}

	byRound := make(map[int][]Screenshot)
	seenPath := make(map[string]struct{})

	rounds, err := w.discoverFromVerify(featureID, byRound, seenPath)
	if err != nil {
		return nil, err
	}
	if err := w.discoverFromDir(featureID, screenshotDir, rounds, byRound, seenPath); err != nil {
		return nil, err
	}

	keys := make([]int, 0, len(byRound))
	for round, shots := range byRound {
		if len(shots) == 0 {
			continue
		}
		sortScreenshots(shots)
		byRound[round] = shots
		keys = append(keys, round)
	}
	sort.Ints(keys)

	groups := make([]ScreenshotGroup, 0, len(keys))
	for _, round := range keys {
		groups = append(groups, ScreenshotGroup{Round: round, Screenshots: byRound[round]})
	}
	return groups, nil
}

func (w *Workspace) discoverFromVerify(
	featureID string,
	byRound map[int][]Screenshot,
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
			path := NormalizeScreenshotPath(raw.Path)
			if path == "" {
				continue
			}
			if strings.HasPrefix(path, "../") || path == ".." {
				continue
			}
			if !IsScreenshotFile(filepath.Base(path)) {
				continue
			}
			if _, ok := seenPath[path]; ok {
				continue
			}
			shot := raw
			shot.Path = path
			if shot.Step == "" || shot.Description == "" {
				step, desc := parseScreenshotFilename(filepath.Base(path))
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
	byRound map[int][]Screenshot,
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
			if e.IsDir() || !IsScreenshotFile(e.Name()) {
				continue
			}
			absPath := filepath.Join(dirPath, e.Name())
			rel, err := filepath.Rel(w.DotDir(), absPath)
			if err != nil {
				continue
			}
			rel = filepath.ToSlash(rel)
			if strings.HasPrefix(rel, "../") || rel == ".." {
				continue
			}
			rel = NormalizeScreenshotPath(rel)
			if _, ok := seenPath[rel]; ok {
				continue
			}
			step, desc := parseScreenshotFilename(e.Name())
			shot := Screenshot{Path: rel, Step: step, Description: desc}
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
	return []screenshotScanTarget{{Round: 1, Dir: template}}
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

// IsScreenshotFile 判斷檔名是否為支援的截圖格式（png/jpg/jpeg/webp）。
func IsScreenshotFile(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	return ext == ".png" || ext == ".jpg" || ext == ".jpeg" || ext == ".webp"
}

// NormalizeScreenshotPath 將截圖路徑正規化，去除前綴 ./、.4x/，並 trim 空白。
func NormalizeScreenshotPath(path string) string {
	p := filepath.ToSlash(strings.TrimSpace(path))
	p = strings.TrimPrefix(p, "./")
	p = strings.TrimPrefix(p, ".4x/")
	return p
}

// ScreenshotDir 從 config 取得 tester 的截圖目錄，fallback 到 DefaultScreenshotDir。
func ScreenshotDir(cfg Config) string {
	if tester, ok := cfg.Roles[string(RoleTester)]; ok && strings.TrimSpace(tester.ScreenshotDir) != "" {
		return tester.ScreenshotDir
	}
	return DefaultScreenshotDir
}

func parseScreenshotFilename(filename string) (string, string) {
	base := strings.TrimSuffix(filename, filepath.Ext(filename))
	if base == "" {
		return "", ""
	}
	idx := strings.Index(base, "-")
	if idx <= 0 {
		return "", strings.ReplaceAll(base, "-", " ")
	}
	step := base[:idx]
	desc := strings.TrimSpace(strings.ReplaceAll(base[idx+1:], "-", " "))
	return step, desc
}

func sortScreenshots(items []Screenshot) {
	sort.Slice(items, func(i, j int) bool {
		leftN, leftOK := parseStepNumber(items[i].Step)
		rightN, rightOK := parseStepNumber(items[j].Step)
		if leftOK && rightOK && leftN != rightN {
			return leftN < rightN
		}
		if items[i].Step != items[j].Step {
			return items[i].Step < items[j].Step
		}
		return items[i].Path < items[j].Path
	})
}

func parseStepNumber(step string) (int, bool) {
	if step == "" {
		return 0, false
	}
	n, err := strconv.Atoi(step)
	if err != nil {
		return 0, false
	}
	return n, true
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
