//go:build windows

package runner

import (
	"context"
	"fmt"
	"os"
	"os/exec"
)

func startPty(ctx context.Context, cmd *exec.Cmd) (*os.File, error) {
	return nil, fmt.Errorf("pty not supported on windows")
}
