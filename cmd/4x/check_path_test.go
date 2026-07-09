package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// run4xIO 以 os/exec 執行真實編譯出的 4x 二進位，回傳分離的 stdout / stderr 與 exit code。
// dir 為子程序 cwd（用來讓 protocol.Find 命中 fixture .4x）；extraEnv 為額外環境變數；
// stdin 為餵入的標準輸入。
func run4xIO(t *testing.T, dir string, extraEnv []string, stdin string, args ...string) (string, string, int) {
	t.Helper()
	cmd := exec.Command(binaryPath, args...)
	cmd.Dir = dir
	// 剝除所有 FOURX_ 前綴的繼承環境變數，讓 spawn 出的 4x 只依 cmd.Dir 的 fixture
	// workspace 解析 feature/state；否則在 4x runner 子程序內跑測試時，process 帶有的
	// FOURX_FEATURE_ID / FOURX_BIN 會讓子程序誤讀現行 dogfooding feature 的 state.json，
	// 使 scope-deny 斷言假性 fail-open。extraEnv 在剝除後再 append，允許測試顯式覆寫。
	cmd.Env = append(stripFourxEnv(os.Environ()), extraEnv...)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	exitCode := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			exitCode = ee.ExitCode()
		} else {
			t.Fatalf("run 4x %v: %v", args, err)
		}
	}
	return stdout.String(), stderr.String(), exitCode
}

// stripFourxEnv 從 env（KEY=VALUE 列表）中濾除所有 FOURX_ 前綴的變數。
func stripFourxEnv(env []string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		if strings.HasPrefix(kv, "FOURX_") {
			continue
		}
		out = append(out, kv)
	}
	return out
}

// pathFixture 建立含 .4x/features/{id}.yaml + .4x/run/{id}/state.json 的真實 workspace，回傳 root。
func pathFixture(t *testing.T, id, featureYAML, stateJSON string) string {
	t.Helper()
	root := t.TempDir()
	featuresDir := filepath.Join(root, ".4x", "features")
	runDir := filepath.Join(root, ".4x", "run", id)
	if err := os.MkdirAll(featuresDir, 0o755); err != nil {
		t.Fatalf("mkdir features: %v", err)
	}
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		t.Fatalf("mkdir run: %v", err)
	}
	// 最小合法 settings.json：EvaluateWritePathForRun 的 ReadConfig 需成功才能走到判定；
	// config error 一律 fail-open，缺此檔會讓 deny 案例被誤放行。
	cfg := `{"project":{"name":"t"},"isolation":"worktree","commit":"per-round"}`
	if err := os.WriteFile(filepath.Join(root, ".4x", "settings.json"), []byte(cfg), 0o644); err != nil {
		t.Fatalf("write settings: %v", err)
	}
	if err := os.WriteFile(filepath.Join(featuresDir, id+".yaml"), []byte(featureYAML), 0o644); err != nil {
		t.Fatalf("write feature: %v", err)
	}
	if stateJSON != "" {
		if err := os.WriteFile(filepath.Join(runDir, "state.json"), []byte(stateJSON), 0o644); err != nil {
			t.Fatalf("write state: %v", err)
		}
	}
	return root
}

// AC-3：role=reviewer 寫 source → exit 1 + stderr 含 role deny reason；寫 .4x artifact → exit 0。
func TestCheckPath_RoleScopeDeny(t *testing.T) {
	id := "F900-role"
	root := pathFixture(t, id,
		"id: "+id+"\nname: t\n",
		`{"featureId":"`+id+`","phase":"reviewing","role":"reviewer","active":true}`)

	t.Run("reviewer writing source denies", func(t *testing.T) {
		_, stderr, code := run4xIO(t, root, nil, "", "check", "--path", "cmd/4x/foo.go")
		if code != 1 {
			t.Fatalf("want exit 1, got %d (stderr=%s)", code, stderr)
		}
		if !strings.Contains(stderr, "reviewer") || !strings.Contains(stderr, "cannot modify source") {
			t.Errorf("stderr should contain role deny reason, got: %s", stderr)
		}
	})

	t.Run("reviewer writing artifact allows", func(t *testing.T) {
		_, _, code := run4xIO(t, root, nil, "", "check", "--path", ".4x/run/"+id+"/review-report.md")
		if code != 0 {
			t.Errorf("writing artifact should exit 0, got %d", code)
		}
	})
}

// AC-3：run4xIO 剝除繼承環境的 FOURX_*——即使父 process 顯式帶 FOURX_FEATURE_ID=F999-bogus，
// reviewer-writes-source 情境仍應命中 fixture 的 reviewing/reviewer 狀態並 exit 1 deny。
// 若未剝除，子程序會誤讀父環境指定的（不存在的）feature 而 fail-open 放行 → exit 0。
func TestRun4xIO_StripsFourxEnv(t *testing.T) {
	// 父 process 顯式污染：模擬在 4x runner 子程序內執行測試的實況。
	t.Setenv("FOURX_FEATURE_ID", "F999-bogus")
	t.Setenv("FOURX_BIN", "/nonexistent/4x")

	id := "F900-strip"
	root := pathFixture(t, id,
		"id: "+id+"\nname: t\n",
		`{"featureId":"`+id+`","phase":"reviewing","role":"reviewer","active":true}`)

	_, stderr, code := run4xIO(t, root, nil, "", "check", "--path", "cmd/4x/foo.go")
	if code != 1 {
		t.Fatalf("want exit 1 deny (FOURX_* must be stripped), got %d (stderr=%s)", code, stderr)
	}
	if !strings.Contains(stderr, "reviewer") || !strings.Contains(stderr, "cannot modify source") {
		t.Errorf("stderr should contain role deny reason, got: %s", stderr)
	}
}

