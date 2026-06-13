package protocol

// MergeConfig 合併 user-level 和 project-level 設定。
// project 非零值欄位覆蓋 user；project-only 欄位（Project、Isolation 等）直接使用 project 的值。
func MergeConfig(user UserConfig, project Config) Config {
	result := project

	if result.Default == "" && user.DefaultRunner != "" {
		result.Default = user.DefaultRunner
	}

	result.Runners = mergeRunners(user.Runners, project.Runners)
	result.Roles = mergeRoles(user.Roles, project.Roles)

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

	return result
}
