//go:build windows

package runner

import "os/exec"

// setupProcGroup 在 Windows 上為 no-op；exec.CommandContext 的預設行為已足夠。
func setupProcGroup(cmd *exec.Cmd) {}
