package evolution

import (
	"github.com/ggwhite/4x/internal/feature"
	"github.com/ggwhite/4x/internal/protocol"
)

// PreVeto 在 LLM gate 前做便宜去重（省 LLM 成本）：每筆 candidate 與所有 existing feature
// （Name+" "+Description）以及已保留的 candidate（Title+" "+Description）比對 Jaccard 相似度，
// 相似者丟到 dropped，其餘進 kept。比對文字為 candidate 的 Title+" "+Description。
// 回傳順序維持輸入順序。
func PreVeto(cands []protocol.Candidate, existing []feature.Feature, threshold float64) (kept, dropped []protocol.Candidate) {
	for _, c := range cands {
		ctext := c.Title + " " + c.Description
		dup := false
		for _, e := range existing {
			if protocol.IsSimilarFeatureThreshold(ctext, e.Name+" "+e.Description, threshold) {
				dup = true
				break
			}
		}
		if !dup {
			for _, k := range kept {
				if protocol.IsSimilarFeatureThreshold(ctext, k.Title+" "+k.Description, threshold) {
					dup = true
					break
				}
			}
		}
		if dup {
			dropped = append(dropped, c)
		} else {
			kept = append(kept, c)
		}
	}
	return kept, dropped
}
