package prompt

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	feat "github.com/ggwhite/4x/internal/feature"
	"github.com/ggwhite/4x/internal/learning"
	"github.com/ggwhite/4x/internal/protocol"
)

// Context 收納 prompt 生成所需的環境資訊，由 CLI 層從 runContext 組裝後傳入。
type Context struct {
	Ws       *protocol.Workspace
	RunnerWs *protocol.Workspace
	Feature  feat.Feature
	Cfg      protocol.Config
}

// Data 收納一個 role prompt 模板所需的全部資料。
type Data struct {
	Feature          feat.Feature
	Project          protocol.ProjectConfig
	Role             protocol.Role
	Round            int
	Iteration        int
	Config           protocol.Config
	DotDir           string
	Locale           string
	LocaleName       string
	RoleInstructions []string
	ProjectIncludes  []IncludeContent
	RoleIncludes     []IncludeContent
	PlanningDoc      string
	RepoMap          map[string]string
	// ProfileInstructions 是 test-strategy.yaml profiles 載入後的測試方法論，供 Tester template 注入。
	ProfileInstructions []ProfileContent
	// Learnings 是所有 active learnings，供 Designer template 列出供選擇。
	Learnings []learning.Entry
	// SelectedLearnings 是已選中且符合本 role category 的 learnings，供後續 role template 注入。
	SelectedLearnings []learning.Entry
	// CodeMap 是專案的 exported symbol 摘要（每個 package 一行），讓 agent 不用探索就知道 codebase 結構。
	CodeMap string
	// ScreenshotDir 是 settings.json 解析後的 tester 截圖目錄（{feature-id} 已替換），
	// 供 Designer 撰寫 test-strategy.yaml 時直接引用，避免自行編造路徑導致 round 收尾同步遺漏。
	ScreenshotDir string
	// SkippedDesigner 為 true 代表 profile 跳過了 designer phase，template 應從 feature YAML 讀需求。
	SkippedDesigner bool
	// TaskBrief 是 task-brief.md 的完整內容，直接內嵌到 prompt 省掉 agent 的 Read tool call。
	TaskBrief string
	// GuardFeedback 是 guard retry 時的錯誤訊息，供 tester 知道上次哪裡沒做到位。
	GuardFeedback []string
	// PrevReviewReport 是上一輪 review-report.md 的完整內容（amending 時使用）。
	PrevReviewReport string
	// PrevTestReport 是上一輪 test-report.md 的完整內容（amending 時使用，可能不存在）。
	PrevTestReport string
	// PrevDiff 是前幾輪 coder 從 baseline 到目前的 git diff（amending 時使用），
	// 讓 coder 不用重新探索就知道已經改了什麼。超過 maxDiffLines 行時截斷。
	PrevDiff string
	// 以下欄位僅平行 deep review 模式使用：
	ReviewerIndex     int
	ReviewerCount     int
	AssignedAngles    []int
	PartialReportName string
	PartialReports    []IncludeContent
}

// IncludeContent 是一個被載入的檔案內容，含路徑與內文。
type IncludeContent struct {
	Path    string
	Content string
}

// ProfileContent 是單一 test profile 載入後的名稱與內容，供 Tester template 注入。
type ProfileContent struct {
	Name    string
	Content string
}

// Option 在 Data 組好、模板 render 前對其做最後調整，
// 供平行 deep review 注入 ReviewerIndex / AssignedAngles / PartialReports 等額外欄位。
type Option func(*Data)

// Result 是 prefetch goroutine 透過 channel 回傳的生成結果。
type Result struct {
	Prompt string
	Err    error
}

// Prefetch 保存一個已在背景啟動的 prompt 預生成任務；以 role+round 為 key，
// 消費端 mismatch 時丟棄並退回同步生成，確保不會用錯 prompt。
type Prefetch struct {
	Role  protocol.Role
	Round int
	Ch    chan Result
}

