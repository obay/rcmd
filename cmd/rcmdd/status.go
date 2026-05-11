package main

import (
	"fmt"
	"os"

	"github.com/obay/rcmd/internal/state"
	"github.com/spf13/cobra"
)

func newStatusCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:          "status",
		Short:        "Show relay configuration and quick health checks",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			report := map[string]any{"kind": "status"}

			s, err := state.LoadRelay(statePath)
			report["state_path"] = statePath
			if err != nil {
				report["state_loaded"] = false
				report["error"] = err.Error()
				if asJSON {
					return emitJSON(report)
				}
				fmt.Fprintf(os.Stderr, "state         %s  FAIL: %v\n", statePath, err)
				os.Exit(1)
			}
			report["state_loaded"] = true
			report["tls_mode"] = s.TLSMode
			report["domain"] = s.Domain
			report["listen_addr"] = s.ListenAddr
			report["insecure_addr"] = s.InsecureAddr
			report["agents_seen"] = len(s.Agents)
			report["operators_seen"] = len(s.Operators)

			// ACME cache writable? (autocert mode only)
			if s.TLSMode == "autocert" {
				if err := os.MkdirAll(s.ACMECacheDir, 0o700); err != nil {
					report["acme_cache_ok"] = false
					report["acme_cache_error"] = err.Error()
				} else {
					report["acme_cache_ok"] = true
					report["acme_cache_dir"] = s.ACMECacheDir
				}
			}

			if asJSON {
				return emitJSON(report)
			}
			fmt.Printf("state         %s  OK\n", statePath)
			fmt.Printf("tls_mode      %s\n", s.TLSMode)
			if s.TLSMode == "autocert" {
				fmt.Printf("domain        %s\n", s.Domain)
				fmt.Printf("listen_addr   %s\n", s.ListenAddr)
				if v, ok := report["acme_cache_ok"]; ok && v.(bool) {
					fmt.Printf("acme_cache    %s  OK\n", s.ACMECacheDir)
				} else if v, ok := report["acme_cache_error"]; ok {
					fmt.Printf("acme_cache    %s  FAIL: %s\n", s.ACMECacheDir, v)
				}
			} else {
				fmt.Printf("insecure_addr %s\n", s.InsecureAddr)
			}
			fmt.Printf("seen          %d agents, %d operators\n", len(s.Agents), len(s.Operators))
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit a single JSON object")
	return cmd
}
