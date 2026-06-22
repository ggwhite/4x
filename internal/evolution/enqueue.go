package evolution

import (
	"github.com/ggwhite/4x/internal/feature"
	"github.com/ggwhite/4x/internal/protocol"
)

// CandidateToDiscovered 把 mine/gate 階段流通的 protocol.Candidate 轉成 enrich 套件期望的
// protocol.DiscoveredFeature 輸入。兩者目前都只有 Title/Description，轉接僅搬移這兩欄。
func CandidateToDiscovered(c protocol.Candidate) protocol.DiscoveredFeature {
	return protocol.DiscoveredFeature{Title: c.Title, Description: c.Description}
}

// BareFeature 在 enrich（LLM）失敗或 discarded 時，用「已通過 value gate」的 candidate 原文
// 物化成一筆最小 feature（裁決 3：gate 已認可其價值，不因 enrich 失敗而遺失）。
// Status 一律 not-started（裁決 2：閘門即核准，不再用 draft 二次卡關）。
// id 與 name 由呼叫端依 backlog 編號算好後傳入。
func BareFeature(c protocol.Candidate, id, name string) feature.Feature {
	return feature.Feature{
		ID:          id,
		Name:        name,
		Description: c.Description,
		Status:      feature.StatusNotStarted,
	}
}
