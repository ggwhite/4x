package advisor

import (
	"strings"
	"testing"

	"github.com/ggwhite/4x/internal/feature"
	"github.com/ggwhite/4x/internal/protocol"
)

func ptr[T any](v T) *T { return &v }

// AC-2：nil config 全預設；明確設 0 的 *int 欄位保留 0。
func TestResolveConfig(t *testing.T) {
	rc := ResolveConfig(protocol.Config{})
	if !rc.Enabled {
		t.Errorf("Enabled = false, want true (default)")
	}
	if rc.SubtaskPoints != 2 || rc.RepoPoints != 3 || rc.DescBucketRunes != 300 ||
		rc.PriorityWeight != 2 || rc.RefactorPoints != -4 ||
		rc.HeavyMinScore != 8 || rc.MediumMinScore != 3 {
		t.Errorf("numeric defaults wrong: %+v", rc)
	}
	if rc.HeavyProfile != "full" || rc.MediumProfile != "normal" || rc.LightProfile != "quick" {
		t.Errorf("profile name defaults wrong: %q/%q/%q", rc.HeavyProfile, rc.MediumProfile, rc.LightProfile)
	}
	if len(rc.RefactorKeywords) != len(DefaultRefactorKeywords()) {
		t.Errorf("RefactorKeywords = %v, want default set", rc.RefactorKeywords)
	}

	// DR-4：明確設 0 的 *int 欄位必須保留 0（不被預設吃掉）。
	cfg := protocol.Config{ProfileAdvisor: &protocol.ProfileAdvisorConfig{
		SubtaskPoints:  ptr(0),
		RefactorPoints: ptr(0),
		RepoPoints:     ptr(0),
	}}
	rc0 := ResolveConfig(cfg)
	if rc0.SubtaskPoints != 0 {
		t.Errorf("SubtaskPoints = %d, want 0 (explicit)", rc0.SubtaskPoints)
	}
	if rc0.RefactorPoints != 0 {
		t.Errorf("RefactorPoints = %d, want 0 (explicit)", rc0.RefactorPoints)
	}
	if rc0.RepoPoints != 0 {
		t.Errorf("RepoPoints = %d, want 0 (explicit)", rc0.RepoPoints)
	}
	// 未設定的欄位仍套預設。
	if rc0.PriorityWeight != 2 {
		t.Errorf("PriorityWeight = %d, want 2 (default)", rc0.PriorityWeight)
	}

	// Enabled 明確 false 保留。
	rcOff := ResolveConfig(protocol.Config{ProfileAdvisor: &protocol.ProfileAdvisorConfig{Enabled: ptr(false)}})
	if rcOff.Enabled {
		t.Errorf("Enabled = true, want false (explicit)")
	}
}

// AC-3：Extract 正確萃取訊號，含 CJK rune 計數與 refactor 關鍵字大小寫命中。
func TestExtract(t *testing.T) {
	rc := ResolveConfig(protocol.Config{})

	// 純 10 個中文字 → DescRunes==10（非 30 byte）。
	f := feature.Feature{
		Name:        "測試",
		Description: "一二三四五六七八九十",
		Repos:       []string{"a", "b"},
		Subtasks:    []feature.Subtask{{ID: "1"}, {ID: "2"}, {ID: "3"}},
		Priority:    ptr(2),
	}
	sig := Extract(f, rc)
	if sig.DescRunes != 10 {
		t.Errorf("DescRunes = %d, want 10 (rune count, not byte)", sig.DescRunes)
	}
	if sig.SubtaskCount != 3 {
		t.Errorf("SubtaskCount = %d, want 3", sig.SubtaskCount)
	}
	if sig.RepoCount != 2 {
		t.Errorf("RepoCount = %d, want 2", sig.RepoCount)
	}
	if sig.Priority == nil || *sig.Priority != 2 {
		t.Errorf("Priority = %v, want 2", sig.Priority)
	}
	if sig.RefactorKeyword != "" {
		t.Errorf("RefactorKeyword = %q, want empty", sig.RefactorKeyword)
	}

	// refactor 關鍵字大小寫混寫 "God Object 拆分" 應命中。
	f2 := feature.Feature{Name: "God Object 拆分", Description: "把大檔拆開"}
	sig2 := Extract(f2, rc)
	if sig2.RefactorKeyword == "" {
		t.Errorf("RefactorKeyword empty, want a hit on 'God Object 拆分'")
	}
}

