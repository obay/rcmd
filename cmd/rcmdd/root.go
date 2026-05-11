package main

import (
	"encoding/json"
	"fmt"
	"os"
	"runtime/debug"
	"strings"

	"github.com/obay/rcmd/internal/state"
	"github.com/spf13/cobra"
)

var statePath string

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "rcmdd",
		Short: "rcmd relay server",
		Long: strings.TrimSpace(`
rcmdd is the rcmd relay server. It runs on a Linux host and brokers
encrypted commands between the rcmd operator CLI and the rcmd-agent
(Windows).

First-time setup on the relay host (after installing the .deb / .rpm):

  sudo rcmdd init --domain relay.example.com
  sudo systemctl enable --now rcmdd

Day-2 ops:

  rcmdd token       # print the current join token
  rcmdd list        # show seen operators + agents
  rcmdd rekey       # rotate the master secret (invalidates everyone)
  rcmdd forget X    # remove a name from the seen list
  rcmdd status      # health check

The 'serve' subcommand is invoked by systemd / brew services; it is
not meant to be run by humans.
`),
		SilenceUsage: true,
	}
	root.PersistentFlags().StringVar(&statePath, "state", state.DefaultRelayPath(),
		"path to rcmdd.json state file")

	root.AddCommand(
		newInitCmd(),
		newServeCmd(),
		newTokenCmd(),
		newRekeyCmd(),
		newListCmd(),
		newForgetCmd(),
		newStatusCmd(),
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
					"kind":    "version",
					"name":    "rcmdd",
					"version": version,
					"go":      gover,
				})
			}
			fmt.Printf("rcmdd %s\n", version)
			fmt.Printf("go     %s\n", gover)
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
