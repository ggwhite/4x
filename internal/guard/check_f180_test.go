package guard

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ggwhite/4x/internal/protocol"
)

// setupF180Repo 建 git repo 於 temp root、commit 指定 tracked 檔（至少一個 commit 以免 DR-5 跳過），
// 回傳 workspace 與 featureID，供 checkEvidenceTracked 測試共用。
func setupF180Repo(t *testing.T, tracked ...string) (*protocol.Workspace, string) {
	t.Helper()
	root := t.TempDir()
	runGitGuard(t, root, "init")
	runGitGuard(t, root, "config", "user.email", "t@t.io")
	runGitGuard(t, root, "config", "user.name", "t")
	for _, p := range tracked {
		full := filepath.Join(root, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("package x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	runGitGuard(t, root, "add", ".")
	runGitGuard(t, root, "commit", "-m", "init")
	return &protocol.Workspace{Root: root}, "feat-f180"
}

func f180Evidence(acID string, lines ...string) protocol.VerifyEvidence {
	return protocol.VerifyEvidence{ACResults: []protocol.ACEvidence{{ID: acID, Evidence: lines}}}
}

func f180WarnsHas(warns []string, sub string) bool {
	for _, w := range warns {
		if strings.Contains(w, sub) {
			return true
		}
	}
	return false
}

// TestF180EvidenceTrackedWarnsUntracked：untracked scope 路徑 → WARN，且 Pass/RetryableErrors 不變（DR-3）。
func TestF180EvidenceTrackedWarnsUntracked(t *testing.T) {
	ws, fid := setupF180Repo(t, "internal/tracked.go")
	r := &CheckResult{Pass: true}
	checkEvidenceTracked(ws, fid, f180Evidence("AC-1", "grep hit in internal/foo.go"), r)
	if !f180WarnsHas(r.Warns, "internal/foo.go") {
		t.Errorf("expected WARN for untracked internal/foo.go, got %v", r.Warns)
	}
	if !r.Pass {
		t.Error("Pass must remain true (WARN-only, DR-3)")
	}
	if r.RetryableErrors != 0 {
		t.Errorf("RetryableErrors must stay 0, got %d", r.RetryableErrors)
	}
}

// TestF180EvidenceTrackedNoWarnTracked：tracked 路徑不觸發 WARN（AC-5）。
func TestF180EvidenceTrackedNoWarnTracked(t *testing.T) {
	ws, fid := setupF180Repo(t, "internal/foo.go")
	r := &CheckResult{Pass: true}
	checkEvidenceTracked(ws, fid, f180Evidence("AC-1", "$ go test ./internal/foo.go → PASS"), r)
	if f180WarnsHas(r.Warns, "internal/foo.go") {
		t.Errorf("tracked path must not WARN, got %v", r.Warns)
	}
	if !r.Pass {
		t.Error("Pass must remain true")
	}
}

// TestF180EvidenceTrackedAllowlistAndToken：allowlist 產物與 4x-lint:allow token 豁免（AC-6）。
func TestF180EvidenceTrackedAllowlistAndToken(t *testing.T) {
	tests := []struct {
		name     string
		evidence string
		path     string
	}{
		{"allowlist-bin", "produced bin/4x binary", "bin/4x"},
		{"allowlist-generated", "referenced internal/foo/zz_generated.go", "zz_generated.go"},
		{"lint-allow-token", "grep internal/foo.go x 4x-lint:allow", "internal/foo.go"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ws, fid := setupF180Repo(t, "internal/tracked.go")
			r := &CheckResult{Pass: true}
			checkEvidenceTracked(ws, fid, f180Evidence("AC-1", tt.evidence), r)
			if f180WarnsHas(r.Warns, tt.path) {
				t.Errorf("%s: must not WARN for %q, got %v", tt.name, tt.path, r.Warns)
			}
		})
	}
}

// TestF180EvidenceTrackedAbsolutePath：證據引用絕對路徑時，tracked 檔（含 :行:列）不 WARN、
// untracked 檔仍 WARN——驗證絕對路徑經 filepath.Rel 正規化為 repo-relative 後再比對（review 回饋）。
func TestF180EvidenceTrackedAbsolutePath(t *testing.T) {
	ws, fid := setupF180Repo(t, "internal/foo.go")
	trackedAbs := filepath.Join(ws.Root, "internal/foo.go") + ":42:3"
	untrackedAbs := filepath.Join(ws.Root, "internal/bar.go") + ":10"
	r := &CheckResult{Pass: true}
	checkEvidenceTracked(ws, fid, f180Evidence("AC-1",
		"grep hit "+trackedAbs, "grep hit "+untrackedAbs), r)
	if f180WarnsHas(r.Warns, "internal/foo.go") {
		t.Errorf("tracked absolute path must not WARN, got %v", r.Warns)
	}
	if !f180WarnsHas(r.Warns, "internal/bar.go") {
		t.Errorf("expected WARN for untracked absolute internal/bar.go, got %v", r.Warns)
	}
	if !r.Pass {
		t.Error("Pass must remain true (WARN-only, DR-3)")
	}
}

// TestF180EvidenceTrackedEmptyTrackedSetSkip：tracked 聯集為空（非 git）→ 整段跳過（DR-5）。
func TestF180EvidenceTrackedEmptyTrackedSetSkip(t *testing.T) {
	ws := &protocol.Workspace{Root: t.TempDir()} // 非 git 目錄 → TrackedPaths 回空
	r := &CheckResult{Pass: true}
	checkEvidenceTracked(ws, "feat-f180", f180Evidence("AC-1", "reference internal/foo.go"), r)
	if len(r.Warns) != 0 {
		t.Errorf("empty tracked set must skip (no WARN), got %v", r.Warns)
	}
	if !r.Pass {
		t.Error("Pass must remain true")
	}
}

// TestF180BuildTrackedRootsPartialFailure：多 root 情境下，一個 root 正常、另一個 root
// 的 git 查詢失敗（非 git 目錄）時，只應略過失敗的那個 root，不能讓正常 root 的結果被
// 拖累整段跳過，也不能讓失敗 root 下的路徑被誤判為 untracked（round-3 review DR-5 finding）。
func TestF180BuildTrackedRootsPartialFailure(t *testing.T) {
	goodWs, _ := setupF180Repo(t, "internal/foo.go")
	badRoot := t.TempDir() // 非 git 目錄 → TrackedPaths 回空

	roots := buildTrackedRoots([]string{goodWs.Root, badRoot})

	if len(roots) != 1 {
		t.Fatalf("expected exactly 1 usable root (bad root skipped), got %d: %v", len(roots), roots)
	}
	if roots[0].root != goodWs.Root {
		t.Errorf("expected surviving root to be the good repo %q, got %q", goodWs.Root, roots[0].root)
	}
	if !roots[0].tracked["internal/foo.go"] {
		t.Errorf("expected good root's tracked set to contain internal/foo.go, got %v", roots[0].tracked)
	}
}
