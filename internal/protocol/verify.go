package protocol

import (
	"time"

	"github.com/ggwhite/4x/internal/feature"
)

// ACEvidence 是單一 acceptance criterion 的驗證結果
type ACEvidence struct {
	ID         string   `json:"id"`
	Passed     bool     `json:"passed"`
	Evidence   []string `json:"evidence"`
	VerifyType string   `json:"verify_type,omitempty"` // Tester 寫入供記錄；Guard 從 TestStrategy.ACVerifyMap 判斷
	// Checks 是 ac_checks 命令的實際執行結果（由 `4x verify` 寫入，非 Tester 手填）。
	// guard 從這裡的 exit code 重新計算 Passed，作為權威判定。為空表示此 AC 未綁定 ac_checks。
	Checks []VerifyCommand `json:"checks,omitempty"`
}

// VerifyEvidence 是 rounds/round-N/verify.json 的結構
type VerifyEvidence struct {
	Passed             bool                 `json:"passed"`
	Round              int                  `json:"round"`
	Role               Role                 `json:"role"`
	Commands           []VerifyCommand      `json:"commands"`
	Screenshots        []feature.Screenshot `json:"screenshots,omitempty"`
	ACResults          []ACEvidence         `json:"ac_results,omitempty"`
	ManualCheckResults []ManualCheckResult  `json:"manual_check_results,omitempty"`
}

// ManualCheckResult 是 Tester 對單一 ManualCheck 的執行結果。
type ManualCheckResult struct {
	ID       string   `json:"id"`
	Passed   bool     `json:"passed"`
	Evidence []string `json:"evidence"`
}

// VerifyCommand 是單一 verify command 的結果
type VerifyCommand struct {
	Command          string    `json:"command"`
	ExitCode         int       `json:"exitCode"`
	ExpectedExitCode *int      `json:"expectedExitCode,omitempty"`
	DurationMs       int64     `json:"durationMs"`
	Summary          string    `json:"summary"`
	StartedAt        time.Time `json:"startedAt"`
	FinishedAt       time.Time `json:"finishedAt"`
	Group            string    `json:"group,omitempty"`
	Skipped          bool      `json:"skipped,omitempty"`
}

// HealthCheck 是 testing phase 啟動前的環境檢查設定。
// Commands 逐一執行任一失敗即停；失敗時若有 Recovery 則逐一執行後重跑一次 Commands。
// Timeout 為每個 command 的逾時秒數，未設定（0）時由呼叫端套用預設 30 秒。
type HealthCheck struct {
	Commands []string `yaml:"commands" json:"commands"`
	Recovery []string `yaml:"recovery,omitempty" json:"recovery,omitempty"`
	Timeout  int      `yaml:"timeout,omitempty" json:"timeout,omitempty"`
}

// TestStrategy 是 test-strategy.yaml 的結構
type TestStrategy struct {
	Web          bool                `yaml:"web" json:"web"`
	API          bool                `yaml:"api" json:"api"`
	Gate         bool                `yaml:"gate" json:"gate"`
	CoderOnly    bool                `yaml:"coder_only" json:"coder_only"`
	Verify       []string            `yaml:"verify_commands" json:"verify_commands"`
	HealthCheck  *HealthCheck        `yaml:"health_check,omitempty" json:"health_check,omitempty"`
	VerifyGroups map[string][]string `yaml:"verify_groups,omitempty" json:"verify_groups,omitempty"`
	// Profiles 標記本 feature 適用的測試 profile（如 unit/web/api/e2e），
	// Tester prompt 會依此自動注入對應的測試方法論；為空時行為與舊版一致。
	Profiles []string `yaml:"profiles,omitempty" json:"profiles,omitempty"`
	// ManualChecks 定義 Tester 必須實際執行（非讀 code）的驗證步驟。
	// Guard 會驗證 verify.json 含有對應的 manual_check_results。
	ManualChecks []ManualCheck `yaml:"manual_checks,omitempty" json:"manual_checks,omitempty"`
	// ACVerifyMap 標記每條 AC 的驗證類型（unit-test / integration / inspection / skip）。
	// Guard 依此決定 evidence 品質要求；未列出的 AC 預設 execution（從嚴）。
	ACVerifyMap map[string]string `yaml:"ac_verify_map,omitempty" json:"ac_verify_map,omitempty"`
	// E2ERepos 列出本 feature 的 e2e 測試產出所在的獨立 repo 名稱（如 kairos-e2e）。
	// testing phase 起（含 testing 之後回到的 amending），這些 repo 的變更不算 scope violation
	// （Tester 必要寫入放行）；為空時行為與舊版完全一致。granularity 為 repo 層級，比照 Config.HubRepos。
	E2ERepos []string `yaml:"e2e_repos,omitempty" json:"e2e_repos,omitempty"`
	// ACChecks 綁定每條 AC 到一或多條可執行 check 命令。key 為 AC ID（如 "AC-1"）。
	// 命令以 exit code 判定：全部 exit 0 = 該 AC PASS，任一非 0 = FAIL——此為權威判定，
	// 覆蓋 verify.json 內 Tester 自報的 passed。為空時行為與舊版完全一致（回退 prose evidence）。
	ACChecks map[string][]string `yaml:"ac_checks,omitempty" json:"ac_checks,omitempty"`
}

// ManualCheck 是 Designer 在 test-strategy.yaml 定義的手動驗證步驟。
// Tester 必須逐步執行 Steps 並記錄實際輸出，不能只讀 code 判定。
type ManualCheck struct {
	ID          string   `yaml:"id" json:"id"`
	ACRef       string   `yaml:"ac_ref,omitempty" json:"ac_ref,omitempty"`
	Description string   `yaml:"description" json:"description"`
	Steps       []string `yaml:"steps" json:"steps"`
}

// TestProfileOverride 允許專案在 settings.json 覆寫或新增 test profile。
// Content 直接指定內容；Include 指定相對於 workspace root 的檔案路徑。兩者擇一，整組取代內建。
type TestProfileOverride struct {
	Content string `json:"content,omitempty"`
	Include string `json:"include,omitempty"`
}
