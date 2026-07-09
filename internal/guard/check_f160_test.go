package guard

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	feat "github.com/ggwhite/4x/internal/feature"
	"github.com/ggwhite/4x/internal/protocol"
)

// writeTaskBriefF160 寫入指定內容的 task-brief.md 到 feature dir。
func writeTaskBriefF160(t *testing.T, ws *protocol.Workspace, featureID, content string) {
	t.Helper()
	writeFile(t, filepath.Join(ws.FeatureDir(featureID), protocol.TaskBrief), content)
}

// TestF160Challenge 驗證 checkTaskBriefChallenge 對 ## Premise Challenge 段落的純結構檢查
// （AC-2/AC-3/AC-4/AC-5）。
func TestF160Challenge(t *testing.T) {
	t.Run("valid-section-passes", func(t *testing.T) { // AC-2
		ws := setupGuardWorkspace(t, "F160-valid")
		writeTaskBriefF160(t, ws, "F160-valid",
			"# Task Brief\n## Premise Challenge\n- 挑戰前提 1：spec 說 X 但程式碼是 Y。\n## Goal\ndo things\n")
		r := CheckResult{Pass: true}
		checkTaskBriefChallenge(ws, "F160-valid", &r)
		if !r.Pass {
			t.Fatalf("valid Premise Challenge section should pass, got errors: %v", r.Errors)
		}
		if hasErrContaining(r.Errors, "Premise Challenge") {
			t.Fatalf("did not expect a Premise Challenge error, got %v", r.Errors)
		}
	})

	t.Run("missing-section-fails-and-retryable", func(t *testing.T) { // AC-3
		ws := setupGuardWorkspace(t, "F160-missing")
		writeTaskBriefF160(t, ws, "F160-missing", "# Task Brief\n## Goal\ndo things\n")
		r := CheckResult{Pass: true}
		checkTaskBriefChallenge(ws, "F160-missing", &r)
		if r.Pass {
			t.Fatal("missing Premise Challenge section should fail")
		}
		if !hasErrContaining(r.Errors, "Premise Challenge") {
			t.Fatalf("expected error naming Premise Challenge, got %v", r.Errors)
		}
		if r.RetryableErrors != 1 {
			t.Fatalf("expected RetryableErrors == 1, got %d", r.RetryableErrors)
		}
	})

	t.Run("empty-section-fails", func(t *testing.T) { // AC-4
		t.Run("followed-by-next-heading", func(t *testing.T) {
			ws := setupGuardWorkspace(t, "F160-empty-a")
			writeTaskBriefF160(t, ws, "F160-empty-a", "# Task Brief\n## Premise Challenge\n\n## Goal\ndo things\n")
			r := CheckResult{Pass: true}
			checkTaskBriefChallenge(ws, "F160-empty-a", &r)
			if r.Pass {
				t.Fatal("empty Premise Challenge (heading then next heading) should fail")
			}
			if !hasErrContaining(r.Errors, "Premise Challenge") {
				t.Fatalf("expected error naming Premise Challenge, got %v", r.Errors)
			}
		})

		t.Run("followed-by-eof", func(t *testing.T) {
			ws := setupGuardWorkspace(t, "F160-empty-b")
			writeTaskBriefF160(t, ws, "F160-empty-b", "# Task Brief\n## Premise Challenge\n")
			r := CheckResult{Pass: true}
			checkTaskBriefChallenge(ws, "F160-empty-b", &r)
			if r.Pass {
				t.Fatal("empty Premise Challenge (heading then EOF) should fail")
			}
		})
	})

	t.Run("no-task-brief-no-error", func(t *testing.T) { // AC-5
		ws := setupGuardWorkspace(t, "F160-nofile")
		r := CheckResult{Pass: true}
		checkTaskBriefChallenge(ws, "F160-nofile", &r)
		if !r.Pass {
			t.Fatalf("missing task-brief.md should not be flagged by this check, got errors: %v", r.Errors)
		}
		if len(r.Errors) != 0 || r.RetryableErrors != 0 {
			t.Fatalf("expected no errors when task-brief.md absent, got errors=%v retryable=%d", r.Errors, r.RetryableErrors)
		}
	})
}

// TestF160DocResolve 驗證 ResolveDocProtectedPaths 的三種輸入（AC-7）。
func TestF160DocResolve(t *testing.T) {
	t.Run("nil-doc-guard-default", func(t *testing.T) {
		got := ResolveDocProtectedPaths(protocol.Config{})
		if len(got) != 1 || got[0] != "docs/" {
			t.Fatalf("nil DocGuard should return default [docs/], got %v", got)
		}
	})

	t.Run("empty-slice-default", func(t *testing.T) {
		got := ResolveDocProtectedPaths(protocol.Config{DocGuard: &protocol.DocGuardSettings{ProtectedPaths: []string{}}})
		if len(got) != 1 || got[0] != "docs/" {
			t.Fatalf("empty ProtectedPaths should return default [docs/], got %v", got)
		}
	})

	t.Run("custom-value-returned", func(t *testing.T) {
		got := ResolveDocProtectedPaths(protocol.Config{DocGuard: &protocol.DocGuardSettings{ProtectedPaths: []string{"spec/", "rfc/"}}})
		if len(got) != 2 || got[0] != "spec/" || got[1] != "rfc/" {
			t.Fatalf("custom ProtectedPaths should be returned verbatim, got %v", got)
		}
	})
}

