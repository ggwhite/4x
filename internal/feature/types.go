package feature

import "strings"

// Status 表示 feature 的狀態，對應 Phase 但面向 dashboard 與人類可讀
type Status string

const (
	StatusNotStarted     Status = "not-started"
	StatusInProgress     Status = "in-progress"
	StatusDone           Status = "done"
	StatusAbandoned      Status = "abandoned"
	StatusBlocked        Status = "blocked"
	StatusNeedsAttention Status = "needs-attention"
	StatusReadyForReview Status = "ready-for-review"
)

// BatchCompleted 判斷 feature 的最終狀態是否視為 batch 已完成。
// done / abandoned / ready-for-review 三者皆算完成（與 cmd/4x batch 排程語義一致），
// 抽到 feature 供 batch report 與排程共用，避免兩處判定漂移。
func BatchCompleted(s Status) bool {
	return s == StatusDone || s == StatusAbandoned || s == StatusReadyForReview
}

// HookEntry 描述一個 phase hook 的 shell command 與失敗策略。
// 失敗策略 on_fail 未設定時預設為 "block"（中止 phase 轉換）；
// 設為 "warn" 則只記錄警告，不中止流程。
type HookEntry struct {
	Run    string `json:"run" yaml:"run"`
	OnFail string `json:"on_fail,omitempty" yaml:"on_fail,omitempty"`
}

// EffectiveOnFail 回傳實際的失敗策略，未設定時預設 "block"。
func (h HookEntry) EffectiveOnFail() string {
	if h.OnFail == "" {
		return "block"
	}
	return strings.ToLower(h.OnFail)
}

// Feature 是 features/*.yaml 的結構
type Feature struct {
	ID          string                 `yaml:"id" json:"id"`
	Name        string                 `yaml:"name" json:"name"`
	Description string                 `yaml:"description" json:"description"`
	Status      Status                 `yaml:"status" json:"status"`
	Priority    *int                   `yaml:"priority,omitempty" json:"priority,omitempty"`
	Repos       []string               `yaml:"repos,omitempty" json:"repos,omitempty"`
	Subtasks    []Subtask              `yaml:"subtasks,omitempty" json:"subtasks,omitempty"`
	Rules       []string               `yaml:"rules,omitempty" json:"rules,omitempty"`
	Depends     []string               `yaml:"depends,omitempty" json:"depends,omitempty"`
	Spec        string                 `yaml:"spec,omitempty" json:"-"`
	Plan        string                 `yaml:"plan,omitempty" json:"-"`
	Hooks       map[string][]HookEntry `yaml:"hooks,omitempty" json:"hooks,omitempty"`
}

// Subtask 是 feature 內的子任務
type Subtask struct {
	ID          string   `yaml:"id" json:"id"`
	Name        string   `yaml:"name" json:"name"`
	Status      string   `yaml:"status" json:"status"`
	Description string   `yaml:"description,omitempty" json:"description,omitempty"`
	Depends     []string `yaml:"depends,omitempty" json:"depends,omitempty"`
}

// BacklogMirror 是根目錄 feature_list.json 的 legacy mirror 結構。
type BacklogMirror struct {
	Version  int              `json:"version"`
	Features []BacklogFeature `json:"features"`
}

// BacklogFeature 表示 feature_list.json 中單一 legacy backlog entry。
type BacklogFeature struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Status      string `json:"status"`
	Area        string `json:"area,omitempty"`
	Description string `json:"description,omitempty"`
	Priority    *int   `json:"priority,omitempty"`
}

// BacklogDriftKind 表示 feature_list.json 與 .4x/features/*.yaml 的差異類型。
type BacklogDriftKind string

const (
	BacklogDriftMissing  BacklogDriftKind = "missing"
	BacklogDriftExtra    BacklogDriftKind = "extra"
	BacklogDriftMismatch BacklogDriftKind = "mismatch"
)

// BacklogDrift 表示一筆 feature_list.json legacy mirror 漂移結果。
type BacklogDrift struct {
	Kind      BacklogDriftKind `json:"kind"`
	FeatureID string           `json:"featureId"`
	Field     string           `json:"field,omitempty"`
	Canonical string           `json:"canonical,omitempty"`
	Mirror    string           `json:"mirror,omitempty"`
	Message   string           `json:"message"`
}

// Screenshot 是 tester 在 verify.json 記錄的截圖 metadata。
type Screenshot struct {
	Path        string `json:"path"`
	Step        string `json:"step"`
	Description string `json:"description"`
}

// ScreenshotGroup 表示同一 round 的截圖清單。
type ScreenshotGroup struct {
	Round       int          `json:"round"`
	Screenshots []Screenshot `json:"screenshots"`
}

// DefaultScreenshotDir 是 tester 預設截圖目錄，可用 {feature-id}、{round} 變數。
const DefaultScreenshotDir = ".4x/e2e/{feature-id}/screenshot/"
