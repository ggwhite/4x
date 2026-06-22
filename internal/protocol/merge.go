package protocol

import "github.com/ggwhite/4x/internal/feature"

// MergeHooks 合併全域和 feature 的 hooks，feature 同名 key 整組替換全域。
// 兩者皆為 nil 時回傳 nil；否則複製 global 所有 key，再由 featureHooks 的 key 整組覆蓋。
func MergeHooks(global, featureHooks map[string][]feature.HookEntry) map[string][]feature.HookEntry {
	if global == nil && featureHooks == nil {
		return nil
	}
	merged := make(map[string][]feature.HookEntry)
	for k, v := range global {
		merged[k] = v
	}
	for k, v := range featureHooks {
		merged[k] = v
	}
	return merged
}

// MergeConfig 合併 user-level 和 project-level 設定。
// project 非零值欄位覆蓋 user；project-only 欄位（Project、Isolation 等）直接使用 project 的值。
func MergeConfig(user UserConfig, project Config) Config {
	result := project

	if result.Default == "" && user.DefaultRunner != "" {
		result.Default = user.DefaultRunner
	}

	result.Runners = mergeRunners(user.Runners, project.Runners)
	result.Roles = mergeRoles(user.Roles, project.Roles)

	// Notifications：project 非 nil 已在 result 沿用；project 未設定時退回 user 設定。
	if result.Notifications == nil && user.Notifications != nil {
		result.Notifications = user.Notifications
	}

	// SelfMod：project 非 nil 已在 result 沿用；project 未設定時退回 user 設定（與 Notifications 同模式）。
	if result.SelfMod == nil && user.SelfMod != nil {
		result.SelfMod = user.SelfMod
	}

	return result
}

func mergeRunners(user, project map[string]RunnerConfig) map[string]RunnerConfig {
	merged := make(map[string]RunnerConfig)

	for name, rc := range user {
		merged[name] = rc
	}

	for name, prc := range project {
		if urc, ok := merged[name]; ok {
			merged[name] = mergeRunner(urc, prc)
		} else {
			merged[name] = prc
		}
	}

	return merged
}

func mergeRunner(user, project RunnerConfig) RunnerConfig {
	result := user

	if project.Command != "" {
		result.Command = project.Command
	}
	if project.Args != nil {
		result.Args = project.Args
	}
	if project.Model != "" {
		result.Model = project.Model
	}
	if project.OutputFormat != "" {
		result.OutputFormat = project.OutputFormat
	}
	if project.Tiers != nil {
		if result.Tiers == nil {
			result.Tiers = make(map[string]string)
		}
		for k, v := range project.Tiers {
			result.Tiers[k] = v
		}
	}
	if project.Stdin != nil {
		result.Stdin = project.Stdin
	}
	if project.Tty != nil {
		result.Tty = project.Tty
	}
	if project.Quiet != nil {
		result.Quiet = project.Quiet
	}

	return result
}

func mergeRoles(user, project map[string]RoleConfig) map[string]RoleConfig {
	merged := make(map[string]RoleConfig)

	for name, rc := range user {
		merged[name] = rc
	}

	for name, prc := range project {
		if urc, ok := merged[name]; ok {
			merged[name] = mergeRole(urc, prc)
		} else {
			merged[name] = prc
		}
	}

	return merged
}

func mergeRole(user, project RoleConfig) RoleConfig {
	result := user

	if project.Model != "" {
		result.Model = project.Model
	}
	if project.DeepModel != "" {
		result.DeepModel = project.DeepModel
	}
	if project.ScreenshotDir != "" {
		result.ScreenshotDir = project.ScreenshotDir
	}
	if project.Instructions != nil {
		result.Instructions = project.Instructions
	}
	if project.Includes != nil {
		result.Includes = project.Includes
	}
	if project.MaxFixRounds > 0 {
		result.MaxFixRounds = project.MaxFixRounds
	}
	if project.ParallelReviewers > 0 {
		result.ParallelReviewers = project.ParallelReviewers
	}
	if project.AnglesPerReviewer > 0 {
		result.AnglesPerReviewer = project.AnglesPerReviewer
	}

	return result
}