// runGitF160 在 dir 執行 git 指令，失敗即 fatal。
func runGitF160(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// setupDocGitRepo 建一個真實 temp git repo：protocol.Init 寫 settings.json（帶入 cfg，供
// override 測試）、git init/config、寫入並 commit files（相對路徑 → 內容），回傳 ws 與 featureID。
func setupDocGitRepo(t *testing.T, featureID string, cfg protocol.Config, files map[string]string) *protocol.Workspace {
	t.Helper()
	root := t.TempDir()
	// ValidateConfig 要求 project.name 非空；測試只關心 DocGuard，故補一個合法名稱。
	if cfg.Project.Name == "" {
		cfg.Project.Name = "guard-test"
	}
	if err := protocol.Init(root, cfg); err != nil {
		t.Fatal(err)
	}
	ws := &protocol.Workspace{Root: root}
	if err := ws.InitFeatureDir(featureID); err != nil {
		t.Fatal(err)
	}
	if err := ws.SaveFeature(feat.Feature{ID: featureID, Name: featureID}); err != nil {
		t.Fatal(err)
	}

	runGitF160(t, root, "init")
	runGitF160(t, root, "config", "user.email", "test@test.com")
	runGitF160(t, root, "config", "user.name", "Test")

	var paths []string
	for rel, content := range files {
		full := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, rel)
	}
	runGitF160(t, root, append([]string{"add"}, paths...)...)
	runGitF160(t, root, "commit", "-m", "init")
	return ws
}

// TestF160DocDeletion 以真實 temp git repo 驗證 doc-deletion guard（AC-8~AC-12）。
func TestF160DocDeletion(t *testing.T) {
	t.Run("protected-deletion-flagged", func(t *testing.T) { // AC-8, AC-12
		ws := setupDocGitRepo(t, "F160-del", protocol.Config{}, map[string]string{"docs/foo.md": "hello\n"})
		if err := os.Remove(filepath.Join(ws.Root, "docs/foo.md")); err != nil {
			t.Fatal(err)
		}
		r := CheckResult{Pass: true}
		checkDocDeletion(ws, "F160-del", nil, &r)
		if r.Pass {
			t.Fatal("deleting protected docs/foo.md should fail")
		}
		if !hasErrContaining(r.Errors, "docs/foo.md") {
			t.Fatalf("expected error naming docs/foo.md, got %v", r.Errors)
		}
		if r.RetryableErrors != 0 { // AC-12: hard failure, not retryable
			t.Fatalf("doc-deletion must not increment RetryableErrors, got %d", r.RetryableErrors)
		}
	})

	t.Run("rename-not-flagged", func(t *testing.T) { // AC-9
		ws := setupDocGitRepo(t, "F160-mv", protocol.Config{}, map[string]string{"docs/foo.md": "hello\n"})
		if err := os.MkdirAll(filepath.Join(ws.Root, "docs/archive"), 0o755); err != nil {
			t.Fatal(err)
		}
		runGitF160(t, ws.Root, "mv", "docs/foo.md", "docs/archive/foo.md")
		r := CheckResult{Pass: true}
		checkDocDeletion(ws, "F160-mv", nil, &r)
		if !r.Pass {
			t.Fatalf("git mv (archive/rename) should not be flagged as deletion, got errors: %v", r.Errors)
		}
	})

	t.Run("non-doc-deletion-ignored", func(t *testing.T) { // AC-10
		ws := setupDocGitRepo(t, "F160-nondoc", protocol.Config{}, map[string]string{"internal/x/y.go": "package x\n"})
		if err := os.Remove(filepath.Join(ws.Root, "internal/x/y.go")); err != nil {
			t.Fatal(err)
		}
		r := CheckResult{Pass: true}
		checkDocDeletion(ws, "F160-nondoc", nil, &r)
		if !r.Pass {
			t.Fatalf("deleting a non-protected file should not be flagged, got errors: %v", r.Errors)
		}
	})

	t.Run("doc-modification-ignored", func(t *testing.T) { // AC-10
		ws := setupDocGitRepo(t, "F160-mod", protocol.Config{}, map[string]string{"docs/foo.md": "hello\n"})
		if err := os.WriteFile(filepath.Join(ws.Root, "docs/foo.md"), []byte("hello world\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		r := CheckResult{Pass: true}
		checkDocDeletion(ws, "F160-mod", nil, &r)
		if !r.Pass {
			t.Fatalf("modifying (not deleting) a protected doc should not be flagged, got errors: %v", r.Errors)
		}
	})

	t.Run("settings-override-consumed", func(t *testing.T) { // AC-11
		cfg := protocol.Config{DocGuard: &protocol.DocGuardSettings{ProtectedPaths: []string{"spec/"}}}
		ws := setupDocGitRepo(t, "F160-override", cfg, map[string]string{
			"spec/a.md":   "spec\n",
			"docs/foo.md": "hello\n",
		})

		// 刪 spec/a.md → 覆寫後受保護 → 違規。
		if err := os.Remove(filepath.Join(ws.Root, "spec/a.md")); err != nil {
			t.Fatal(err)
		}
		r := CheckResult{Pass: true}
		checkDocDeletion(ws, "F160-override", nil, &r)
		if r.Pass || !hasErrContaining(r.Errors, "spec/a.md") {
			t.Fatalf("deleting spec/a.md under override should fail, got pass=%v errors=%v", r.Pass, r.Errors)
		}

		// 刪 docs/foo.md → 覆寫後不在保護清單 → 不違規（證明消費端讀設定值而非硬編碼預設）。
		if err := os.Remove(filepath.Join(ws.Root, "docs/foo.md")); err != nil {
			t.Fatal(err)
		}
		r2 := CheckResult{Pass: true}
		checkDocDeletion(ws, "F160-override", nil, &r2)
		if hasErrContaining(r2.Errors, "docs/foo.md") {
			t.Fatalf("under spec/ override, deleting docs/foo.md should NOT be flagged, got %v", r2.Errors)
		}
	})
}
