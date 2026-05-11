//go:build !windows

package main

import (
	"context"
	"os/exec"
	"time"

	"github.com/obay/obcmd/internal/api"
)

// execShell on non-Windows platforms. Used for local development and
// for installing the agent on a Linux/macOS box as well. "cmd" maps to
// sh, "powershell" maps to pwsh (Windows PowerShell isn't a thing here).
func execShell(parent context.Context, shell, cmdline, cwd string, timeoutSecs int) api.Result {
	if timeoutSecs <= 0 {
		timeoutSecs = 60
	}
	ctx, cancel := context.WithTimeout(parent, time.Duration(timeoutSecs)*time.Second)
	defer cancel()

	var cmd *exec.Cmd
	switch shell {
	case api.ShellPowerShell:
		cmd = exec.CommandContext(ctx, "pwsh",
			"-NoLogo", "-NonInteractive", "-NoProfile",
			"-Command", cmdline)
	default: // ShellCmd or "sh"
		cmd = exec.CommandContext(ctx, "sh", "-c", cmdline)
	}
	cmd.Dir = cwd
	return runCapture(ctx, cmd, timeoutSecs)
}
