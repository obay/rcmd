package main

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/obay/rcmd/internal/api"
	"github.com/spf13/cobra"
)

func newStatusCmd() *cobra.Command {
	var (
		agentFlag string
		asJSON    bool
	)
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Probe the relay and round-trip a quick liveness check via an agent",
		Long: strings.TrimSpace(`
status performs two checks:
  1. GET /healthz on the relay (unauthenticated): confirms the
     relay process is up and its TLS / DNS is fine.
  2. Run "echo ok" through the encrypted command path on the target
     agent: confirms the agent is polling and all keys match.

If step 2 hangs, the agent is offline or the firewall is dropping its
poll requests. If step 1 fails, the relay or DNS / TLS is broken.

EXIT CODES
  0   both relay and agent OK
  1   relay or agent failed (see output for which)
`),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newClient()
			if err != nil {
				return err
			}
			agentID, err := c.resolveAgent(agentFlag)
			if err != nil {
				return err
			}

			out := map[string]any{
				"kind":      "status",
				"relay_url": c.relayURL,
				"agent_id":  agentID,
			}

			t0 := time.Now()
			resp, err := http.Get(c.relayURL + "/healthz")
			relayLatency := time.Since(t0).Milliseconds()
			relayOK := err == nil && resp != nil && resp.StatusCode == 200
			if resp != nil {
				resp.Body.Close()
			}
			out["relay_ok"] = relayOK
			out["relay_latency_ms"] = relayLatency
			if !relayOK {
				if err != nil {
					out["relay_error"] = err.Error()
				} else {
					out["relay_error"] = resp.Status
				}
			}

			if !asJSON {
				fmt.Printf("relay  %s ", c.relayURL)
				if relayOK {
					fmt.Printf("OK (%dms)\n", relayLatency)
				} else if err != nil {
					fmt.Println("UNREACHABLE:", err)
				} else {
					fmt.Println("BAD STATUS", resp.Status)
				}
			}

			if !relayOK {
				if asJSON {
					out["agent_ok"] = false
					out["agent_error"] = "skipped (relay down)"
					_ = emitJSON(out)
				}
				return fmt.Errorf("relay unreachable")
			}

			t1 := time.Now()
			res, runErr := c.Run(agentID, api.Command{
				Kind:        api.KindExec,
				Cmd:         "echo ok",
				Shell:       "cmd",
				TimeoutSecs: 10,
			})
			agentLatency := time.Since(t1).Milliseconds()
			agentOK := runErr == nil && res != nil && res.ExitCode == 0
			out["agent_ok"] = agentOK
			out["agent_latency_ms"] = agentLatency
			if !agentOK {
				if runErr != nil {
					out["agent_error"] = runErr.Error()
				} else if res != nil {
					out["agent_error"] = fmt.Sprintf("exit %d", res.ExitCode)
				}
			}

			if !asJSON {
				fmt.Printf("agent  %s ", agentID)
				if agentOK {
					fmt.Printf("OK (%dms)\n", agentLatency)
				} else if runErr != nil {
					fmt.Println("UNREACHABLE:", runErr)
				} else {
					fmt.Println("EXIT", res.ExitCode)
				}
			} else {
				_ = emitJSON(out)
			}

			if !agentOK {
				return fmt.Errorf("agent unreachable")
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&agentFlag, "agent", "", "agent ID (default: from state)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit a single JSON object")
	return cmd
}
