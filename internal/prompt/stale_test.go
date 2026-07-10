package prompt

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	feat "github.com/ggwhite/4x/internal/feature"
	"github.com/ggwhite/4x/internal/protocol"
)

// staleBase 是測試用固定基準時間；old = staleBase，new = staleBase.Add(time.Hour)。
var staleBase = time.Unix(1_700_000_000, 0)

// writeFileAt 建立檔案並用 os.Chtimes 精準設定其 mtime。
func writeFileAt(t *testing.T, path string, mod time.Time) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, mod, mod); err != nil {
		t.Fatal(err)
	}
}

// TestDetectStaleReport 涵蓋 Design Rulings 全部分支：不存在、過期、未過期、相等、
// 僅 fixer 較新、無基準、非目標角色、以及三個目標角色的正向映射與 acceptor 目錄反向 guard。
func TestDetectStaleReport(t *testing.T) {
	const featureID = "F172-detect"
	oldT := staleBase
	newT := staleBase.Add(time.Hour)

	// AC-1：目標角色但報告不存在（首次執行）→ nil。
	t.Run("report-not-exist", func(t *testing.T) {
		ws := newTestWorkspace(t)
		writeFileAt(t, filepath.Join(ws.RoundDir(featureID, 1), protocol.CoderReport), newT)
		if got := detectStaleReport(ws, featureID, protocol.RoleReviewer, 1); got != nil {
			t.Fatalf("expected nil when report absent, got %+v", got)
		}
	})

	// AC-2：報告存在且 coder-report 較新 → 非 nil，欄位正確且 ModTime 為 RFC3339。
	t.Run("stale-reviewer", func(t *testing.T) {
		ws := newTestWorkspace(t)
		roundDir := ws.RoundDir(featureID, 1)
		writeFileAt(t, filepath.Join(roundDir, protocol.ReviewReport), oldT)
		writeFileAt(t, filepath.Join(roundDir, protocol.CoderReport), newT)
		got := detectStaleReport(ws, featureID, protocol.RoleReviewer, 1)
		if got == nil {
			t.Fatal("expected non-nil for stale reviewer report")
		}
		if got.ReportName != protocol.ReviewReport {
			t.Errorf("ReportName = %q, want %q", got.ReportName, protocol.ReviewReport)
		}
		if got.CodeChangeName != protocol.CoderReport {
			t.Errorf("CodeChangeName = %q, want %q", got.CodeChangeName, protocol.CoderReport)
		}
		if got.ReportModTime != oldT.Format(time.RFC3339) {
			t.Errorf("ReportModTime = %q, want %q", got.ReportModTime, oldT.Format(time.RFC3339))
		}
		if got.CodeChangeModTime != newT.Format(time.RFC3339) {
			t.Errorf("CodeChangeModTime = %q, want %q", got.CodeChangeModTime, newT.Format(time.RFC3339))
		}
	})

	// AC-3：報告晚於 code-change → nil。
	t.Run("not-stale", func(t *testing.T) {
		ws := newTestWorkspace(t)
		roundDir := ws.RoundDir(featureID, 1)
		writeFileAt(t, filepath.Join(roundDir, protocol.ReviewReport), newT)
		writeFileAt(t, filepath.Join(roundDir, protocol.CoderReport), oldT)
		if got := detectStaleReport(ws, featureID, protocol.RoleReviewer, 1); got != nil {
			t.Fatalf("expected nil when report newer, got %+v", got)
		}
	})

	// AC-3：mtime 相等（嚴格大於）→ nil。
	t.Run("mtime-equal", func(t *testing.T) {
		ws := newTestWorkspace(t)
		roundDir := ws.RoundDir(featureID, 1)
		writeFileAt(t, filepath.Join(roundDir, protocol.ReviewReport), oldT)
		writeFileAt(t, filepath.Join(roundDir, protocol.CoderReport), oldT)
		if got := detectStaleReport(ws, featureID, protocol.RoleReviewer, 1); got != nil {
			t.Fatalf("expected nil when mtime equal, got %+v", got)
		}
	})

	// AC-4：僅 fixer-report 存在且較新 → 非 nil 且 CodeChangeName = fixer-report.md。
	t.Run("only-fixer-newer", func(t *testing.T) {
		ws := newTestWorkspace(t)
		roundDir := ws.RoundDir(featureID, 1)
		writeFileAt(t, filepath.Join(roundDir, protocol.ReviewReport), oldT)
		writeFileAt(t, filepath.Join(roundDir, protocol.FixerReport), newT)
		got := detectStaleReport(ws, featureID, protocol.RoleReviewer, 1)
		if got == nil {
			t.Fatal("expected non-nil when only fixer-report newer")
		}
		if got.CodeChangeName != protocol.FixerReport {
			t.Errorf("CodeChangeName = %q, want %q", got.CodeChangeName, protocol.FixerReport)
		}
	})

	// AC-4：無任何 code-change artifact → nil（無基準不觸發）。
	t.Run("no-code-change-artifact", func(t *testing.T) {
		ws := newTestWorkspace(t)
		writeFileAt(t, filepath.Join(ws.RoundDir(featureID, 1), protocol.ReviewReport), oldT)
		if got := detectStaleReport(ws, featureID, protocol.RoleReviewer, 1); got != nil {
			t.Fatalf("expected nil without code-change baseline, got %+v", got)
		}
	})

	// AC-5：非目標角色即使報告 + 較新 code-change 同時存在 → 一律 nil。
	t.Run("non-target-roles", func(t *testing.T) {
		ws := newTestWorkspace(t)
		roundDir := ws.RoundDir(featureID, 1)
		writeFileAt(t, filepath.Join(roundDir, protocol.ReviewReport), oldT)
		writeFileAt(t, filepath.Join(roundDir, protocol.CoderReport), newT)
		for _, role := range []protocol.Role{
			protocol.RoleCoder, protocol.RoleFixer, protocol.RoleDesigner, protocol.RoleDesignReviewer,
		} {
			if got := detectStaleReport(ws, featureID, role, 1); got != nil {
				t.Errorf("role %s: expected nil, got %+v", role, got)
			}
		}
	})

	// AC-10：tester → test-report.md（round 目錄）。
	t.Run("stale-tester", func(t *testing.T) {
		ws := newTestWorkspace(t)
		roundDir := ws.RoundDir(featureID, 1)
		writeFileAt(t, filepath.Join(roundDir, protocol.TestReport), oldT)
		writeFileAt(t, filepath.Join(roundDir, protocol.CoderReport), newT)
		got := detectStaleReport(ws, featureID, protocol.RoleTester, 1)
		if got == nil || got.ReportName != protocol.TestReport {
			t.Fatalf("tester: got %+v, want ReportName=%q", got, protocol.TestReport)
		}
	})

	// AC-10：deep-reviewer → deep-review-report.md（round 目錄）。
	t.Run("stale-deep-reviewer", func(t *testing.T) {
		ws := newTestWorkspace(t)
		roundDir := ws.RoundDir(featureID, 1)
		writeFileAt(t, filepath.Join(roundDir, protocol.DeepReviewReport), oldT)
		writeFileAt(t, filepath.Join(roundDir, protocol.CoderReport), newT)
		got := detectStaleReport(ws, featureID, protocol.RoleDeepReviewer, 1)
		if got == nil || got.ReportName != protocol.DeepReviewReport {
			t.Fatalf("deep-reviewer: got %+v, want ReportName=%q", got, protocol.DeepReviewReport)
		}
	})

	// AC-10：acceptor → final-report.md 建在 ws.FeatureDir（非 round 目錄）→ 觸發。
	t.Run("stale-acceptor", func(t *testing.T) {
		ws := newTestWorkspace(t)
		writeFileAt(t, filepath.Join(ws.FeatureDir(featureID), protocol.FinalReport), oldT)
		writeFileAt(t, filepath.Join(ws.RoundDir(featureID, 1), protocol.CoderReport), newT)
		got := detectStaleReport(ws, featureID, protocol.RoleAcceptor, 1)
		if got == nil || got.ReportName != protocol.FinalReport {
			t.Fatalf("acceptor: got %+v, want ReportName=%q", got, protocol.FinalReport)
		}
	})

	// AC-10 反向 guard：final-report.md 僅放在 round 目錄（FeatureDir 無此檔）→ acceptor 回傳 nil，
	// 釘死「acceptor 讀 FeatureDir 而非 round 目錄」的目錄解析分支。
	t.Run("acceptor-wrong-dir-guard", func(t *testing.T) {
		ws := newTestWorkspace(t)
		roundDir := ws.RoundDir(featureID, 1)
		writeFileAt(t, filepath.Join(roundDir, protocol.FinalReport), oldT)
		writeFileAt(t, filepath.Join(roundDir, protocol.CoderReport), newT)
		if got := detectStaleReport(ws, featureID, protocol.RoleAcceptor, 1); got != nil {
			t.Fatalf("acceptor should read FeatureDir not round dir; got %+v", got)
		}
	})
}

