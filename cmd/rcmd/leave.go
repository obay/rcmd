package main

import (
	"fmt"
	"os"

	"github.com/obay/rcmd/internal/state"
	"github.com/spf13/cobra"
)

func newLeaveCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "leave",
		Short:        "Delete the operator state file",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !state.Exists(statePath) {
				fmt.Printf("No state file at %s (already gone).\n", statePath)
				return nil
			}
			if err := os.Remove(statePath); err != nil {
				return err
			}
			fmt.Printf("Removed %s\n", statePath)
			return nil
		},
	}
}
