package main

import (
	"fmt"

	"github.com/obay/rcmd/internal/state"
	"github.com/spf13/cobra"
)

func newSetDefaultAgentCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "set-default-agent NAME",
		Short:        "Pin a default agent so commands don't need --agent",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := state.LoadOperator(statePath)
			if err != nil {
				return err
			}
			s.DefaultAgent = args[0]
			if err := state.SaveOperator(statePath, s); err != nil {
				return err
			}
			fmt.Printf("default_agent = %s\n", s.DefaultAgent)
			return nil
		},
	}
}
