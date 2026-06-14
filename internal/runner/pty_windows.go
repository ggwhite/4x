//go:build windows

package runner

import (
	"context"
	"fmt"
	"os"
	"os/exec"
)

func startPty(ctx context.Context, cmd *exec.Cmd) (*os.File, func(), error) {
	return nil, nil, fmt.Errorf("pty not supported on windows")
}
