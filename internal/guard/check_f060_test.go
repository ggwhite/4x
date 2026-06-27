package guard

import (
	"encoding/json"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ggwhite/4x/internal/protocol"
)

// W7：verify.json passed==true 但某 command exitCode!=0 → guard fail 並指出該 command。
func TestCheckTestingToAccepting_CommandExitNonZero(t *testing.T) {
	ws := setupGuardWorkspace(t, "feat-1")
	roundDir := ws.RoundDir("feat-1", 1)
	featureDir := ws.FeatureDir("feat-1")

	ev := protocol.VerifyEvidence{
		Passed: true,
		Round:  1,
		Commands: []protocol.VerifyCommand{
			{Command: "make build", ExitCode: 0},
			{Command: "make test", ExitCode: 1},
		},
	}
	data, _ := json.Marshal(ev)
	writeFile(t, filepath.Join(roundDir, protocol.VerifyFile), string(data))
	writeFile(t, filepath.Join(roundDir, protocol.TestReport), "# Test")
	writeFile(t, filepath.Join(featureDir, protocol.FinalReport), "# Final")

	result := CheckTestingToAccepting(ws, "feat-1", 1)
	if result.Pass {
		t.Fatal("passed=true with a non-zero command exit should fail")
	}
	found := false
	for _, err := range result.Errors {
		if strings.Contains(err, "make test") && strings.Contains(err, "1") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected error mentioning the failing command, got %v", result.Errors)
	}
}

// W7 向後相容（AC-13）：passed==true 且 commands 為空 → 仍 Pass。
func TestCheckTestingToAccepting_EmptyCommandsPasses(t *testing.T) {
	ws := setupGuardWorkspace(t, "feat-1")
	roundDir := ws.RoundDir("feat-1", 1)
	featureDir := ws.FeatureDir("feat-1")

	data, _ := json.Marshal(protocol.VerifyEvidence{Passed: true, Round: 1,
		ACResults: []protocol.ACEvidence{{ID: "AC-1", Passed: true, Evidence: []string{"ok"}}}})
	writeFile(t, filepath.Join(roundDir, protocol.VerifyFile), string(data))
	writeFile(t, filepath.Join(roundDir, protocol.TestReport), "# Test")
	writeFile(t, filepath.Join(featureDir, protocol.FinalReport), "# Final")

	result := CheckTestingToAccepting(ws, "feat-1", 1)
	if !result.Pass {
		t.Fatalf("passed=true with empty commands should pass, got errors: %v", result.Errors)
	}
}

// W7：expectedExitCode 非零且實際 exit code 吻合 → Pass。
func TestCheckTestingToAccepting_ExpectedNonZeroExitPasses(t *testing.T) {
	ws := setupGuardWorkspace(t, "feat-1")
	roundDir := ws.RoundDir("feat-1", 1)
	featureDir := ws.FeatureDir("feat-1")

	expectedOne := 1
	ev := protocol.VerifyEvidence{
		Passed: true,
		Round:  1,
		Commands: []protocol.VerifyCommand{
			{Command: "make build", ExitCode: 0},
			{Command: "status __nonexistent__ --json", ExitCode: 1, ExpectedExitCode: &expectedOne},
		},
		ACResults: []protocol.ACEvidence{{ID: "AC-1", Passed: true, Evidence: []string{"ok"}}},
	}
	data, _ := json.Marshal(ev)
	writeFile(t, filepath.Join(roundDir, protocol.VerifyFile), string(data))
	writeFile(t, filepath.Join(roundDir, protocol.TestReport), "# Test")
	writeFile(t, filepath.Join(featureDir, protocol.FinalReport), "# Final")

	result := CheckTestingToAccepting(ws, "feat-1", 1)
	if !result.Pass {
		t.Fatalf("exit 1 matching expectedExitCode=1 should pass, got errors: %v", result.Errors)
	}
}

