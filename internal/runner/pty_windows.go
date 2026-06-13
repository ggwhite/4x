//go:build windows

package runner

import (
	"fmt"
	"os"
	"os/exec"
)

func startPty(cmd *exec.Cmd) (*os.File, error) {
	return nil, fmt.Errorf("pty not supported on windows")
}
