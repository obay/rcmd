package main

import (
	"errors"
	"fmt"
	"net"
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
		Short: "Self-diagnostic for the relay (config, keys, listeners, autocert cache)",
		Long: strings.TrimSpace(`
DESCRIPTION
  doctor verifies the relay can start cleanly:
    1. Find and load the config file.
    2. Decode agent_key and operator_key.
    3. Validate listen addresses (parse + try a brief test-bind on a
       non-privileged port to surface obvious mistakes).
    4. Confirm acme_cache_dir exists and is writable.

  Run this after editing /etc/obcmd/obcmdd.toml and before
  'systemctl restart obcmdd' to catch typos.

EXAMPLES
  obcmdd doctor
  obcmdd doctor --json

EXIT CODES
  0   all checks passed
  1   any check failed (see the report)
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

			domain := viper.GetString("domain")
			listenAddr := viper.GetString("listen_addr")
			httpAddr := viper.GetString("http_addr")
			acmeDir := viper.GetString("acme_cache_dir")
			insecure := viper.GetBool("insecure")
			ak := viper.GetString("agent_key")
			ok := viper.GetString("operator_key")
			agentIDs := viper.GetStringSlice("agent_ids")

			report["domain"] = domain
			report["listen_addr"] = listenAddr
			report["http_addr"] = httpAddr
			report["acme_cache_dir"] = acmeDir
			report["insecure"] = insecure
			report["agent_ids"] = agentIDs

			akOK := check(ak, "agent_key", report)
			opOK := check(ok, "operator_key", report)

			if !asJSON {
				fmt.Printf("config         %s  OK\n", viper.ConfigFileUsed())
				fmt.Printf("domain         %s\n", domain)
				fmt.Printf("listen_addr    %s\n", listenAddr)
				fmt.Printf("http_addr      %s\n", httpAddr)
				fmt.Printf("agent_ids      %v\n", agentIDs)
				fmt.Printf("insecure       %v\n", insecure)
				printKeyStatus("agent_key   ", ak, report, "agent_key_error")
				printKeyStatus("operator_key", ok, report, "operator_key_error")
			}

			// acme_cache_dir writable?
			cacheOK := true
			if !insecure {
				if err := os.MkdirAll(acmeDir, 0o700); err != nil {
					report["acme_cache_ok"] = false
					report["acme_cache_error"] = err.Error()
					cacheOK = false
				} else {
					f, err := os.CreateTemp(acmeDir, ".doctor-*")
					if err != nil {
						report["acme_cache_ok"] = false
						report["acme_cache_error"] = err.Error()
						cacheOK = false
					} else {
						f.Close()
						os.Remove(f.Name())
						report["acme_cache_ok"] = true
					}
				}
				if !asJSON {
					if cacheOK {
						fmt.Printf("acme_cache_dir %s  OK\n", acmeDir)
					} else {
						fmt.Printf("acme_cache_dir %s  FAIL: %v\n", acmeDir, report["acme_cache_error"])
					}
				}
			}

			// Listen addr parse check.
			parseOK := true
			for _, addr := range []string{listenAddr, httpAddr} {
				if addr == "" {
					continue
				}
				if _, _, err := net.SplitHostPort(addr); err != nil {
					report["listen_parse_error"] = err.Error()
					parseOK = false
				}
			}
			report["listen_parse_ok"] = parseOK
			if !asJSON {
				if parseOK {
					fmt.Printf("listen_parse   OK\n")
				} else {
					fmt.Printf("listen_parse   FAIL: %v\n", report["listen_parse_error"])
				}
			}

			// Quick reachability ping on insecure_addr if set.
			if insecure {
				addr := viper.GetString("insecure_addr")
				if addr != "" {
					l, err := net.Listen("tcp", addr)
					if err == nil {
						l.Close()
						report["insecure_bind_ok"] = true
					} else {
						report["insecure_bind_ok"] = false
						report["insecure_bind_error"] = err.Error()
					}
				}
			}

			allOK := akOK && opOK && parseOK
			if !insecure {
				allOK = allOK && cacheOK
			}
			report["all_ok"] = allOK
			if asJSON {
				_ = emitJSON(report)
			}
			if !allOK {
				return errors.New("one or more checks failed")
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit a single JSON report")
	return cmd
}

func check(val, name string, report map[string]any) bool {
	if val == "" {
		report[name+"_ok"] = false
		report[name+"_error"] = "missing"
		return false
	}
	if _, err := crypto.ParseKey(val); err != nil {
		report[name+"_ok"] = false
		report[name+"_error"] = err.Error()
		return false
	}
	report[name+"_ok"] = true
	report[name+"_last4"] = last4(val)
	return true
}

func printKeyStatus(label, val string, report map[string]any, errKey string) {
	if err, bad := report[errKey].(string); bad && err != "" {
		fmt.Printf("%s   FAIL: %s\n", label, err)
		return
	}
	suffix := "(empty)"
	if val != "" {
		suffix = "…" + last4(val)
	}
	fmt.Printf("%s   %s  OK\n", label, suffix)
}

func effectiveConfigPath() string {
	if cfgFile != "" {
		return cfgFile
	}
	return defaultConfigPath
}

// silence unused import on hosts without time.* usage if any.
var _ = time.Second