// W7：expectedExitCode 非零但實際 exit code 不符 → Fail。
func TestCheckTestingToAccepting_ExpectedNonZeroMismatchFails(t *testing.T) {
	ws := setupGuardWorkspace(t, "feat-1")
	roundDir := ws.RoundDir("feat-1", 1)
	featureDir := ws.FeatureDir("feat-1")

	expectedOne := 1
	ev := protocol.VerifyEvidence{
		Passed: true,
		Round:  1,
		Commands: []protocol.VerifyCommand{
			{Command: "should-fail-but-passed", ExitCode: 0, ExpectedExitCode: &expectedOne},
		},
	}
	data, _ := json.Marshal(ev)
	writeFile(t, filepath.Join(roundDir, protocol.VerifyFile), string(data))
	writeFile(t, filepath.Join(roundDir, protocol.TestReport), "# Test")
	writeFile(t, filepath.Join(featureDir, protocol.FinalReport), "# Final")

	result := CheckTestingToAccepting(ws, "feat-1", 1)
	if result.Pass {
		t.Fatal("exit 0 with expectedExitCode=1 should fail")
	}
}

// W7：所有 command exit 0 + passed==true → Pass。
func TestCheckTestingToAccepting_AllCommandsZeroPasses(t *testing.T) {
	ws := setupGuardWorkspace(t, "feat-1")
	roundDir := ws.RoundDir("feat-1", 1)
	featureDir := ws.FeatureDir("feat-1")

	ev := protocol.VerifyEvidence{
		Passed: true,
		Round:  1,
		Commands: []protocol.VerifyCommand{
			{Command: "make build", ExitCode: 0},
			{Command: "make test", ExitCode: 0},
		},
		ACResults: []protocol.ACEvidence{{ID: "AC-1", Passed: true, Evidence: []string{"ok"}}},
	}
	data, _ := json.Marshal(ev)
	writeFile(t, filepath.Join(roundDir, protocol.VerifyFile), string(data))
	writeFile(t, filepath.Join(roundDir, protocol.TestReport), "# Test")
	writeFile(t, filepath.Join(featureDir, protocol.FinalReport), "# Final")

	result := CheckTestingToAccepting(ws, "feat-1", 1)
	if !result.Pass {
		t.Fatalf("passed=true with all-zero commands should pass, got errors: %v", result.Errors)
	}
}

func gitInit(t *testing.T, dir string) {
	t.Helper()
	for _, args := range [][]string{
		{"init"},
		{"config", "user.email", "t@t.io"},
		{"config", "user.name", "t"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
}

// W11（AC-15）：detectChangedRepos 須偵測到 untracked 檔案所屬的 repo。
func TestDetectChangedRepos_IncludesUntracked(t *testing.T) {
	root := t.TempDir()
	gitInit(t, root)

	// 未追蹤檔案：git diff HEAD 看不到，必須靠 ls-files --others 補。
	writeFile(t, filepath.Join(root, "svc-a", "new.txt"), "hello")

	repos := detectChangedRepos(root)
	found := false
	for _, r := range repos {
		if r == "svc-a" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected svc-a (untracked) in changed repos, got %v", repos)
	}
}

// W11：tracked 變更偵測不退步（git diff HEAD 路徑仍有效）。
func TestDetectChangedRepos_IncludesTracked(t *testing.T) {
	root := t.TempDir()
	gitInit(t, root)

	writeFile(t, filepath.Join(root, "svc-b", "file.txt"), "v1")
	for _, args := range [][]string{{"add", "."}, {"commit", "-m", "init"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	// 修改已追蹤檔案 → git diff HEAD 偵測得到。
	writeFile(t, filepath.Join(root, "svc-b", "file.txt"), "v2")

	repos := detectChangedRepos(root)
	found := false
	for _, r := range repos {
		if r == "svc-b" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected svc-b (tracked change) in changed repos, got %v", repos)
	}
}
