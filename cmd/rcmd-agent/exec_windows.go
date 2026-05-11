//go:build windows

package main

import (
	"context"
	"os/exec"
	"syscall"
	"time"

	"github.com/obay/rcmd/internal/api"
)

func execShell(parent context.Context, shell, cmdline, cwd string, timeoutSecs int) api.Result {
	if timeoutSecs <= 0 {
		timeoutSecs = 60
	}
	ctx, cancel := context.WithTimeout(parent, time.Duration(timeoutSecs)*time.Second)
	defer cancel()

	var cmd *exec.Cmd
	switch shell {
	case api.ShellPowerShell:
		cmd = exec.CommandContext(ctx, "powershell.exe",
			"-NoLogo", "-NonInteractive", "-NoProfile",
			"-ExecutionPolicy", "Bypass",
			"-Command", cmdline)
	default: // ShellCmd
		cmd = exec.CommandContext(ctx, "cmd.exe", "/C", cmdline)
	}
	cmd.Dir = cwd
	// HideWindow on Windows so 'powershell' doesn't briefly flash a console
	// when the agent runs as a service.
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return runCapture(ctx, cmd, timeoutSecs)
}
