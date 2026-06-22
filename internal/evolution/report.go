package evolution

import (
	"fmt"
	"strings"

	"github.com/ggwhite/4x/internal/protocol"
)

// EnqueuedFeature 是一筆已排入 backlog 的 feature 摘要。Enriched 標記其是否經 LLM 補強
// （false 代表走裁決 3 的 bare candidate fallback）。
type EnqueuedFeature struct {
	ID       string
	Name     string
	Enriched bool
}

// AutoRunResult 是 --auto-run 對單一 enqueued feature 跑完 meta-loop 後的結果。
// SelfModBlocked 為 true 表示觸及 self_mod_guard 受保護路徑、需人工 approve 才能完成（F098）。
type AutoRunResult struct {
	ID             string
	FinalStatus    string
	SelfModBlocked bool
}

// EvolveRoundResult 彙整單輪 evolve pipeline 的所有產出，供 BuildEvolveReport 生成 markdown。
type EvolveRoundResult struct {
	Round               int
	Mined               []protocol.Candidate
	Deduped             int
	Accepted            []protocol.Candidate
	Rejected            []Rejection
	Enqueued            []EnqueuedFeature
	AutoRan             []AutoRunResult
	Halted              bool
	ConsecutiveNoAccept int
}

// BuildEvolveReport 把單輪結果組成 .4x/evolve-report.md 內容，分段呈現
// Mined / Accepted / Rejected / Enqueued / Auto-Run / Halted。
func BuildEvolveReport(r EvolveRoundResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Evolve Report — Round %d\n\n", r.Round)

	b.WriteString("## Mined\n")
	fmt.Fprintf(&b, "%d candidate(s) sent to gate (%d deduped).\n", len(r.Mined), r.Deduped)
	for _, c := range r.Mined {
		fmt.Fprintf(&b, "- %s\n", c.Title)
	}

	b.WriteString("\n## Accepted\n")
	if len(r.Accepted) == 0 {
		b.WriteString("None\n")
	} else {
		for _, c := range r.Accepted {
			fmt.Fprintf(&b, "- %s (value %.2f)\n", c.Title, c.ValueScore)
		}
	}

	b.WriteString("\n## Rejected\n")
	if len(r.Rejected) == 0 {
		b.WriteString("None\n")
	} else {
		for _, rej := range r.Rejected {
			fmt.Fprintf(&b, "- %s — %s\n", rej.Title, rej.Reason)
		}
	}

	b.WriteString("\n## Enqueued\n")
	if len(r.Enqueued) == 0 {
		b.WriteString("None\n")
	} else {
		for _, e := range r.Enqueued {
			fmt.Fprintf(&b, "- %s — %s (enriched=%t)\n", e.ID, e.Name, e.Enriched)
		}
	}

	b.WriteString("\n## Auto-Run\n")
	if len(r.AutoRan) == 0 {
		b.WriteString("None\n")
	} else {
		for _, a := range r.AutoRan {
			fmt.Fprintf(&b, "- %s — %s (self-mod blocked=%t)\n", a.ID, a.FinalStatus, a.SelfModBlocked)
		}
	}

	b.WriteString("\n## Halted\n")
	if r.Halted {
		fmt.Fprintf(&b, "true — %d consecutive idle rounds reached anti-spin threshold; mine/gate skipped. Use `4x evolve --force` to override.\n", r.ConsecutiveNoAccept)
	} else {
		fmt.Fprintf(&b, "false (consecutive idle rounds: %d)\n", r.ConsecutiveNoAccept)
	}

	return b.String()
}
