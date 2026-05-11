package main

import (
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/obay/obcmd/internal/crypto"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func newDoctorCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Self-diagnostic for the agent (config, keys, relay reachability)",
		Long: strings.TrimSpace(`
DESCRIPTION
  doctor verifies that the agent can:
    1. Find and load its config file.
    2. Decode agent_key and payload_key correctly.
    3. Reach the relay's /healthz endpoint over the network.
    4. (informational) Report log_file path and default_shell.

  Run this before installing as a service to catch config/network
  issues without the SCM swallowing the error.

EXAMPLES
  obcmd-agent doctor
  obcmd-agent doctor --json

EXIT CODES
  0   all checks passed
  1   any check failed (see the report for which)
`),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			report := map[string]any{
				"kind":          "doctor",
				"config_path":   effectiveConfigPath(),
				"config_loaded": false,
				"all_ok":        false,
			}
			if err := initConfig(); err != nil {
				report["error"] = err.Error()
				if !asJSON {
					fmt.Fprintln(os.Stderr, "config       FAIL:", err)
				} else {
					_ = emitJSON(report)
				}
				return err
			}
			report["config_loaded"] = true
			report["config_path"] = viper.ConfigFileUsed()

			relayURL := strings.TrimRight(viper.GetString("relay_url"), "/")
			agentID := viper.GetString("agent_id")
			ak := viper.GetString("agent_key")
			pk := viper.GetString("payload_key")

			report["relay_url"] = relayURL
			report["agent_id"] = agentID
			report["log_file"] = viper.GetString("log_file")
			report["default_shell"] = viper.GetString("default_shell")

			akOK := true
			if _, err := crypto.ParseKey(ak); err != nil {
				report["agent_key_ok"] = false
				report["agent_key_error"] = err.Error()
				akOK = false
			} else {
				report["agent_key_ok"] = true
				report["agent_key_last4"] = last4(ak)
			}
			pkOK := true
			if _, err := crypto.ParseKey(pk); err != nil {
				report["payload_key_ok"] = false
				report["payload_key_error"] = err.Error()
				pkOK = false
			} else {
				report["payload_key_ok"] = true
				report["payload_key_last4"] = last4(pk)
			}

			if !asJSON {
				fmt.Printf("config        %s  OK\n", viper.ConfigFileUsed())
				fmt.Printf("relay_url     %s\n", relayURL)
				fmt.Printf("agent_id      %s\n", agentID)
				fmt.Printf("log_file      %s\n", viper.GetString("log_file"))
				fmt.Printf("default_shell %s\n", viper.GetString("default_shell"))
				if akOK {
					fmt.Printf("agent_key     …%s  OK\n", last4(ak))
				} else {
					fmt.Printf("agent_key     FAIL: %v\n", report["agent_key_error"])
				}
				if pkOK {
					fmt.Printf("payload_key   …%s  OK\n", last4(pk))
				} else {
					fmt.Printf("payload_key   FAIL: %v\n", report["payload_key_error"])
				}
			}

			relayOK := false
			if relayURL != "" {
				t0 := time.Now()
				resp, err := http.Get(relayURL + "/healthz")
				lat := time.Since(t0).Milliseconds()
				relayOK = err == nil && resp != nil && resp.StatusCode == 200
				report["relay_ok"] = relayOK
				report["relay_latency_ms"] = lat
				if resp != nil {
					resp.Body.Close()
				}
				if !relayOK {
					if err != nil {
						report["relay_error"] = err.Error()
					} else {
						report["relay_error"] = resp.Status
					}
				}
				if !asJSON {
					if relayOK {
						fmt.Printf("relay         /healthz  OK (%dms)\n", lat)
					} else if err != nil {
						fmt.Printf("relay         /healthz  FAIL: %v\n", err)
					} else {
						fmt.Printf("relay         /healthz  FAIL: %s\n", resp.Status)
					}
				}
			}

			allOK := akOK && pkOK && relayOK
			report["all_ok"] = allOK
			if asJSON {
				_ = emitJSON(report)
			}
			if !allOK {
				return fmt.Errorf("one or more checks failed")
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit a single JSON report")
	return cmd
}

func effectiveConfigPath() string {
	if cfgFile != "" {
		return cfgFile
	}
	return defaultConfigPath()
}
