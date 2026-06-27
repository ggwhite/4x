package main

import (
	"testing"

	"github.com/ggwhite/4x/internal/orchestrator"
	"github.com/ggwhite/4x/internal/protocol"
)

func TestParsePhaseOverrides_Success(t *testing.T) {
	raw := []string{
		"reviewing:gemini:",   // 只覆寫 runner
		"testing::opus",       // 只覆寫 model tier
		"coding:codex:sonnet", // 兩者都覆寫
	}
	got, err := orchestrator.ParsePhaseOverrides(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d overrides, want 3", len(got))
	}
	if spec := got[protocol.PhaseReviewing]; spec.Runner != "gemini" || spec.Model != "" {
		t.Errorf("reviewing: got %+v, want runner=gemini model=\"\"", spec)
	}
	if spec := got[protocol.PhaseTesting]; spec.Runner != "" || spec.Model != "opus" {
		t.Errorf("testing: got %+v, want runner=\"\" model=opus", spec)
	}
	if spec := got[protocol.PhaseCoding]; spec.Runner != "codex" || spec.Model != "sonnet" {
		t.Errorf("coding: got %+v, want runner=codex model=sonnet", spec)
	}
}

func TestParsePhaseOverrides_Empty(t *testing.T) {
	got, err := orchestrator.ParsePhaseOverrides(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Errorf("got %v, want nil for empty input", got)
	}
}

func TestParsePhaseOverrides_Errors(t *testing.T) {
	tests := []struct {
		name string
		raw  []string
	}{
		{"missing segments", []string{"reviewing:gemini"}},
		{"non-selectable phase", []string{"init:claude:"}},
		{"both empty", []string{"coding::"}},
		{"duplicate phase", []string{"coding:codex:", "coding::opus"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := orchestrator.ParsePhaseOverrides(tt.raw); err == nil {
				t.Errorf("expected error for %q", tt.raw)
			}
		})
	}
}

// TestPhaseOverrideFlagRegistered 確認 4x run 已註冊 --phase-override flag（AC-1）。
func TestPhaseOverrideFlagRegistered(t *testing.T) {
	cmd := newRunCmd()
	if cmd.Flags().Lookup("phase-override") == nil {
		t.Error("--phase-override flag not registered on run command")
	}
}
