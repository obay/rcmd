package main

import (
	"encoding/json"
	"fmt"
	"os"
	"runtime/debug"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

const defaultConfigPath = "/etc/obcmd/obcmdd.toml"

var cfgFile string

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "obcmdd",
		Short: "obcmd relay server",
		Long: strings.TrimSpace(`
obcmdd is the obcmd relay server. It runs on a Linux host at a domain
you own and brokers encrypted commands between the obcmd operator CLI
and the obcmd-agent (Windows).

The relay only ever sees encrypted envelopes — command text, file
contents, and output never appear in cleartext on the relay. All
parties authenticate to the relay with HMAC-signed requests.

Typical workflow:
  obcmdd keygen                # generate keys
  obcmdd serve                 # run the server
`),
		SilenceUsage: true,
	}
	root.PersistentFlags().StringVar(&cfgFile, "config", "",
		fmt.Sprintf("config file (default %s)", defaultConfigPath))

	root.AddCommand(newServeCmd(), newDoctorCmd(), newConfigCmd(), newKeygenCmd(), newVersionCmd())
	return root
}

func initConfig() error {
	if cfgFile != "" {
		viper.SetConfigFile(cfgFile)
	} else {
		viper.SetConfigFile(defaultConfigPath)
	}
	viper.SetEnvPrefix("OBCMDD")
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	viper.AutomaticEnv()

	viper.SetDefault("listen_addr", ":443")
	viper.SetDefault("acme_cache_dir", "/var/lib/obcmd/autocert")
	viper.SetDefault("agent_ids", []string{"win-host"})
	viper.SetDefault("insecure", false)
	viper.SetDefault("insecure_addr", ":8080")

	if err := viper.ReadInConfig(); err != nil {
		return fmt.Errorf("read config %s: %w", viper.ConfigFileUsed(), err)
	}
	return nil
}

func newVersionCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "version",
		Short: "Print build info and exit",
		Long: strings.TrimSpace(`
DESCRIPTION
  Print binary version and Go toolchain version.

EXAMPLES
  obcmdd version
  obcmdd version --json
`),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			info, _ := debug.ReadBuildInfo()
			version := "(unknown)"
			gover := "(unknown)"
			if info != nil {
				version = info.Main.Version
				gover = info.GoVersion
			}
			if asJSON {
				return emitJSON(map[string]any{
					"kind":    "version",
					"name":    "obcmdd",
					"version": version,
					"go":      gover,
				})
			}
			fmt.Printf("obcmdd %s\n", version)
			fmt.Printf("go      %s\n", gover)
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit a single JSON object")
	return cmd
}

func emitJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func last4(s string) string {
	if len(s) <= 4 {
		return s
	}
	return s[len(s)-4:]
}
