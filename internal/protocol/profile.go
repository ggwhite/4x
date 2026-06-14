package protocol

import (
	"fmt"
	"os"
)

// DefaultProfiles 回傳內建三組 pipeline profile，作為 auto-select 與 fallback 用。
// full 跑完整 6 role；normal 省略 designer 與 deep-reviewer；quick 只跑 coder 與 reviewer。
func DefaultProfiles() map[string]ProfileConfig {
	return map[string]ProfileConfig{
		"full":   {Roles: []string{"designer", "coder", "reviewer", "tester", "deep-reviewer", "acceptor"}},
		"normal": {Roles: []string{"coder", "reviewer", "tester", "acceptor"}},
		"quick":  {Roles: []string{"coder", "reviewer"}},
	}
}

// ResolveProfile 決定本次 run 使用的 profile 名稱與設定。
//
// 解析優先序：
//   - override 非空（--profile）：先查 cfg.Profiles，再退 DefaultProfiles；都找不到回 error。
//   - override 為空且 cfg.Profiles 為空：回 full（向後相容，priority 不參與）。
//   - override 為空且 cfg.Profiles 非空：依 feature.Priority auto-select
//     （nil/0/1→full、2→normal、>=3→quick），選到的名稱優先取 cfg.Profiles，
//     缺則退 DefaultProfiles，再缺則退 full 並印 warning。
//
// 解析出的 profile 若 Roles 不含 "coder" 視為設定錯誤，回 error（coder 為唯一必要 role）。
func ResolveProfile(cfg Config, feature Feature, override string) (string, ProfileConfig, error) {
	defaults := DefaultProfiles()

	lookup := func(name string) (ProfileConfig, bool) {
		if pc, ok := cfg.Profiles[name]; ok {
			return pc, true
		}
		if pc, ok := defaults[name]; ok {
			return pc, true
		}
		return ProfileConfig{}, false
	}

	var name string
	var pc ProfileConfig

	switch {
	case override != "":
		got, ok := lookup(override)
		if !ok {
			return "", ProfileConfig{}, fmt.Errorf("unknown profile %q", override)
		}
		name, pc = override, got

	case len(cfg.Profiles) == 0:
		name, pc = "full", defaults["full"]

	default:
		name = autoSelectProfile(feature.Priority)
		got, ok := lookup(name)
		if !ok {
			fmt.Fprintf(os.Stderr, "warning: profile %q not found, falling back to full\n", name)
			name, pc = "full", defaults["full"]
		} else {
			pc = got
		}
	}

	if !pc.EnablesRole(RoleCoder) {
		return "", ProfileConfig{}, fmt.Errorf("profile %q is missing required role %q", name, RoleCoder)
	}

	return name, pc, nil
}

// autoSelectProfile 依 feature priority 對應到內建 profile 名稱。
// priority 越大代表越不重要，pipeline 越精簡。
func autoSelectProfile(priority *int) string {
	if priority == nil {
		return "full"
	}
	switch p := *priority; {
	case p <= 1:
		return "full"
	case p == 2:
		return "normal"
	default:
		return "quick"
	}
}

// EnablesRole 判斷某 role 是否在 profile 的 Roles 列表中。
func (pc ProfileConfig) EnablesRole(role Role) bool {
	for _, r := range pc.Roles {
		if Role(r) == role {
			return true
		}
	}
	return false
}

// ResolveProfileModel 在 ResolveModel 外加上 profile 的 coder_model 覆蓋。
// 當 role 為 coder 且 pc.CoderModel 非空時，以 pc.CoderModel 當 tier 解析；
// 其餘 role 直接呼叫 ResolveModel。
func ResolveProfileModel(cfg Config, runnerName string, role Role, pc ProfileConfig) (string, error) {
	if role == RoleCoder && pc.CoderModel != "" {
		override := cfg
		roles := make(map[string]RoleConfig, len(cfg.Roles)+1)
		for k, v := range cfg.Roles {
			roles[k] = v
		}
		coderCfg := roles[string(RoleCoder)]
		coderCfg.Model = pc.CoderModel
		roles[string(RoleCoder)] = coderCfg
		override.Roles = roles
		return ResolveModel(override, runnerName, role)
	}
	return ResolveModel(cfg, runnerName, role)
}
