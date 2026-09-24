//go:build !unix

package mermaid

import (
	"context"
	"os/exec"
	"time"
)

func command(ctx context.Context, path string, args ...string) *exec.Cmd {
	c := exec.CommandContext(ctx, path, args...)
	c.WaitDelay = time.Second
	return c
}
