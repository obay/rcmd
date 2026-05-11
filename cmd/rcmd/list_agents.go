package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newListAgentsCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:          "list-agents",
		Short:        "Query the relay for known agents",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newClient()
			if err != nil {
				return err
			}
			agents, err := c.ListAgents()
			if err != nil {
				return err
			}
			if asJSON {
				return emitJSON(map[string]any{
					"kind":          "agents",
					"agents":        agents,
					"default_agent": c.state.DefaultAgent,
				})
			}
			if len(agents) == 0 {
				fmt.Println("(no agents have shown up yet)")
				return nil
			}
			for _, a := range agents {
				if a == c.state.DefaultAgent {
					fmt.Printf("%s  (default)\n", a)
				} else {
					fmt.Println(a)
				}
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit a single JSON object")
	return cmd
}
