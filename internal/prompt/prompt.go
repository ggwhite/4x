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
	// Profile 是已解析的 profile 名稱（如 state.json 的 profile 欄位）。
	// 空字串代表由 ResolveProfile(cfg, feature, "") 依 feature/default 決定。
	Profile string
	// ExtraPrompt 是本次啟動的一次性 note，由 orchestrator 從 State.RunNote 帶入
	// （只給本次第一個 role）。空字串代表無 note，template 不渲染對應區塊。
	ExtraPrompt string
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
	// Learnings 是本角色相關的 learnings（依 category 篩選 + active/candidate 配額交錯排序），供 template 注入。
	Learnings []learning.Entry
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
	// DesignRevision 為 true 表示這是 designer 的修訂輪（design-review-report.md 存在且 verdict=FAIL）；
	// template 據此在頂端渲染 REVISION 硬指令，並注入下方 PrevDesignReview / PrevTaskBrief 等前版成品，
	// 逼 designer 就地 Edit 而非拿完整素材從零重新分析（避免 designer↔design-reviewer 迴圈不收斂）。
	DesignRevision bool
	// PrevDesignReview 是 design-review-report.md 抽出的 delta（Architecture Risks / Overengineering /
	// Missing Requirements / Verdict），僅 designer 修訂輪注入，讓其只聚焦被點名的問題。
	PrevDesignReview string
	// PrevTaskBrief / PrevCriteria / PrevTestStrategy 是前一版三個設計產出物的完整內容，
	// designer 修訂輪注入供其就地 Edit（不是從零重寫）。
	PrevTaskBrief    string
	PrevCriteria     string
	PrevTestStrategy string
	// PrevReviewReport 是上一輪 review-report.md 的完整內容（amending 時使用）。
	PrevReviewReport string
	// PrevTestReport 是上一輪 test-report.md 的完整內容（amending 時使用，可能不存在）。
	PrevTestReport string
	// PrevDiff 是前幾輪 coder 從 baseline 到目前的 git diff（amending 時使用），
	// 讓 coder 不用重新探索就知道已經改了什麼。超過 maxDiffLines 行時截斷。
	PrevDiff string
	// SelfHealSource 是 mini-coder 建立 fix 清單的來源報告檔名。空字串時由 Generate
	// 對 RoleMiniCoder 預設為 protocol.DeepReviewReport（deep-reviewing 自癒路徑不變）；
	// reviewing 同輪收斂則透過 WithConditionalSource(protocol.ReviewReport) 設為 review-report.md。
	SelfHealSource string
	// ReviewingConvergence 為 true 表示這個 mini-coder 跑在 reviewing 同輪收斂路徑（fix 來源是
	// review-report.md、由 reviewer 產出、phase 為 reviewing）；false 表示 deep-reviewing 自癒
	// 路徑。供模板切換 phase/role 框架用語，避免收斂路徑仍顯示 deep-review 專屬字樣。
	ReviewingConvergence bool
	// 以下欄位僅平行 deep review 模式使用：
	ReviewerIndex     int
	ReviewerCount     int
	AssignedAngles    []int
	PartialReportName string
	PartialReports    []IncludeContent
	// ProfileName 是本次 render 解析出的 profile 名稱；解析失敗時為空。
	ProfileName string
	// ProfilePhases 是本 profile 已啟用的 phase 名稱（canonical 順序）；解析失敗時為空。
	ProfilePhases []string
	// ProfileArtifactSection 是 profile-aware 產出物契約段落（僅 6 個執行角色注入），
	// 由 FormatProfileArtifactSection 產生；不注入時為空，template 據此省略整段。
	ProfileArtifactSection string
	// StaleReport 非 nil 時代表 resume 情境偵測到本階段報告 mtime 落後本輪最新程式碼變更，
	// 供 template 注入強制警示；首次執行（報告不存在）或報告較新時為 nil，渲染與現況一致。
	StaleReport *StaleReportInfo
	// ExtraPrompt 是本次啟動的一次性 note，由 Generate 從 Context.ExtraPrompt 複製而來；
	// locale.tmpl 以 {{if .ExtraPrompt}} 區塊渲染，空字串則不輸出。
	ExtraPrompt string
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
		ExtraPrompt:         ctx.ExtraPrompt,
	}
	briefPath := filepath.Join(ws.FeatureDir(feature.ID), protocol.TaskBrief)
	skippedDesigner := false
	if _, err := os.Stat(briefPath); err != nil {
		skippedDesigner = true
	}
	condensePlan := role != protocol.RoleDesigner && role != protocol.RoleDesignReviewer && !skippedDesigner
	data.PlanningDoc = LoadPlanningDocs(ws.Root, feature, cfg.DesignDocDirs, condensePlan)
	data.Learnings = LoadLearningsForRole(ws.DotDir(), role)
	data.SkippedDesigner = skippedDesigner
	if role == protocol.RoleDesigner || role == protocol.RoleCoder || role == protocol.RoleReviewer {
		data.CodeMap = BuildCodeMap(ws.Root)
	}
	if role == protocol.RoleCoder || role == protocol.RoleMiniCoder {
		if !skippedDesigner {
			if b, err := os.ReadFile(briefPath); err == nil {
				data.TaskBrief = string(b)
			}
		}
		if round > 1 {
			prevRound := ws.RoundDir(feature.ID, round-1)
			if b, err := os.ReadFile(filepath.Join(prevRound, protocol.ReviewReport)); err == nil {
				// F142：只注入 review delta（Issues + Verdict），抽取失敗則 fallback 全文。
				data.PrevReviewReport, _ = extractReviewDelta(string(b))
			}
			if b, err := os.ReadFile(filepath.Join(prevRound, protocol.TestReport)); err == nil {
				// F142：只注入 test delta（FAIL/SKIP 列 + Verdict），抽取失敗則 fallback 全文。
				data.PrevTestReport, _ = extractTestDelta(string(b))
			}
			data.PrevDiff = BaselineDiff(ws, feature.ID)
		}
	}
	if role == protocol.RoleDesigner && !skippedDesigner {
		// designer 修訂輪：design-review-report.md 存在且 verdict=FAIL 時，注入前一版三個產出物全文
		// 與抽出的 review delta，並打開 DesignRevision 讓 template 走「就地 Edit、只修被點名項」的
		// 硬指令路徑；首次分析（報告不存在）或 PASS/畸形報告時 extractDesignReviewDelta 回 false，
		// 一切留零值、渲染與現況一致。
		drrPath := filepath.Join(ws.FeatureDir(feature.ID), protocol.DesignReviewReport)
		if b, err := os.ReadFile(drrPath); err == nil {
			if delta, ok := extractDesignReviewDelta(string(b)); ok {
				data.DesignRevision = true
				data.PrevDesignReview = delta
				if pb, err := os.ReadFile(briefPath); err == nil {
					data.PrevTaskBrief = string(pb)
				}
				if pc, err := os.ReadFile(filepath.Join(ws.FeatureDir(feature.ID), protocol.Criteria)); err == nil {
					data.PrevCriteria = string(pc)
				}
				if pt, err := os.ReadFile(filepath.Join(ws.FeatureDir(feature.ID), protocol.TestStratFile)); err == nil {
					data.PrevTestStrategy = string(pt)
				}
			}
		}
	}
	if role == protocol.RoleTester {
		data.GuardFeedback = readGuardFeedback(ws, feature.ID, round)
	}
	// F150：注入 profile-aware 產出物契約段落。解析出當前 profile 後填 ProfileName/
	// ProfilePhases；僅對列入 ArtifactContract 的執行角色（6 個）填 ProfileArtifactSection。
	// 解析失敗時三欄位留零值，渲染行為與現況一致（不注入段落）。
	if profileName, pc, perr := protocol.ResolveProfile(cfg, feature, ctx.Profile); perr == nil {
		data.ProfileName = profileName
		data.ProfilePhases = pc.EnabledPhaseNames()
		if _, ok := protocol.ArtifactContract(role); ok {
			data.ProfileArtifactSection = FormatProfileArtifactSection(profileName, pc, role)
		}
	}
	// F172：resume 情境下偵測本階段報告 mtime 是否落後本輪最新程式碼變更（coder/fixer-report），
	// 若落後則注入 StaleReport 供 template 渲染強制警示；非目標角色或無基準時為 nil，渲染與現況一致。
	data.StaleReport = detectStaleReport(ws, feature.ID, role, round)
	for _, opt := range opts {
		opt(&data)
	}
	// mini-coder 的 fix 來源報告：未由 WithConditionalSource 指定時預設 deep-review-report.md，
	// 維持既有 deep-reviewing 自癒路徑不變。
	if role == protocol.RoleMiniCoder && data.SelfHealSource == "" {
		data.SelfHealSource = protocol.DeepReviewReport
	}
	data.ReviewingConvergence = role == protocol.RoleMiniCoder && data.SelfHealSource == protocol.ReviewReport
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
