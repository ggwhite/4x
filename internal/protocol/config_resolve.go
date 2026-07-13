package protocol

import (
	"path/filepath"

	"github.com/ggwhite/4x/internal/feature"
)

// NotificationsEnabled 回報合併後設定是否啟用 OS 通知。
// Notifications 為 nil（使用者未設定）時預設啟用，回傳 true。
func NotificationsEnabled(cfg Config) bool {
	if cfg.Notifications == nil {
		return true
	}
	return *cfg.Notifications
}

// BoolPtr 建立 *bool 指標，用於 RunnerConfig 布林欄位的初始化
func BoolPtr(b bool) *bool {
	return &b
}

// BoolVal 安全取 *bool 的值，nil 視為 false
func BoolVal(p *bool) bool {
	if p == nil {
		return false
	}
	return *p
}

// ResolveRepoPaths 從 workspace config 解析 repo name → absolute path。
// monorepo 模式回傳 {"." : root}。
func ResolveRepoPaths(cfg Config, root string) map[string]string {
	if len(cfg.Workspace.Repos) == 0 {
		return map[string]string{".": root}
	}
	paths := make(map[string]string, len(cfg.Workspace.Repos))
	for name, rc := range cfg.Workspace.Repos {
		paths[name] = filepath.Join(root, rc.Path)
	}
	return paths
}

// ResolveFeatureRepoPaths 解析 feature 涉及的 repo name → absolute path。
// feature.Repos 為空時：multi-repo 回傳所有 workspace repos，monorepo 回傳 {".": root}。
func ResolveFeatureRepoPaths(f feature.Feature, cfg Config, root string) map[string]string {
	all := ResolveRepoPaths(cfg, root)
	if len(f.Repos) == 0 {
		return all
	}
	result := make(map[string]string, len(f.Repos))
	for _, name := range f.Repos {
		if p, ok := all[name]; ok {
			result[name] = p
		}
	}
	return result
}

// SupportedRunners 回傳所有受支援 runner 的預設設定，作為 init 和 dashboard 的單一真相源
func SupportedRunners() []RunnerPreset {
	return []RunnerPreset{
		{Name: "claude", Config: RunnerConfig{
			Command:      "claude",
			Args:         []string{"--dangerously-skip-permissions", "-p", "{prompt}", "--output-format", "stream-json", "--verbose"},
			Model:        "opus",
			OutputFormat: "stream-json",
			Tiers:        map[string]string{TierStrong: "opus", TierFast: "sonnet"},
		}},
		{Name: "codex", Config: RunnerConfig{
			Command: "codex",
			Args:    []string{"exec", "--json"},
			Stdin:   BoolPtr(true),
			Quiet:   BoolPtr(true),
			Tiers:   map[string]string{TierStrong: "gpt-5.5", TierFast: "gpt-5.5"},
		}},
		{Name: "gemini", Config: RunnerConfig{
			Command: "gemini",
			Args:    []string{"-y", "-p", "{prompt}"},
			Tiers:   map[string]string{TierStrong: "gemini-2.5-flash", TierFast: "gemini-2.5-flash"},
		}},
		{Name: "agy", Config: RunnerConfig{
			Command: "agy",
			Args:    []string{"--dangerously-skip-permissions", "-p", "{prompt}"},
			Tiers:   map[string]string{TierStrong: "claude-opus-4-6-thinking", TierFast: "claude-sonnet-4-6-thinking"},
		}},
		{Name: "copilot", Config: RunnerConfig{
			Command: "copilot",
			Args:    []string{"--yolo", "-p", "{prompt}"},
			Tiers:   map[string]string{TierStrong: "auto", TierFast: "auto"},
		}},
		{Name: "opencode", Config: RunnerConfig{
			Command: "opencode",
			Args:    []string{"run", "--dangerously-skip-permissions", "{prompt}"},
			Tiers:   map[string]string{TierStrong: "anthropic/claude-opus-4-6", TierFast: "anthropic/claude-sonnet-4-6"},
		}},
		{Name: "cursor", Config: RunnerConfig{
			Command: "agent",
			Args:    []string{"-p", "{prompt}"},
		}},
	}
}

// SupportedRunnerMap 回傳 name → RunnerConfig 的 map
func SupportedRunnerMap() map[string]RunnerConfig {
	m := make(map[string]RunnerConfig)
	for _, p := range SupportedRunners() {
		m[p.Name] = p.Config
	}
	return m
}

// DefaultRoleModels 依 runner 產生 4x init scaffold 的 per-role model 預設，全程使用 canonical
// tier 常數（TierStrong / TierFast），不出現字面 "opus"/"sonnet"。
// 對有 tiers 的 runner（能解析 canonical tier）：designer 用 TierStrong（判斷密集），
// coder / reviewer / tester 用 TierFast（例行、量大），reviewer 另帶 TierStrong 的 deep_model。
// 對無 tiers 的 runner（如 cursor）或不在內建 registry 的 runner 名：Model / DeepModel 皆留空，
// 讓該 runner 用自身預設、不 scaffold 出無法解析的 tier；tester 一律帶 ScreenshotDir。
func DefaultRoleModels(runnerName string) map[string]RoleConfig {
	preset, ok := SupportedRunnerMap()[runnerName]
	strong, fast := "", ""
	if ok && len(preset.Tiers) > 0 {
		strong, fast = TierStrong, TierFast
	}
	return map[string]RoleConfig{
		"designer": {Model: strong},
		"coder":    {Model: fast},
		"reviewer": {Model: fast, DeepModel: strong},
		"tester":   {Model: fast, ScreenshotDir: feature.DefaultScreenshotDir},
	}
}

// EffectiveHubRepos 合併 Config.HubRepos 與 workspace config 中 Hub: true 的 repo。
func EffectiveHubRepos(cfg Config) []string {
	seen := make(map[string]bool)
	var hubs []string
	for _, h := range cfg.HubRepos {
		if !seen[h] {
			seen[h] = true
			hubs = append(hubs, h)
		}
	}
	for name, rc := range cfg.Workspace.Repos {
		if rc.Hub && !seen[name] {
			seen[name] = true
			hubs = append(hubs, name)
		}
	}
	return hubs
}
