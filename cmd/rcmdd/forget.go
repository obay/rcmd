package main

import (
	"errors"
	"fmt"

	"github.com/obay/rcmd/internal/state"
	"github.com/spf13/cobra"
)

func newForgetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "forget NAME",
		Short:        "Remove a name from the seen-agents/seen-operators list (cosmetic)",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		Long: `forget removes NAME from both the agents and operators maps in the
state file. This is cosmetic — anyone with the master secret can
re-introduce that name by simply showing up. To actually revoke
access, use 'rcmdd rekey'.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			s, err := state.LoadRelay(statePath)
			if err != nil {
				return err
			}
			removed := false
			if _, ok := s.Agents[name]; ok {
				delete(s.Agents, name)
				removed = true
			}
			if _, ok := s.Operators[name]; ok {
				delete(s.Operators, name)
				removed = true
			}
			if !removed {
				return errors.New("name not in seen list")
			}
			if err := state.SaveRelay(statePath, s); err != nil {
				return err
			}
			fmt.Printf("Removed %q from seen list.\n", name)
			return nil
		},
	}
	return cmd
}
