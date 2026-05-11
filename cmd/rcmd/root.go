package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"

	"github.com/obay/rcmd/internal/state"
	"github.com/spf13/cobra"
)

var statePath string

func defaultStatePath() string {
	p, err := state.DefaultOperatorPath()
	if err != nil {
		return filepath.Join(os.TempDir(), "rcmd.json")
	}
	return p
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:          "rcmd",
		Short:        "Operator CLI for the rcmd remote-exec relay",
		SilenceUsage: true,
		Long: strings.TrimSpace(`
rcmd is the operator CLI for the rcmd remote-exec relay.

First-time setup, on your operator machine:

  rcmd join <token>     # token comes from 'rcmdd init' on the relay

Then:

  rcmd list-agents              # see which agents the relay has seen
  rcmd set-default-agent NAME   # pin a default so --agent isn't needed
  rcmd status                   # round-trip probe
  rcmd run "hostname"           # run a command on the default agent
  rcmd push ./file C:\path
  rcmd pull C:\path ./file

Use --agent NAME on any command to target a specific agent.
`),
	}
	root.PersistentFlags().StringVar(&statePath, "state", defaultStatePath(),
		"path to rcmd.json state file")

	root.AddCommand(
		newJoinCmd(),
		newLeaveCmd(),
		newListAgentsCmd(),
		newSetDefaultAgentCmd(),
		newStatusCmd(),
		newRunCmd(),
		newPushCmd(),
		newPullCmd(),
		newVersionCmd(),
	)
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
			if asJSON {
				return emitJSON(map[string]any{
					"kind":     "version",
					"name":     "rcmd",
					"version":  version,
					"go":       gover,
					"platform": platformTag(),
				})
			}
			fmt.Printf("rcmd     %s\n", version)
			fmt.Printf("go       %s\n", gover)
			fmt.Printf("platform %s\n", platformTag())
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
