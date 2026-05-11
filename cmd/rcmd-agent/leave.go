package main

import (
	"fmt"
	"os"
	"runtime"

	"github.com/obay/rcmd/internal/state"
	"github.com/spf13/cobra"
)

func newLeaveCmd() *cobra.Command {
	var keepState bool
	cmd := &cobra.Command{
		Use:          "leave",
		Short:        "Stop + uninstall the agent service and delete state",
		SilenceUsage: true,
		Long: `leave is the inverse of join. On Windows it stops and uninstalls
the SCM service. It then deletes the state file. Pass --keep-state to
preserve the JSON file (useful if you only want to remove the service).`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if runtime.GOOS == "windows" {
				if err := uninstallService(); err != nil {
					// don't fail hard — maybe the service was already gone
					fmt.Fprintf(os.Stderr, "warn: %v\n", err)
				}
			}
			if !keepState {
				if !state.Exists(statePath) {
					fmt.Printf("No state file at %s (already gone).\n", statePath)
					return nil
				}
				if err := os.Remove(statePath); err != nil {
					return fmt.Errorf("remove state: %w", err)
				}
				fmt.Printf("Removed %s\n", statePath)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&keepState, "keep-state", false, "do not delete the state file")
	return cmd
}
