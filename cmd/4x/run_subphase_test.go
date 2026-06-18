package main

import (
	"testing"

	"github.com/ggwhite/4x/internal/protocol"
)

// parallelCfg 回傳一個讓 deep-review 走平行模式（want>1）的 Config。
// DeepReviewAngleCount=11、ParallelReviewers=3 → GroupReviewAngles 切成 3 組（want=3）。
func parallelCfg(reviewers int) protocol.Config {
	return protocol.Config{
		Roles: map[string]protocol.RoleConfig{
			string(protocol.RoleDeepReviewer): {ParallelReviewers: reviewers},
		},
	}
}

// completePartial 是含結尾 `## Statistics` 段落的完整 partial report；半成品則缺此標記。
const completePartial = "# Deep Review Partial Report\n## Issues\nnone\n## Statistics\n- Reported: 0\n"

// TestDeepPartialComplete 驗證單一 partial 完整性判準：空檔/缺 sentinel→不完整，含 `## Statistics`→完整。
func TestDeepPartialComplete(t *testing.T) {
	ws := &protocol.Workspace{Root: t.TempDir()}
	const fid = "F-partial"

	// 不存在的檔案 → 不完整。
	missing := ws.RoundDir(fid, 1) + "/deep-review-partial-9.md"
	if deepPartialComplete(missing) {
		t.Error("nonexistent partial reported complete")
	}

	tests := []struct {
		name    string
		content string
		want    bool
	}{
		{"empty", "", false},
		{"whitespace only", "  \n\t\n", false},
		{"missing sentinel", "# Deep Review Partial\n## Issues\nhalf-written", false},
		{"complete with statistics", completePartial, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			writeRoundFile(t, ws, fid, 1, "deep-review-partial-1.md", tt.content)
			path := ws.RoundDir(fid, 1) + "/deep-review-partial-1.md"
			if got := deepPartialComplete(path); got != tt.want {
				t.Errorf("deepPartialComplete = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestMissingDeepPartials 驗證在不同 partial 存在組合下回傳的缺失 index 清單（升冪）。
func TestMissingDeepPartials(t *testing.T) {
	tests := []struct {
		name    string
		present []int // 寫入完整 partial 的 index
		want    int
		wantIdx []int
	}{
		{"all missing", nil, 3, []int{1, 2, 3}},
		{"missing middle", []int{1, 3}, 3, []int{2}},
		{"all present", []int{1, 2, 3}, 3, nil},
		{"single agent want 1 none", nil, 1, []int{1}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ws := &protocol.Workspace{Root: t.TempDir()}
			const fid = "F-miss"
			for _, idx := range tt.present {
				writeRoundFile(t, ws, fid, 1, deepReviewPartialName(idx), completePartial)
			}
			got := missingDeepPartials(ws.RoundDir(fid, 1), tt.want)
			if len(got) != len(tt.wantIdx) {
				t.Fatalf("missing = %v, want %v", got, tt.wantIdx)
			}
			for i := range got {
				if got[i] != tt.wantIdx[i] {
					t.Errorf("missing[%d] = %d, want %d (%v)", i, got[i], tt.wantIdx[i], tt.wantIdx)
				}
			}
		})
	}
}

// TestDeepResumeSubPhase 驗證 deep-review report 不完整時的子步驟推斷：
// 缺 partial→reviewing；partial 全到齊→synthesizing；單 agent 模式→reviewing。
func TestDeepResumeSubPhase(t *testing.T) {
	t.Run("missing partial → reviewing", func(t *testing.T) {
		ws := &protocol.Workspace{Root: t.TempDir()}
		const fid = "F-sub"
		writeRoundFile(t, ws, fid, 1, deepReviewPartialName(1), completePartial)
		// partial 2、3 缺。
		if got := deepResumeSubPhase(ws, fid, 1, parallelCfg(3)); got != protocol.SubPhaseReviewing {
			t.Errorf("subPhase = %q, want reviewing", got)
		}
	})

	t.Run("all partials present → synthesizing", func(t *testing.T) {
		ws := &protocol.Workspace{Root: t.TempDir()}
		const fid = "F-sub"
		for i := 1; i <= 3; i++ {
			writeRoundFile(t, ws, fid, 1, deepReviewPartialName(i), completePartial)
		}
		if got := deepResumeSubPhase(ws, fid, 1, parallelCfg(3)); got != protocol.SubPhaseSynthesizing {
			t.Errorf("subPhase = %q, want synthesizing", got)
		}
	})

	t.Run("single-agent mode → reviewing", func(t *testing.T) {
		ws := &protocol.Workspace{Root: t.TempDir()}
		const fid = "F-sub"
		// 無 parallel 設定（want<=1）→ 無 partial 概念，回 reviewing。
		if got := deepResumeSubPhase(ws, fid, 1, protocol.Config{}); got != protocol.SubPhaseReviewing {
			t.Errorf("subPhase = %q, want reviewing", got)
		}
	})
}

// TestSmartResume_DeepSubPhase 驗證 deep-review report 不完整時，smartResumePhase 依 partial
// 狀態回傳正確的 (phase, role, subPhase)。
func TestSmartResume_DeepSubPhase(t *testing.T) {
	// 先把 round 跑到 deep-review（coding/review/verify 都 PASS），但 deep-review-report 缺。
	seedToDeepReview := func(t *testing.T, ws *protocol.Workspace, fid string) {
		writeRoundFile(t, ws, fid, 1, protocol.CoderReport, completeCoderReport)
		writeRoundFile(t, ws, fid, 1, protocol.ReviewReport, reviewPassReport)
		writeRoundFile(t, ws, fid, 1, protocol.TestReport, "# Test\nok\n")
		writeRoundFile(t, ws, fid, 1, protocol.VerifyFile, verifyPassedJSON)
	}

	t.Run("missing partials → reviewing", func(t *testing.T) {
		ws := &protocol.Workspace{Root: t.TempDir()}
		const fid = "F-srsub"
		seedToDeepReview(t, ws, fid)
		writeRoundFile(t, ws, fid, 1, deepReviewPartialName(1), completePartial)
		phase, role, sub := smartResumePhase(ws, fid, 1, parallelCfg(3))
		if phase != protocol.PhaseDeepReviewing || role != protocol.RoleDeepReviewer || sub != protocol.SubPhaseReviewing {
			t.Errorf("got (%s, %s, %s), want (deep-reviewing, deep-reviewer, reviewing)", phase, role, sub)
		}
	})

	t.Run("all partials present → synthesizing", func(t *testing.T) {
		ws := &protocol.Workspace{Root: t.TempDir()}
		const fid = "F-srsub"
		seedToDeepReview(t, ws, fid)
		for i := 1; i <= 3; i++ {
			writeRoundFile(t, ws, fid, 1, deepReviewPartialName(i), completePartial)
		}
		phase, role, sub := smartResumePhase(ws, fid, 1, parallelCfg(3))
		if phase != protocol.PhaseDeepReviewing || role != protocol.RoleSynthesizer || sub != protocol.SubPhaseSynthesizing {
			t.Errorf("got (%s, %s, %s), want (deep-reviewing, synthesizer, synthesizing)", phase, role, sub)
		}
	})

	t.Run("deep-review FAIL → amending, no subPhase", func(t *testing.T) {
		ws := &protocol.Workspace{Root: t.TempDir()}
		const fid = "F-srsub"
		seedFullRoundDeepFail(t, ws, fid, 1)
		phase, role, sub := smartResumePhase(ws, fid, 1, parallelCfg(3))
		if phase != protocol.PhaseAmending || role != protocol.RoleCoder || sub != "" {
			t.Errorf("got (%s, %s, %q), want (amending, coder, \"\")", phase, role, sub)
		}
	})
}

// TestWriteState_ClearsSubPhaseOutsideDeepReview 驗證 WriteState 的不變式：phase 非 deep-reviewing
// 時，殘留的 SubPhase 一律被清空（各退出路徑的單一收斂點）。
func TestWriteState_ClearsSubPhaseOutsideDeepReview(t *testing.T) {
	ws := &protocol.Workspace{Root: t.TempDir()}
	const fid = "F-clear"
	if err := ws.InitFeatureDir(fid); err != nil {
		t.Fatalf("init: %v", err)
	}

	// 模擬殘留：phase 已推進 accepting，但 SubPhase 仍帶 fixing。
	s := protocol.State{FeatureID: fid, Phase: protocol.PhaseAccepting, Role: protocol.RoleAcceptor, SubPhase: protocol.SubPhaseFixing, Round: 1}
	if err := ws.WriteState(fid, s); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := ws.ReadState(fid)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got.SubPhase != "" {
		t.Errorf("SubPhase = %q, want cleared", got.SubPhase)
	}

	// 反向：phase 仍在 deep-reviewing 時，SubPhase 保留（resume 需據此判斷中斷點）。
	s2 := protocol.State{FeatureID: fid, Phase: protocol.PhaseDeepReviewing, Role: protocol.RoleSynthesizer, SubPhase: protocol.SubPhaseSynthesizing, Round: 1}
	if err := ws.WriteState(fid, s2); err != nil {
		t.Fatalf("write: %v", err)
	}
	got2, err := ws.ReadState(fid)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got2.SubPhase != protocol.SubPhaseSynthesizing {
		t.Errorf("SubPhase = %q, want synthesizing (preserved in deep-reviewing)", got2.SubPhase)
	}
}
