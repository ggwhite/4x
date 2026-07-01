package protocol

import "github.com/ggwhite/4x/internal/feature"

// Phase 表示 4x 狀態機的各階段
type Phase string

const (
	PhaseInit            Phase = "init"
	PhaseDesigning       Phase = "designing"
	PhaseDesignReviewing Phase = "design-reviewing"
	PhaseCoding          Phase = "coding"
	PhaseReviewing       Phase = "reviewing"
	PhaseDeepReviewing   Phase = "deep-reviewing"
	PhaseFixing          Phase = "fixing"
	PhaseTesting         Phase = "testing"
	PhaseAmending        Phase = "amending"
	PhaseAccepting       Phase = "accepting"
	PhasePendingReview   Phase = "pending-review"
	PhaseDone            Phase = "done"
	PhaseAbandoned       Phase = "abandoned"
	PhaseBlocked         Phase = "blocked"
	PhaseNeedsAttention  Phase = "needs-attention"
)

// Role 表示 4x pipeline 中的角色
type Role string

const (
	RoleDesigner       Role = "designer"
	RoleDesignReviewer Role = "design-reviewer"
	RoleCoder          Role = "coder"
	RoleReviewer       Role = "reviewer"
	RoleDeepReviewer   Role = "deep-reviewer"
	RoleTester         Role = "tester"
	RoleAcceptor       Role = "acceptor"
	RoleFixer          Role = "fixer"
	// RoleMiniCoder 與 RoleReVerifier 是 deep-reviewing phase 內自癒循環的子 role，
	// 不對應任何 state machine phase（全程維持 deep-reviewing），僅用於 prompt template
	// 與 event/log 辨識。
	RoleMiniCoder  Role = "mini-coder"
	RoleReVerifier Role = "re-verifier"
	// RoleSynthesizer 是平行 deep review 模式下合併多份 partial report 的子 role，
	// 同樣全程維持 deep-reviewing phase，僅用於 prompt template 與 event/log 辨識。
	RoleSynthesizer Role = "synthesizer"
	// RoleGate 是 F097 evolve 價值閘門 role，判斷 candidate feature 是否值得進 backlog 並強制寫
	// why_not_hack。不對應任何 state machine phase，由 evolve driver（F099）在 CLI veto 之間執行。
	RoleGate Role = "gate"
	// RoleConsolidator 是 learnings consolidator role，在 harvest 後判斷語意重複的 learnings 並合併/移除。
	RoleConsolidator Role = "consolidator"
	// RoleRoundSummarizer 是 round summarizer role，在進入 accepting 前將舊輪次的 review/test report
	// 壓縮為 rounds-summary.md，供 Acceptor 取代讀取所有輪次全文。
	RoleRoundSummarizer Role = "round-summarizer"
)

// SubPhase 表示 deep-reviewing phase 內的子步驟，僅在 phase==deep-reviewing 時有意義；
// 其餘 phase 一律為空字串。用於 dashboard 顯示細部進度與 crash recovery 推斷重跑起點。
type SubPhase string

const (
	SubPhaseReviewing    SubPhase = "reviewing"    // sub-reviewer 平行審查中
	SubPhaseSynthesizing SubPhase = "synthesizing" // synthesizer 合併 partial 中
	SubPhaseFixing       SubPhase = "fixing"       // mini-coder 修正 issue 中
	SubPhaseReverifying  SubPhase = "reverifying"  // re-verifier 複驗中
)

// DeepReviewAngleCount 是 deep-reviewer 模板定義的 review angle 總數（angle 1..11）。
// 平行 deep review 依此把 angle 平均分配給各 sub-reviewer。
const DeepReviewAngleCount = 11

// Severity 表示 review issue 的嚴重等級
type Severity string

const (
	SeverityCritical Severity = "critical"
	SeverityWarning  Severity = "warning"
	SeverityLow      Severity = "low"
)

// PhaseToStatus 將 state machine 的 Phase 映射為面向 dashboard 的 feature.Status
func PhaseToStatus(phase Phase) feature.Status {
	switch phase {
	case PhasePendingReview:
		return feature.StatusReadyForReview
	case PhaseDone:
		return feature.StatusDone
	case PhaseAbandoned:
		return feature.StatusAbandoned
	case PhaseBlocked:
		return feature.StatusBlocked
	case PhaseNeedsAttention:
		return feature.StatusNeedsAttention
	case PhaseInit:
		return feature.StatusNotStarted
	default:
		return feature.StatusInProgress
	}
}

// ChangedFile 是檔案層變更的跨層共用型別（guard 與 gitops 共用，放 protocol 避免 import cycle）。
type ChangedFile struct {
	// Path 是變更檔案相對 scope root 的路徑。
	Path string
	// Lines 是變更行數：tracked 檔為 added+deleted，untracked 檔為檔案總行數。
	Lines int
}
