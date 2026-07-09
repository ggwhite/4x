package main

import (
	"encoding/json"
	"strings"
	"testing"

	feat "github.com/ggwhite/4x/internal/feature"
)

// AC-7：命令行為測試——透過真實二進位 + fixture 斷言 stdout/stderr/exit code（非結構欄位）。
// 統一用 run4xIO（已剝除 FOURX_*）確保在 4x session 內（父環境帶 FOURX_FEATURE_ID）也綠燈。

func TestStatusCmd(t *testing.T) {
	dir, ws := initWorkspace(t)
	if err := ws.SaveFeature(feat.Feature{ID: "F001-alpha", Name: "F001: Alpha", Status: feat.StatusInProgress}); err != nil {
		t.Fatal(err)
	}
	if err := ws.SaveFeature(feat.Feature{ID: "F002-beta", Name: "F002: Beta", Status: feat.StatusNotStarted}); err != nil {
		t.Fatal(err)
	}

	// 文字視圖：含兩個 feature id 與分類統計行
	stdout, stderr, code := run4xIO(t, dir, nil, "", "status")
	if code != 0 {
		t.Fatalf("status exit %d, stderr=%s", code, stderr)
	}
	if !strings.Contains(stdout, "F001-alpha") || !strings.Contains(stdout, "F002-beta") {
		t.Errorf("status output missing feature ids:\n%s", stdout)
	}
	if !strings.Contains(stdout, "Total:") {
		t.Errorf("status output missing Total summary line:\n%s", stdout)
	}

	// --json 視圖：可解析且 features 陣列含兩個 id
	jstdout, _, jcode := run4xIO(t, dir, nil, "", "status", "--json")
	if jcode != 0 {
		t.Fatalf("status --json exit %d", jcode)
	}
	var parsed struct {
		Features []struct {
			ID string `json:"id"`
		} `json:"features"`
	}
	if err := json.Unmarshal([]byte(jstdout), &parsed); err != nil {
		t.Fatalf("status --json not valid JSON: %v\n%s", err, jstdout)
	}
	ids := map[string]bool{}
	for _, f := range parsed.Features {
		ids[f.ID] = true
	}
	if !ids["F001-alpha"] || !ids["F002-beta"] {
		t.Errorf("status --json features missing expected ids: %v", ids)
	}
}

func TestConfigCmd(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	dir, _ := initWorkspace(t)

	// config set 寫入 user config
	if _, stderr, code := run4xIO(t, dir, nil, "", "config", "set", "locale", "zh-TW"); code != 0 {
		t.Fatalf("config set exit %d, stderr=%s", code, stderr)
	}

	// config get <key> 回傳剛設定的值
	stdout, stderr, code := run4xIO(t, dir, nil, "", "config", "get", "locale")
	if code != 0 {
		t.Fatalf("config get exit %d, stderr=%s", code, stderr)
	}
	if strings.TrimSpace(stdout) != "zh-TW" {
		t.Errorf("config get locale = %q, want zh-TW", strings.TrimSpace(stdout))
	}

	// config list 輸出含設定值
	lstdout, _, lcode := run4xIO(t, dir, nil, "", "config", "list")
	if lcode != 0 {
		t.Fatalf("config list exit %d", lcode)
	}
	if !strings.Contains(lstdout, "zh-TW") {
		t.Errorf("config list should contain set locale zh-TW:\n%s", lstdout)
	}
}

func TestDoctorCmd(t *testing.T) {
	dir, _ := initWorkspace(t)

	// 文字視圖：含 section 分隔線與 Summary 行
	stdout, stderr, code := run4xIO(t, dir, nil, "", "doctor")
	if code != 0 && code != 1 { // doctor 有 FAIL 時 exit 1，仍為合法輸出
		t.Fatalf("doctor unexpected exit %d, stderr=%s", code, stderr)
	}
	if !strings.Contains(stdout, "──") || !strings.Contains(stdout, "Summary:") {
		t.Errorf("doctor output missing section headers/summary:\n%s", stdout)
	}

	// --json 視圖：可解析為 {checks:[{section,severity,...}]}
	jstdout, _, _ := run4xIO(t, dir, nil, "", "doctor", "--json")
	var report struct {
		Checks []struct {
			Section  string `json:"section"`
			Severity string `json:"severity"`
		} `json:"checks"`
	}
	if err := json.Unmarshal([]byte(jstdout), &report); err != nil {
		t.Fatalf("doctor --json not valid JSON: %v\n%s", err, jstdout)
	}
	if len(report.Checks) == 0 {
		t.Errorf("doctor --json should report at least one check:\n%s", jstdout)
	}
}

