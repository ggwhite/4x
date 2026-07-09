package runner

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ggwhite/4x/internal/protocol"
)

// parseEnvDump 讀取子程序 `env` 輸出檔並解析成 map（以第一個 = 切 key/value）。
func parseEnvDump(t *testing.T, path string) map[string]string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read env dump %s: %v", path, err)
	}
	m := make(map[string]string)
	for _, line := range strings.Split(string(data), "\n") {
		if idx := strings.IndexByte(line, '='); idx >= 0 {
			m[line[:idx]] = line[idx+1:]
		}
	}
	return m
}

// envDumpRunner 建立一個 spawn 測試腳本、把子程序完整環境變數 dump 到 outFile 的 runner。
func envDumpRunner(t *testing.T, name string, ef EnvFilter) (*SubprocessRunner, string) {
	t.Helper()
	binDir := t.TempDir()
	outFile := filepath.Join(t.TempDir(), "env.out")
	writeScript(t, binDir, "test-runner", "#!/bin/sh\nenv > "+outFile+"\nexit 0\n")
	r := &SubprocessRunner{
		Workspace: &protocol.Workspace{Root: t.TempDir()},
		Name:      name,
		Config: protocol.RunnerConfig{
			Command: filepath.Join(binDir, "test-runner"),
			Args:    []string{"-p", "{prompt}"},
		},
		envFilter: ef,
	}
	return r, outFile
}

