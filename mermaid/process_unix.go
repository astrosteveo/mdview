//go:build unix

package mermaid

import (
	"context"
	"os/exec"
	"syscall"
	"time"
)

// A browser is a process tree; killing only the Node parent leaks Chromium.
func command(ctx context.Context, path string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = time.Second
	return cmd
}
