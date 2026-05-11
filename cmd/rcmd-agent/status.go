package main

import (
	"fmt"

	"github.com/obay/rcmd/internal/state"
	"github.com/spf13/cobra"
)

func newStatusCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:          "status",
		Short:        "Show agent state",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := state.LoadAgent(statePath)
			if err != nil {
				if asJSON {
					return emitJSON(map[string]any{
						"kind":         "status",
						"state_path":   statePath,
						"state_loaded": false,
						"error":        err.Error(),
					})
				}
				fmt.Printf("state         %s  FAIL: %v\n", statePath, err)
				return err
			}
			if asJSON {
				return emitJSON(map[string]any{
					"kind":          "status",
					"state_path":    statePath,
					"state_loaded":  true,
					"relay_url":     s.RelayURL,
					"agent_id":      s.AgentID,
					"default_shell": s.DefaultShell,
					"log_file":      s.LogFile,
				})
			}
			fmt.Printf("state         %s  OK\n", statePath)
			fmt.Printf("relay_url     %s\n", s.RelayURL)
			fmt.Printf("agent_id      %s\n", s.AgentID)
			fmt.Printf("default_shell %s\n", s.DefaultShell)
			fmt.Printf("log_file      %s\n", s.LogFile)
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit a single JSON object")
	return cmd
}
