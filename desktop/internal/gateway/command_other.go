//go:build !windows

package gateway

import (
	"context"
	"os/exec"
)

func gatewayCommand(ctx context.Context, name string, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, name, args...)
}
