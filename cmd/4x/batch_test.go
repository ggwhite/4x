package main

import (
	"testing"

	"github.com/ggwhite/4x/internal/feature"
	"github.com/ggwhite/4x/internal/protocol"
)

// F094：batch 跑多個 feature 時，各 feature 依自身 YAML profile 欄位解析出不同 profile
// （per-feature 套用）。模擬 batch 路徑：LoadFeature 帶出 Profile → 以空 override 呼叫
// ResolveProfile（batch 無 batch 層級 --profile，s.Profile 對全新 feature 為空）。
func TestBatch_PerFeatureProfileResolution(t *testing.T) {
	ws := setupLoopWorkspace(t, "feat-quick")
	cfg, err := ws.ReadConfig()
	if err != nil {
		t.Fatal(err)
	}
	// 兩個 feature priority 相同（皆 0，平常會 auto-select full），靠各自 profile 欄位區分。
	if err := ws.SaveFeature(feature.Feature{ID: "feat-quick", Name: "quick", Status: "not-started", Priority: intPtrCLI(0), Profile: "quick"}); err != nil {
		t.Fatal(err)
	}
	if err := ws.SaveFeature(feature.Feature{ID: "feat-full", Name: "full", Status: "not-started", Priority: intPtrCLI(0), Profile: "full"}); err != nil {
		t.Fatal(err)
	}

	resolve := func(id string) string {
		f, err := ws.LoadFeature(id)
		if err != nil {
			t.Fatalf("LoadFeature %s: %v", id, err)
		}
		name, _, err := protocol.ResolveProfile(cfg, f, "")
		if err != nil {
			t.Fatalf("ResolveProfile %s: %v", id, err)
		}
		return name
	}

	if got := resolve("feat-quick"); got != "quick" {
		t.Errorf("feat-quick resolved to %q, want quick", got)
	}
	if got := resolve("feat-full"); got != "full" {
		t.Errorf("feat-full resolved to %q, want full", got)
	}
}

func intPtrCLI(i int) *int { return &i }