// Generate 根據 Context 與 role/round 等參數組裝並 render prompt 模板。
func Generate(ctx *Context, role protocol.Role, round, iteration int, runnerName string, opts ...Option) (string, error) {
	ws := ctx.Ws
	runnerWs := ctx.RunnerWs
	feature := ctx.Feature
	cfg := ctx.Cfg

	tmpl, err := LoadRoleTemplate(runnerWs.DotDir(), role)
	if err != nil {
		return "", fmt.Errorf("no template for role %s: %w", role, err)
	}
	locale, localeName := ResolveLocale()
	var roleInc []string
	if roleCfg, ok := cfg.Roles[string(role)]; ok {
		roleInc = roleCfg.Includes
	}
	var repoMap map[string]string
	if len(cfg.Workspace.Repos) > 0 {
		if runnerWs.Root != ws.Root {
			featureRepos := make(map[string]bool, len(feature.Repos))
			for _, r := range feature.Repos {
				featureRepos[r] = true
			}
			repoMap = make(map[string]string, len(cfg.Workspace.Repos))
			for name := range cfg.Workspace.Repos {
				if len(feature.Repos) > 0 && !featureRepos[name] {
					continue
				}
				repoMap[name] = name
			}
		} else {
			repoMap = protocol.ResolveFeatureRepoPaths(feature, cfg, ws.Root)
		}
	}
	data := Data{
		Feature:             feature,
		Project:             cfg.Project,
		Role:                role,
		Round:               round,
		Iteration:           iteration,
		Config:              cfg,
		DotDir:              runnerWs.DotDir(),
		Locale:              locale,
		LocaleName:          localeName,
		RoleInstructions:    RoleInstructions(cfg, role),
		ProjectIncludes:     append(LoadIncludes(ws.Root, cfg.Project.Includes, RunnerAutoReads[runnerName]...), DiscoverConventionFiles(ws.Root, runnerName, cfg.Project.Includes)...),
		RoleIncludes:        LoadIncludes(ws.Root, roleInc),
		RepoMap:             repoMap,
		ProfileInstructions: LoadProfiles(ws, feature.ID, cfg),
		ScreenshotDir:       strings.ReplaceAll(protocol.ScreenshotDir(cfg), "{feature-id}", feature.ID),
	}
	briefPath := filepath.Join(ws.FeatureDir(feature.ID), protocol.TaskBrief)
	skippedDesigner := false
	if _, err := os.Stat(briefPath); err != nil {
		skippedDesigner = true
	}
	condensePlan := role != protocol.RoleDesigner && role != protocol.RoleDesignReviewer && !skippedDesigner
	data.PlanningDoc = LoadPlanningDocs(ws.Root, feature, cfg.DesignDocDirs, condensePlan)
	if role == protocol.RoleDesigner {
		data.Learnings = LoadActiveLearnings(ws.DotDir())
	} else if skippedDesigner && round == 1 && role == protocol.RoleCoder {
		data.Learnings = LoadActiveLearnings(ws.DotDir())
	} else {
		data.SelectedLearnings = LoadSelectedLearnings(ws.DotDir(), feature.ID, role)
	}
	data.SkippedDesigner = skippedDesigner
	data.CodeMap = BuildCodeMap(ws.Root)
	if role == protocol.RoleCoder || role == protocol.RoleMiniCoder {
		if !skippedDesigner {
			if b, err := os.ReadFile(briefPath); err == nil {
				data.TaskBrief = string(b)
			}
		}
		if round > 1 {
			prevRound := ws.RoundDir(feature.ID, round-1)
			if b, err := os.ReadFile(filepath.Join(prevRound, protocol.ReviewReport)); err == nil {
				data.PrevReviewReport = string(b)
			}
			if b, err := os.ReadFile(filepath.Join(prevRound, protocol.TestReport)); err == nil {
				data.PrevTestReport = string(b)
			}
			data.PrevDiff = BaselineDiff(ws, feature.ID)
		}
	}
	if role == protocol.RoleTester {
		data.GuardFeedback = readGuardFeedback(ws, feature.ID, round)
	}
	for _, opt := range opts {
		opt(&data)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", err
	}
	return CompactBlankLines(buf.String()), nil
}

// CompactBlankLines 把連續多個空行壓成一個，保留 Markdown 段落分隔語意。
func CompactBlankLines(s string) string {
	lines := strings.Split(s, "\n")
	var out []string
	blank := false
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			if !blank {
				out = append(out, "")
				blank = true
			}
			continue
		}
		blank = false
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

// PrefetchablePhase 回報某 phase 是否會在主迴圈頂端走同步 Generate，
// 只有這些 phase 值得預生成 prompt。reviewing 僅在非平行模式下才走頂端路徑。
func PrefetchablePhase(phase protocol.Phase, cfg protocol.Config) bool {
	switch phase {
	case protocol.PhaseDesignReviewing, protocol.PhaseCoding, protocol.PhaseAmending, protocol.PhaseTesting, protocol.PhaseAccepting:
		return true
	case protocol.PhaseReviewing:
		return !cfg.ParallelReviewTest
	default:
		return false
	}
}

// RoleInstructions 從 Config 取出指定角色的 instructions。
func RoleInstructions(cfg protocol.Config, r protocol.Role) []string {
	if rc, ok := cfg.Roles[string(r)]; ok {
		return rc.Instructions
	}
	return nil
}

func readGuardFeedback(ws *protocol.Workspace, featureID string, round int) []string {
	path := filepath.Join(ws.RoundDir(featureID, round), protocol.GuardFeedback)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var fb struct {
		Errors []string `json:"errors"`
	}
	if json.Unmarshal(data, &fb) != nil {
		return nil
	}
	return fb.Errors
}