func TestCostCmd(t *testing.T) {
	dir, ws := initWorkspace(t)
	// seed 兩輪 coder stream log（round-2 為 retry），驅動 by-role/by-round 聚合。
	writeStreamLog(t, ws, "F003-cost", "round-1-coder.stream.jsonl", resultLine(4.0))
	writeStreamLog(t, ws, "F003-cost", "round-2-coder.stream.jsonl", resultLine(1.0))

	// 預設（by-role）文字視圖
	stdout, stderr, code := run4xIO(t, dir, nil, "", "cost")
	if code != 0 {
		t.Fatalf("cost exit %d, stderr=%s", code, stderr)
	}
	if !strings.Contains(stdout, "Cost by role") {
		t.Errorf("default cost view should be by-role:\n%s", stdout)
	}

	// --by-round 文字視圖
	rstdout, _, rcode := run4xIO(t, dir, nil, "", "cost", "--by-round")
	if rcode != 0 {
		t.Fatalf("cost --by-round exit %d", rcode)
	}
	if !strings.Contains(rstdout, "Cost by round") {
		t.Errorf("--by-round view header missing:\n%s", rstdout)
	}

	// --json 視圖：view=by-role、total=5.0、calls=2
	jstdout, _, jcode := run4xIO(t, dir, nil, "", "cost", "--json")
	if jcode != 0 {
		t.Fatalf("cost --json exit %d", jcode)
	}
	var cj struct {
		View     string  `json:"view"`
		Calls    int     `json:"calls"`
		TotalUSD float64 `json:"totalUsd"`
	}
	if err := json.Unmarshal([]byte(jstdout), &cj); err != nil {
		t.Fatalf("cost --json not valid JSON: %v\n%s", err, jstdout)
	}
	if cj.View != "by-role" || cj.Calls != 2 || cj.TotalUSD != 5.0 {
		t.Errorf("cost --json = %+v, want by-role/2/5.0", cj)
	}
}

func TestCmdErrorPaths(t *testing.T) {
	// (1) 缺 workspace：非 4x 目錄執行 status → 非 0 exit + stderr 有錯誤訊息
	t.Run("missing workspace", func(t *testing.T) {
		bare := t.TempDir()
		_, stderr, code := run4xIO(t, bare, nil, "", "status")
		if code == 0 {
			t.Fatal("status outside a workspace should exit non-zero")
		}
		if strings.TrimSpace(stderr) == "" {
			t.Error("expected an error message on stderr")
		}
	})

	// (2) 缺 feature：合法 workspace 查不存在的 feature → 非 0 exit + stderr 含 id
	t.Run("missing feature", func(t *testing.T) {
		dir, _ := initWorkspace(t)
		_, stderr, code := run4xIO(t, dir, nil, "", "status", "F999-nonexistent")
		if code == 0 {
			t.Fatal("status on nonexistent feature should exit non-zero")
		}
		if !strings.Contains(stderr, "F999-nonexistent") {
			t.Errorf("stderr should reference the missing feature id, got: %s", stderr)
		}
	})

	// (3) 非法 flag：cobra 應報 unknown flag 並非 0 exit
	t.Run("invalid flag", func(t *testing.T) {
		dir, _ := initWorkspace(t)
		_, stderr, code := run4xIO(t, dir, nil, "", "status", "--bogus-flag")
		if code == 0 {
			t.Fatal("unknown flag should exit non-zero")
		}
		if !strings.Contains(stderr, "unknown flag") {
			t.Errorf("stderr should mention unknown flag, got: %s", stderr)
		}
	})
}
