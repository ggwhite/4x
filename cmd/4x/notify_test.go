package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/ggwhite/4x/internal/protocol"
)

func TestRunOutcome(t *testing.T) {
	tests := []struct {
		name      string
		state     protocol.State
		loopErr   error
		wantLevel string
		wantBody  string
	}{
		{
			name:      "done is success",
			state:     protocol.State{Phase: protocol.PhaseDone},
			wantLevel: protocol.NotifySuccess,
			wantBody:  "完成：done",
		},
		{
			name:      "pending-review is success",
			state:     protocol.State{Phase: protocol.PhasePendingReview},
			wantLevel: protocol.NotifySuccess,
			wantBody:  "完成：pending-review",
		},
		{
			name:      "interrupted is warning",
			state:     protocol.State{Phase: protocol.PhaseCoding, StopReason: "interrupted"},
			wantLevel: protocol.NotifyWarning,
			wantBody:  "中斷：interrupted",
		},
		{
			name:      "needs-attention is error with stop reason",
			state:     protocol.State{Phase: protocol.PhaseNeedsAttention, StopReason: "scope violation"},
			wantLevel: protocol.NotifyError,
			wantBody:  "失敗：scope violation",
		},
		{
			name:      "error falls back to loopErr",
			state:     protocol.State{Phase: protocol.PhaseBlocked},
			loopErr:   errors.New("hard failure"),
			wantLevel: protocol.NotifyError,
			wantBody:  "失敗：hard failure",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			level, title, body := runOutcome("F076", tt.state, tt.loopErr)
			if level != tt.wantLevel {
				t.Errorf("level = %q, want %q", level, tt.wantLevel)
			}
			if body != tt.wantBody {
				t.Errorf("body = %q, want %q", body, tt.wantBody)
			}
			if !strings.Contains(title, "F076") {
				t.Errorf("title = %q, want it to contain feature id", title)
			}
		})
	}
}

func TestQuotePowerShell(t *testing.T) {
	if got := quotePowerShell("a'b"); got != "'a''b'" {
		t.Errorf("quotePowerShell = %q, want 'a''b'", got)
	}
}

func TestEscapeQuotes(t *testing.T) {
	if got := escapeQuotes(`a"b\c`); got != `a\"b\\c` {
		t.Errorf("escapeQuotes = %q, want %q", got, `a\"b\\c`)
	}
}
