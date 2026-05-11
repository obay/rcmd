//go:build !windows

package main

import (
	"errors"
	"runtime"

	"github.com/spf13/cobra"
)

// addServiceCommands on non-Windows platforms registers stub
// subcommands so 'rcmd-agent --help' shows them and a user gets
// a clear error if they try to install on the wrong OS.
//
// On macOS/Linux just run 'rcmd-agent run' under systemd / launchd.
func addServiceCommands(root *cobra.Command) {
	root.AddCommand(
		stub("install", "Register the Windows service (Windows only)"),
		stub("uninstall", "Remove the Windows service (Windows only)"),
		stub("service", "Run as a Windows service (Windows only)"),
		stub("start", "Start the Windows service (Windows only)"),
		stub("stop", "Stop the Windows service (Windows only)"),
	)
}

func stub(name, short string) *cobra.Command {
	return &cobra.Command{
		Use:   name,
		Short: short,
		RunE: func(cmd *cobra.Command, args []string) error {
			return errors.New(cmd.Use + " is only available on Windows (current: " + runtime.GOOS + "). Use 'rcmd-agent run' on this platform.")
		},
	}
}

// installService / uninstallService stubs for non-Windows builds so
// join.go and leave.go can call them unconditionally.
func installService(binPath string) error {
	return errors.New("service install is Windows-only; on this platform use 'rcmd-agent run' for foreground operation")
}

func uninstallService() error {
	return errors.New("service uninstall is Windows-only")
}
