package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"

	"github.com/obay/rcmd/internal/state"
	"github.com/spf13/cobra"
)

// ServiceName is the SCM service name on Windows.
const ServiceName = "rcmd-agent"

var statePath string

func defaultStatePath() string {
	p, err := state.DefaultAgentPath()
	if err != nil {
		// Last-resort fallback. Only hits if HOME is unset on a non-Windows
		// host, which is unusual; surface a sensible default rather than
		// erroring at package-init time.
		return filepath.Join(os.TempDir(), "rcmd-agent.json")
	}
	return p
}

func defaultLogPath() string {
	switch runtime.GOOS {
	case "windows":
		pd := os.Getenv("PROGRAMDATA")
		if pd == "" {
			pd = `C:\ProgramData`
		}
		return filepath.Join(pd, "rcmd", "agent.log")
	case "darwin":
		return "/usr/local/var/log/rcmd-agent.log"
	default:
		return "/var/log/rcmd-agent.log"
	}
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:          "rcmd-agent",
		Short:        "rcmd remote-execution agent (Windows)",
		SilenceUsage: true,
		Long: strings.TrimSpace(`
rcmd-agent runs on a Windows host and polls the rcmd relay over HTTPS
for encrypted commands. Outbound connections only — no inbound.

First-time setup, in an elevated PowerShell on the agent host:

  rcmd-agent join <token>

The token comes from 'sudo rcmdd init' on the relay. join writes the
agent's state file, registers the Windows service, and starts it.

Useful commands:

  rcmd-agent join TOKEN     write state and (on Windows) install + start service
  rcmd-agent leave          stop + uninstall service and delete state
  rcmd-agent status         show state + service status
  rcmd-agent run            run agent loop in the foreground (dev/test)
  rcmd-agent start|stop     manage the Windows service
`),
	}
	root.PersistentFlags().StringVar(&statePath, "state", defaultStatePath(),
		"path to rcmd-agent.json state file")

	root.AddCommand(
		newJoinCmd(),
		newLeaveCmd(),
		newRunCmd(),
		newStatusCmd(),
		newVersionCmd(),
	)
	addServiceCommands(root)
	return root
}

func newVersionCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:          "version",
		Short:        "Print build info and exit",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			info, _ := debug.ReadBuildInfo()
			version := "(unknown)"
			gover := "(unknown)"
			if info != nil {
				version = info.Main.Version
				gover = info.GoVersion
			}
			platform := runtime.GOOS + "/" + runtime.GOARCH
			if asJSON {
				return emitJSON(map[string]any{
					"kind":     "version",
					"name":     "rcmd-agent",
					"version":  version,
					"go":       gover,
					"platform": platform,
				})
			}
			fmt.Printf("rcmd-agent %s\n", version)
			fmt.Printf("go         %s\n", gover)
			fmt.Printf("platform   %s\n", platform)
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit a single JSON object")
	return cmd
}

func emitJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