// TestRun_FiltersSensitiveEnv 真實子程序端到端：敏感變數被濾、per-runner 認證保留、
// essential 變數保留（AC-4）。
func TestRun_FiltersSensitiveEnv(t *testing.T) {
	t.Setenv("MY_FAKE_TOKEN", "leak")
	t.Setenv("ANTHROPIC_API_KEY", "keep")

	r, outFile := envDumpRunner(t, "claude", EnvFilter{})
	if _, err := r.Run(context.Background(), "x"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	env := parseEnvDump(t, outFile)
	if _, ok := env["MY_FAKE_TOKEN"]; ok {
		t.Errorf("MY_FAKE_TOKEN should have been filtered out of subprocess env")
	}
	if env["ANTHROPIC_API_KEY"] != "keep" {
		t.Errorf("ANTHROPIC_API_KEY = %q, want keep (per-runner allowlist)", env["ANTHROPIC_API_KEY"])
	}
	if _, ok := env["PATH"]; !ok {
		t.Errorf("PATH missing from subprocess env")
	}
	if _, ok := env["HOME"]; !ok {
		t.Errorf("HOME missing from subprocess env")
	}
}

// TestRun_SettingsDenylistAllowlist 真實子程序：envFilter（由 ResolveEnvFilter 帶入）的
// Denylist/Allowlist 被消費——CUSTOM_THING 被濾、MY_FAKE_TOKEN 被 allowlist 放行（AC-5）。
func TestRun_SettingsDenylistAllowlist(t *testing.T) {
	t.Setenv("CUSTOM_THING", "drop")
	t.Setenv("MY_FAKE_TOKEN", "keep")

	cfg := protocol.Config{
		RunnerEnv: protocol.RunnerEnvConfig{
			Denylist:  []string{"CUSTOM_*"},
			Allowlist: []string{"MY_FAKE_TOKEN"},
		},
	}
	r, outFile := envDumpRunner(t, "someunknown", ResolveEnvFilter(cfg))
	if _, err := r.Run(context.Background(), "x"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	env := parseEnvDump(t, outFile)
	if _, ok := env["CUSTOM_THING"]; ok {
		t.Errorf("CUSTOM_THING should be filtered by settings denylist")
	}
	if env["MY_FAKE_TOKEN"] != "keep" {
		t.Errorf("MY_FAKE_TOKEN = %q, want keep (settings allowlist overrides *_TOKEN)", env["MY_FAKE_TOKEN"])
	}
}

// TestRun_FourxBinSurvivesFilter 真實子程序：FOURX_BIN（4x 自注入）不被過濾（AC-7）。
func TestRun_FourxBinSurvivesFilter(t *testing.T) {
	r, outFile := envDumpRunner(t, "claude", EnvFilter{})
	if _, err := r.Run(context.Background(), "x"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	env := parseEnvDump(t, outFile)
	if _, ok := env["FOURX_BIN"]; !ok {
		t.Errorf("FOURX_BIN missing from subprocess env (should never be filtered)")
	}
}

// TestRun_AllowlistByNameWithAbsCommand 真實子程序：Name=claude、Command 為絕對路徑，
// 父程序 set ANTHROPIC_API_KEY，子程序仍看得到（AC-11 d）。
func TestRun_AllowlistByNameWithAbsCommand(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "keep")

	binDir := t.TempDir()
	outFile := filepath.Join(t.TempDir(), "env.out")
	absCmd := writeScript(t, binDir, "claude-wrapper", "#!/bin/sh\nenv > "+outFile+"\nexit 0\n")
	if !filepath.IsAbs(absCmd) {
		t.Fatalf("expected absolute command path, got %q", absCmd)
	}
	r := &SubprocessRunner{
		Workspace: &protocol.Workspace{Root: t.TempDir()},
		Name:      "claude",
		Config: protocol.RunnerConfig{
			Command: absCmd,
			Args:    []string{"-p", "{prompt}"},
		},
	}
	if _, err := r.Run(context.Background(), "x"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	env := parseEnvDump(t, outFile)
	if env["ANTHROPIC_API_KEY"] != "keep" {
		t.Errorf("ANTHROPIC_API_KEY = %q, want keep (name=claude allowlist with abs command)", env["ANTHROPIC_API_KEY"])
	}
}

// TestRun_LogFilePerms0600 真實 spawned runner 建立的 .log 檔權限為 0600（AC-8）。
func TestRun_LogFilePerms0600(t *testing.T) {
	binDir := t.TempDir()
	writeScript(t, binDir, "test-runner", "#!/bin/sh\necho hi\nexit 0\n")
	logPath := filepath.Join(t.TempDir(), "logs", "round-1-coder.log")
	r := &SubprocessRunner{
		Workspace: &protocol.Workspace{Root: t.TempDir()},
		Name:      "test",
		LogPath:   logPath,
		Config: protocol.RunnerConfig{
			Command: filepath.Join(binDir, "test-runner"),
			Args:    []string{"-p", "{prompt}"},
		},
	}
	if _, err := r.Run(context.Background(), "x"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	info, err := os.Stat(logPath)
	if err != nil {
		t.Fatalf("stat log: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("log perm = %o, want 0600", perm)
	}
}

// TestRun_StreamLogPerms0600 stream-json 模式下 .stream.jsonl 權限為 0600（AC-8）。
func TestRun_StreamLogPerms0600(t *testing.T) {
	binDir := t.TempDir()
	writeScript(t, binDir, "test-runner", "#!/bin/sh\necho '{\"type\":\"x\"}'\nexit 0\n")
	logPath := filepath.Join(t.TempDir(), "logs", "round-1-coder.log")
	r := &SubprocessRunner{
		Workspace: &protocol.Workspace{Root: t.TempDir()},
		Name:      "test",
		LogPath:   logPath,
		Config: protocol.RunnerConfig{
			Command:      filepath.Join(binDir, "test-runner"),
			Args:         []string{"-p", "{prompt}"},
			OutputFormat: "stream-json",
		},
	}
	if _, err := r.Run(context.Background(), "x"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	streamPath := strings.TrimSuffix(logPath, ".log") + ".stream.jsonl"
	info, err := os.Stat(streamPath)
	if err != nil {
		t.Fatalf("stat stream log: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("stream log perm = %o, want 0600", perm)
	}
}

// TestBuildArgs_PromptTempPerms0600 回歸鎖定 R1：{promptFile} 與 claude guard-settings
// temp 檔皆為 0600（AC-9）。
func TestBuildArgs_PromptTempPerms0600(t *testing.T) {
	r := &SubprocessRunner{
		Name: "claude",
		Config: protocol.RunnerConfig{
			Command: "claude",
			Args:    []string{"-p", "{promptFile}"},
		},
	}
	args, cleanup, err := r.buildArgs("hello prompt")
	if cleanup != nil {
		defer cleanup()
	}
	if err != nil {
		t.Fatalf("buildArgs: %v", err)
	}

	// prompt temp 檔為 args[1]
	promptFile := args[1]
	info, err := os.Stat(promptFile)
	if err != nil {
		t.Fatalf("stat prompt temp: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("prompt temp perm = %o, want 0600", perm)
	}

	// claude runner 注入 --settings <guard-settings temp>
	var settingsFile string
	for i, a := range args {
		if a == "--settings" && i+1 < len(args) {
			settingsFile = args[i+1]
		}
	}
	if settingsFile == "" {
		t.Fatal("expected --settings guard temp injected for claude runner")
	}
	sinfo, err := os.Stat(settingsFile)
	if err != nil {
		t.Fatalf("stat guard settings temp: %v", err)
	}
	if perm := sinfo.Mode().Perm(); perm != 0o600 {
		t.Errorf("guard settings temp perm = %o, want 0600", perm)
	}
}

// TestRun_DebugLogsFilteredKeysNotValues 驗證 slog.Debug 以 "filtered" 記錄被濾變數名稱、
// 不含其值（AC-10）。
func TestRun_DebugLogsFilteredKeysNotValues(t *testing.T) {
	t.Setenv("MY_FAKE_TOKEN", "leak")

	var buf bytes.Buffer
	orig := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	defer slog.SetDefault(orig)

	r, _ := envDumpRunner(t, "claude", EnvFilter{})
	if _, err := r.Run(context.Background(), "x"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "MY_FAKE_TOKEN") {
		t.Errorf("debug log should mention filtered key MY_FAKE_TOKEN, got:\n%s", out)
	}
	if strings.Contains(out, "leak") {
		t.Errorf("debug log must NOT contain the filtered value 'leak', got:\n%s", out)
	}
}
