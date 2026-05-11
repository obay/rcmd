package main

import (
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/obay/obcmd/internal/api"
	"github.com/obay/obcmd/internal/crypto"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func newDoctorCmd() *cobra.Command {
	var (
		asJSON   bool
		skipExec bool
	)
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "End-to-end self-diagnostic for the operator side",
		Long: strings.TrimSpace(`
DESCRIPTION
  doctor runs a top-to-bottom diagnostic:
    1. Resolves and loads the config file.
    2. Validates relay_url + agent_id are set.
    3. Decodes operator_key and payload_key, checks lengths.
    4. Probes the relay's /healthz endpoint.
    5. Runs "echo ok" through the encrypted command path
       (skip with --skip-exec to test only the relay).

  Designed to be run after install and on every config change. Print a
  human-friendly summary by default, or a structured report with --json
  that AI agents and scripts can consume.

EXAMPLES
  obcmd doctor
  obcmd doctor --json
  obcmd doctor --skip-exec     # don't bounce the agent

EXIT CODES
  0   all checks passed
  1   any check failed (see the report for which)
`),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			report := map[string]any{
				"kind":         "doctor",
				"config_path":  effectiveConfigPath(),
				"config_loaded": false,
				"all_ok":       false,
			}

			cl, err := newClient()
			if err != nil {
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
			report["relay_url"] = cl.relayURL
			report["agent_id"] = cl.agentID
			report["operator_key_ok"] = true
			report["operator_key_last4"] = last4(viper.GetString("operator_key"))
			report["payload_key_ok"] = true
			report["payload_key_last4"] = last4(viper.GetString("payload_key"))

			if !asJSON {
				fmt.Printf("config       %s  OK\n", viper.ConfigFileUsed())
				fmt.Printf("relay_url    %s\n", cl.relayURL)
				fmt.Printf("agent_id     %s\n", cl.agentID)
				fmt.Printf("operator_key …%s  OK (%d bytes)\n", report["operator_key_last4"], crypto.KeyBytes)
				fmt.Printf("payload_key  …%s  OK (%d bytes)\n", report["payload_key_last4"], crypto.KeyBytes)
			}

			// Probe relay /healthz.
			t0 := time.Now()
			resp, err := http.Get(cl.relayURL + "/healthz")
			lat := time.Since(t0).Milliseconds()
			relayOK := err == nil && resp != nil && resp.StatusCode == 200
			if resp != nil {
				resp.Body.Close()
			}
			report["relay_ok"] = relayOK
			report["relay_latency_ms"] = lat
			if !relayOK {
				if err != nil {
					report["relay_error"] = err.Error()
				} else {
					report["relay_error"] = resp.Status
				}
			}
			if !asJSON {
				if relayOK {
					fmt.Printf("relay        /healthz  OK (%dms)\n", lat)
				} else if err != nil {
					fmt.Printf("relay        /healthz  FAIL: %v\n", err)
				} else {
					fmt.Printf("relay        /healthz  FAIL: %s\n", resp.Status)
				}
			}

			allOK := relayOK
			if !skipExec && relayOK {
				t1 := time.Now()
				res, runErr := cl.Run(api.Command{
					Kind:        api.KindExec,
					Cmd:         "echo ok",
					Shell:       "cmd",
					TimeoutSecs: 10,
				})
				lat := time.Since(t1).Milliseconds()
				agentOK := runErr == nil && res != nil && res.ExitCode == 0
				report["agent_ok"] = agentOK
				report["agent_latency_ms"] = lat
				if !agentOK {
					if runErr != nil {
						report["agent_error"] = runErr.Error()
					} else if res != nil {
						report["agent_error"] = fmt.Sprintf("exit %d", res.ExitCode)
					}
				}
				if !asJSON {
					if agentOK {
						fmt.Printf("agent        echo ok   OK (%dms)\n", lat)
					} else if runErr != nil {
						fmt.Printf("agent        echo ok   FAIL: %v\n", runErr)
					} else {
						fmt.Printf("agent        echo ok   FAIL: exit %d\n", res.ExitCode)
					}
				}
				allOK = allOK && agentOK
			} else {
				report["agent_ok"] = nil // skipped
			}

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
	cmd.Flags().BoolVar(&skipExec, "skip-exec", false, "skip the agent round-trip (test relay only)")
	return cmd
}

func effectiveConfigPath() string {
	if cfgFile != "" {
		return cfgFile
	}
	return defaultConfigPath()
}

func last4(s string) string {
	if len(s) <= 4 {
		return s
	}
	return s[len(s)-4:]
}