// staleWarningMarker 是 template 警示區塊的標題字串，測試據此判斷 prompt 是否含警示。
const staleWarningMarker = "STALE REPORT DETECTED"

// TestGenerateStaleWarning 驗證真實 template render：過期情境含警示區塊與四欄位值（AC-6）、
// 未過期/首次執行情境不含警示區塊（AC-7）。
func TestGenerateStaleWarning(t *testing.T) {
	const featureID = "F172-generate"
	oldT := staleBase
	newT := staleBase.Add(time.Hour)

	// AC-6：過期情境 → prompt 含警示標題與四個欄位值。
	t.Run("stale-scenario", func(t *testing.T) {
		ws := newTestWorkspace(t)
		if err := ws.InitFeatureDir(featureID); err != nil {
			t.Fatal(err)
		}
		roundDir := ws.RoundDir(featureID, 1)
		writeFileAt(t, filepath.Join(roundDir, protocol.ReviewReport), oldT)
		writeFileAt(t, filepath.Join(roundDir, protocol.CoderReport), newT)

		info := detectStaleReport(ws, featureID, protocol.RoleReviewer, 1)
		if info == nil {
			t.Fatal("precondition: expected detectStaleReport non-nil")
		}
		ctx := &Context{Ws: ws, RunnerWs: ws, Feature: feat.Feature{ID: featureID, Name: "Test"}, Cfg: protocol.Config{}}
		got := renderRole(t, ctx, protocol.RoleReviewer)
		if !strings.Contains(got, staleWarningMarker) {
			t.Errorf("prompt missing stale warning marker %q\n---\n%s", staleWarningMarker, got)
		}
		for _, want := range []string{
			info.ReportName, info.ReportModTime, info.CodeChangeName, info.CodeChangeModTime,
		} {
			if !strings.Contains(got, want) {
				t.Errorf("prompt missing field value %q", want)
			}
		}
	})

	// AC-7：首次執行（報告不存在）→ prompt 不含警示區塊。
	t.Run("first-run-scenario", func(t *testing.T) {
		ws := newTestWorkspace(t)
		if err := ws.InitFeatureDir(featureID); err != nil {
			t.Fatal(err)
		}
		// 僅有 code-change artifact、無 review-report → detectStaleReport 回 nil。
		writeFileAt(t, filepath.Join(ws.RoundDir(featureID, 1), protocol.CoderReport), newT)
		ctx := &Context{Ws: ws, RunnerWs: ws, Feature: feat.Feature{ID: featureID, Name: "Test"}, Cfg: protocol.Config{}}
		got := renderRole(t, ctx, protocol.RoleReviewer)
		if strings.Contains(got, staleWarningMarker) {
			t.Errorf("first-run prompt should NOT contain stale warning marker\n---\n%s", got)
		}
	})
}
