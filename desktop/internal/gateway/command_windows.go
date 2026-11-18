package gateway

import (
	"context"
	"os/exec"
	"syscall"
)

func gatewayCommand(ctx context.Context, name string, args ...string) *exec.Cmd {
	const createNoWindow = 0x08000000
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNoWindow}
	return cmd
}
