//go:build !windows

package tool

import (
	"context"
	"io"
	"os"
	"os/exec"
	"syscall"

	"github.com/creack/pty"
)

func startShell(ctx context.Context, command, dir string, env []string, rows, cols int) (io.ReadWriteCloser, *exec.Cmd, error) {
	cmd := exec.CommandContext(ctx, "bash", "-c", command)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	file, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: uint16(rows), Cols: uint16(cols)})
	return file, cmd, err
}

func killShell(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
