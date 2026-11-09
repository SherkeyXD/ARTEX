//go:build windows

package tool

import (
	"context"
	"fmt"
	"io"
	"os/exec"
)

func startShell(context.Context, string, string, []string, int, int) (io.ReadWriteCloser, *exec.Cmd, error) {
	return nil, nil, fmt.Errorf("PTY shells require Linux or macOS")
}
func killShell(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}