// AC-4：Recommend 計分與映射；含 DescBucketRunes=0 不 panic。
func TestRecommend(t *testing.T) {
	cfg := protocol.Config{}

	// (a) 多 subtask + 多 repo → full
	fHeavy := feature.Feature{
		Name:     "big feature",
		Repos:    []string{"a", "b", "c"},
		Subtasks: []feature.Subtask{{ID: "1"}, {ID: "2"}, {ID: "3"}},
		Priority: ptr(0),
	}
	// subtask 3*2=6, repo (3-1)*3=6, priority 2*(2-0)=4 → 16 >= 8 → full
	rec, ok := Recommend(cfg, fHeavy)
	if !ok || rec.Profile != "full" {
		t.Errorf("(a) got (%q,%v), want full/true; score=%d", rec.Profile, ok, rec.Score)
	}

	// (b) 中量 → normal（score 在 [3,8)）
	fMed := feature.Feature{
		Name:     "medium",
		Subtasks: []feature.Subtask{{ID: "1"}, {ID: "2"}},
		Priority: ptr(2),
	}
	// subtask 2*2=4, priority 2*(2-2)=0 → 4 → normal
	recM, okM := Recommend(cfg, fMed)
	if !okM || recM.Profile != "normal" {
		t.Errorf("(b) got (%q,%v), want normal/true; score=%d", recM.Profile, okM, recM.Score)
	}

	// (c) 單一小 feature → quick（score < 3）
	fLight := feature.Feature{
		Name:     "tiny",
		Priority: ptr(3),
	}
	// priority 2*(2-3)=-2 → quick
	recL, okL := Recommend(cfg, fLight)
	if !okL || recL.Profile != "quick" {
		t.Errorf("(c) got (%q,%v), want quick/true; score=%d", recL.Profile, okL, recL.Score)
	}

	// (d) DescBucketRunes=0 → desc 項為 0 且不 panic
	cfg0 := protocol.Config{ProfileAdvisor: &protocol.ProfileAdvisorConfig{DescBucketRunes: ptr(0)}}
	fDesc := feature.Feature{Name: "x", Description: strings.Repeat("字", 1000), Priority: ptr(1)}
	recD, okD := Recommend(cfg0, fDesc)
	if !okD {
		t.Fatalf("(d) DescBucketRunes=0 returned ok=false")
	}
	// desc 貢獻應為 0：只剩 priority 2*(2-1)=2 → quick
	if recD.Score != 2 {
		t.Errorf("(d) score = %d, want 2 (desc contributes 0)", recD.Score)
	}
}

// AC-5：refactor 關鍵字命中壓低分數，profile 不重於不含者。
func TestRecommend_Refactor(t *testing.T) {
	cfg := protocol.Config{}
	base := feature.Feature{
		Name:     "task",
		Subtasks: []feature.Subtask{{ID: "1"}, {ID: "2"}, {ID: "3"}},
		Priority: ptr(1),
	}
	withKW := base
	withKW.Description = "這是一個大重構"

	recPlain, _ := Recommend(cfg, base)
	recKW, _ := Recommend(cfg, withKW)

	if recKW.Score >= recPlain.Score {
		t.Errorf("refactor score %d not lower than plain %d", recKW.Score, recPlain.Score)
	}
}

// AC-6：停用 / 建議 profile 不存在時皆回 ok=false。
func TestRecommend_Disabled_And_UnknownProfile(t *testing.T) {
	f := feature.Feature{Name: "x", Priority: ptr(1)}

	// 停用
	cfgOff := protocol.Config{ProfileAdvisor: &protocol.ProfileAdvisorConfig{Enabled: ptr(false)}}
	if _, ok := Recommend(cfgOff, f); ok {
		t.Errorf("disabled advisor returned ok=true")
	}

	// 建議 profile 名稱不存在（把 HeavyProfile 設成不存在名字並命中 heavy tier）
	cfgBad := protocol.Config{ProfileAdvisor: &protocol.ProfileAdvisorConfig{
		HeavyProfile:  "nonexistent",
		HeavyMinScore: ptr(0), // 任何 score 都命中 heavy tier
	}}
	if _, ok := Recommend(cfgBad, f); ok {
		t.Errorf("unknown profile returned ok=true, want false (DR-5)")
	}
}