// AC-4：repo 越界攔截——multi-repo feature，role=coder，寫 repo 外 → exit 1（scope violation）；repo 內 → exit 0。
func TestCheckPath_RepoScope(t *testing.T) {
	id := "F900-scope"
	root := pathFixture(t, id,
		"id: "+id+"\nname: t\nrepos:\n  - a\n",
		`{"featureId":"`+id+`","phase":"coding","role":"coder","active":true}`)

	t.Run("out-of-scope repo denies", func(t *testing.T) {
		_, stderr, code := run4xIO(t, root, nil, "", "check", "--path", "b/x.go")
		if code != 1 {
			t.Fatalf("want exit 1, got %d", code)
		}
		if !strings.Contains(stderr, "scope violation") {
			t.Errorf("stderr should contain 'scope violation', got: %s", stderr)
		}
	})

	t.Run("in-scope repo allows", func(t *testing.T) {
		_, _, code := run4xIO(t, root, nil, "", "check", "--path", "a/x.go")
		if code != 0 {
			t.Errorf("in-scope repo should exit 0, got %d", code)
		}
	})
}

// AC-5：非 4x run 情境放行——(a) 無 .4x、(b) 有 .4x 但目標 feature 無 state.json、(c) 無唯一 active feature。
func TestCheckPath_NonRunAllows(t *testing.T) {
	t.Run("no .4x dir allows", func(t *testing.T) {
		bare := t.TempDir()
		_, _, code := run4xIO(t, bare, nil, "", "check", "--path", "some/file.go")
		if code != 0 {
			t.Errorf("no .4x should exit 0, got %d", code)
		}
	})

	t.Run("feature without state.json allows", func(t *testing.T) {
		id := "F900-nostate"
		// stateJSON 空 → 不寫 state.json，但仍建立 run 目錄與 feature YAML。
		root := pathFixture(t, id, "id: "+id+"\nname: t\n", "")
		_, _, code := run4xIO(t, root, nil, "", "check", "--path", "some/file.go")
		if code != 0 {
			t.Errorf("missing state.json should exit 0, got %d", code)
		}
	})

	t.Run("no unique active feature allows", func(t *testing.T) {
		id := "F900-inactive"
		root := pathFixture(t, id,
			"id: "+id+"\nname: t\n",
			`{"featureId":"`+id+`","phase":"coding","role":"coder","active":false}`)
		// 未給位置參數 / FOURX_FEATURE_ID，且無 active feature → 放行。
		_, _, code := run4xIO(t, root, nil, "", "check", "--path", "some/file.go")
		if code != 0 {
			t.Errorf("no active feature should exit 0, got %d", code)
		}
	})
}

// AC-6：--path 模式唯讀——執行前後 state.json 位元組不變，且不產生 events.jsonl。
func TestCheckPath_ReadOnly(t *testing.T) {
	id := "F900-ro"
	stateContent := `{"featureId":"` + id + `","phase":"reviewing","role":"reviewer","active":true}`
	root := pathFixture(t, id, "id: "+id+"\nname: t\n", stateContent)
	statePath := filepath.Join(root, ".4x", "run", id, "state.json")
	runDir := filepath.Join(root, ".4x", "run", id)

	before, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("read state before: %v", err)
	}
	beforeList := listDir(t, runDir)

	// 一次 deny（reviewer 寫 source）與一次 allow（寫 artifact）。
	run4xIO(t, root, nil, "", "check", "--path", "cmd/4x/foo.go")
	run4xIO(t, root, nil, "", "check", "--path", ".4x/run/"+id+"/review-report.md")

	after, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("read state after: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Errorf("state.json changed by --path mode:\nbefore=%s\nafter=%s", before, after)
	}
	afterList := listDir(t, runDir)
	if strings.Join(beforeList, ",") != strings.Join(afterList, ",") {
		t.Errorf("run dir contents changed: before=%v after=%v", beforeList, afterList)
	}
	if _, err := os.Stat(filepath.Join(runDir, "events.jsonl")); err == nil {
		t.Errorf("--path mode should not create events.jsonl")
	}
}

// AC-13：--path 是快速單檔模式，繞過 guard.Check 的 required-files gate。
// 同一個「完整 4x check 會因缺 task-brief/acceptance-criteria 而失敗」的 run 目錄中，
// coder 寫 source 的 --path 檢查 → exit 0；作為對照，完整 4x check {id} → exit 非 0 且含 required file missing。
func TestCheckPath_BypassesFullCheck(t *testing.T) {
	id := "F900-bypass"
	// coding phase + role=coder + active，刻意不建立 task-brief.md / acceptance-criteria.md。
	root := pathFixture(t, id,
		"id: "+id+"\nname: t\n",
		`{"featureId":"`+id+`","phase":"coding","role":"coder","active":true,"round":1}`)

	// 對照：完整 check 應 fail 且含 required file missing。
	stdout, stderr, code := run4xIO(t, root, nil, "", "check", id)
	if code == 0 {
		t.Fatalf("full check should fail (missing required files), got exit 0")
	}
	if !strings.Contains(stdout+stderr, "required file missing") {
		t.Errorf("full check output should contain 'required file missing', got stdout=%s stderr=%s", stdout, stderr)
	}

	// --path 快速模式：coder 寫 source（合法）→ exit 0，不受 required-files gate 影響。
	_, _, pcode := run4xIO(t, root, nil, "", "check", "--path", "cmd/4x/foo.go")
	if pcode != 0 {
		t.Errorf("--path fast mode should exit 0 (bypass required-files), got %d", pcode)
	}
}

func listDir(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	return names
}
