package main

import (
	"fmt"

	"github.com/obay/rcmd/internal/state"
	"github.com/obay/rcmd/internal/token"
	"github.com/spf13/cobra"
)

func newTokenCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "token",
		Short:        "Print the current join token (re-prints; does not rotate)",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := state.LoadRelay(statePath)
			if err != nil {
				return err
			}
			tok, err := token.Mint(token.Token{RelayURL: relayURL(s), MasterSecret: s.MasterSecret})
			if err != nil {
				return fmt.Errorf("mint token: %w", err)
			}
			fmt.Println(tok)
			return nil
		},
	}
}
