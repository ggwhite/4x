package protocol

import "sort"

// RoleContract 描述一個 pipeline 角色的產出物契約：Produces 為該角色負責寫入的
// artifact 檔名清單，Consumes 為該角色會讀取的上游 artifact 檔名清單。
// 檔名一律使用 workspace.go 定義的常量（如 TaskBrief / ReviewReport），
// 不使用字面字串，確保與實際 runtime 檔名同步。
type RoleContract struct {
	// Produces 是本角色負責產出的 artifact 檔名（canonical 排序）。
	Produces []string
	// Consumes 是本角色會讀取的上游 artifact 檔名（canonical 排序）。
	Consumes []string
}

// roleContracts 是各 pipeline 角色產出物契約的單一權威表（SoT）。
// 依 runtime 現況（templates + guard gate）建立，涵蓋 AllRoles() 的 8 個角色。
//
// 註：final-report.md 由 Tester 與 Acceptor 皆為 producer（DR2）——
// testing phase 與 accepting phase 的 guard 都 gate 其存在，改歸屬會破壞 gate，
// 因此兩者皆列為 producer，衍生邏輯據此讓它對兩者皆不列入 Forbidden。
//
// 註：verify.json 由 `4x verify` 產出、非角色直接寫，不列入本表。
var roleContracts = map[Role]RoleContract{
	RoleDesigner: {
		Produces: []string{TaskBrief, Criteria, TestStratFile},
		Consumes: nil,
	},
	RoleDesignReviewer: {
		Produces: []string{DesignReviewReport},
		Consumes: []string{TaskBrief, Criteria, TestStratFile},
	},
	RoleCoder: {
		Produces: []string{CoderReport},
		Consumes: []string{TaskBrief, ReviewReport, TestReport},
	},
	RoleReviewer: {
		Produces: []string{ReviewReport},
		Consumes: []string{CoderReport, TaskBrief},
	},
	RoleTester: {
		Produces: []string{TestReport, FinalReport},
		Consumes: []string{Criteria, TestStratFile, CoderReport},
	},
	RoleDeepReviewer: {
		Produces: []string{DeepReviewReport},
		Consumes: []string{TaskBrief, Criteria, ReviewReport, TestReport, CoderReport},
	},
	RoleFixer: {
		Produces: []string{FixerReport},
		Consumes: []string{DeepReviewReport},
	},
	RoleAcceptor: {
		Produces: []string{FinalReport, RetroLearningsFile},
		Consumes: []string{TaskBrief, Criteria, ReviewReport, TestReport, DeepReviewReport},
	},
}

// ArtifactContract 查表回傳某角色的產出物契約。未列入本表的角色
// （mini-coder / synthesizer 等子角色）回傳 (RoleContract{}, false)，
// 代表不注入 profile-aware 段落。回傳的 slice 為 canonical 排序後的副本。
func ArtifactContract(role Role) (RoleContract, bool) {
	rc, ok := roleContracts[role]
	if !ok {
		return RoleContract{}, false
	}
	return RoleContract{
		Produces: sortedCopy(rc.Produces),
		Consumes: sortedCopy(rc.Consumes),
	}, true
}

// RoleArtifactView 是某角色在特定 profile 下的產出物視圖：
// Required 為本 profile 下必須產出的 artifact；Forbidden 為本 profile 其他啟用角色
// 的產出物（不含本角色共產者）；AbsentUpstream 為本角色會消費、但本 profile 不會
// 產出（所有 producer 角色的 phase 都未啟用）的上游 artifact，其缺席屬正常。
type RoleArtifactView struct {
	// Required 是本 profile 下本角色必須產出的 artifact 檔名（排序去重）。
	Required []string
	// Forbidden 是本 profile 其他啟用角色的產出物，本角色不應寫入（排序去重）。
	Forbidden []string
	// AbsentUpstream 是本角色消費的上游 artifact 中，本 profile 不會產出者，缺席屬正常（排序去重）。
	AbsentUpstream []string
}

// artifactProducers 由 roleContracts 反查建立 artifact → producer 角色集合。
// 一個 artifact 可有多個 producer（如 final-report.md 由 tester+acceptor 共產）。
func artifactProducers() map[string][]Role {
	producers := make(map[string][]Role)
	for role, rc := range roleContracts {
		for _, f := range rc.Produces {
			producers[f] = append(producers[f], role)
		}
	}
	return producers
}

// RoleArtifactView 計算某角色在此 profile 下的產出物視圖（見 RoleArtifactView 型別說明）。
// 未列入契約表的角色回傳零值。
func (pc ProfileConfig) RoleArtifactView(role Role) RoleArtifactView {
	rc, ok := roleContracts[role]
	if !ok {
		return RoleArtifactView{}
	}

	self := make(map[string]bool, len(rc.Produces))
	for _, f := range rc.Produces {
		self[f] = true
	}

	// Forbidden：本 profile 其他啟用角色的 Produces 聯集，扣除本角色自己的 Produces。
	forbidden := make(map[string]bool)
	for other, orc := range roleContracts {
		if other == role || !pc.EnablesRole(other) {
			continue
		}
		for _, f := range orc.Produces {
			if !self[f] {
				forbidden[f] = true
			}
		}
	}

	// AbsentUpstream：本角色 Consumes 中，其所有 producer 角色的 phase 都未啟用者。
	producers := artifactProducers()
	absent := make(map[string]bool)
	for _, f := range rc.Consumes {
		producedHere := false
		for _, p := range producers[f] {
			if pc.EnablesRole(p) {
				producedHere = true
				break
			}
		}
		if !producedHere {
			absent[f] = true
		}
	}

	return RoleArtifactView{
		Required:       sortedCopy(rc.Produces),
		Forbidden:      sortedSetKeys(forbidden),
		AbsentUpstream: sortedSetKeys(absent),
	}
}

// EnabledPhaseNames 以 canonical pipeline 順序回傳本 profile 已啟用的 phase 名稱清單，
// 供 prompt 段落列出 profile 的 phase 流程。
func (pc ProfileConfig) EnabledPhaseNames() []string {
	var names []string
	for _, phase := range SelectablePhases() {
		if pc.EnablesPhase(phase) {
			names = append(names, string(phase))
		}
	}
	return names
}

// sortedCopy 回傳去重後排序的副本，nil 輸入回 nil，確保渲染與測試 deterministic。
func sortedCopy(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	set := make(map[string]bool, len(in))
	for _, s := range in {
		set[s] = true
	}
	return sortedSetKeys(set)
}

// sortedSetKeys 回傳 set 的排序 key 清單，空 set 回 nil。
func sortedSetKeys(set map[string]bool) []string {
	if len(set) == 0 {
		return nil
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
