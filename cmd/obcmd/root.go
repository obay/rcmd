package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"

	"github.com/obay/obcmd/internal/crypto"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var cfgFile string

func defaultConfigPath() string {
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".config", "obcmd", "config.toml")
	}
	return "obcmd.toml"
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "obcmd",
		Short: "Operator CLI for the obcmd remote-exec relay",
		Long: strings.TrimSpace(`
obcmd is the operator CLI for obcmd. It ships AES-256-GCM encrypted
commands and small files to a remote Windows agent via a self-hosted
relay over plain HTTPS — so it works through aggressive corporate
firewalls that block SSH and inspect TLS.

Quick start:
  obcmd keygen --count 3            # generate keys (one each: agent_key, operator_key, payload_key)
  $EDITOR ~/.config/obcmd/config.toml
  obcmd run "hostname"              # run a command on the agent
  obcmd push notes.txt C:\Users\Public\notes.txt
  obcmd pull C:\Windows\System32\drivers\etc\hosts hosts.remote

Config (key = value):
  relay_url            = "https://ai.obay.cloud"
  agent_id             = "win-host"
  operator_key         = "<base64 32B>"
  payload_key          = "<base64 32B>"
  default_shell        = "cmd"        # or "powershell"
  default_timeout_secs = 60
`),
		SilenceUsage: true,
	}
	root.PersistentFlags().StringVar(&cfgFile, "config", "",
		fmt.Sprintf("config file (default %s)", defaultConfigPath()))

	root.AddCommand(
		newRunCmd(),
		newPushCmd(),
		newPullCmd(),
		newStatusCmd(),
		newDoctorCmd(),
		newConfigCmd(),
		newKeygenCmd(),
		newVersionCmd(),
	)
	return root
}

func initConfig() error {
	if cfgFile != "" {
		viper.SetConfigFile(cfgFile)
	} else {
		viper.SetConfigFile(defaultConfigPath())
	}
	viper.SetEnvPrefix("OBCMD")
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	viper.AutomaticEnv()

	viper.SetDefault("agent_id", "win-host")
	viper.SetDefault("default_shell", "cmd")
	viper.SetDefault("default_timeout_secs", 60)

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
  obcmd version
  obcmd version --json
  # -> {"kind":"version","name":"obcmd","version":"v0.1.0","go":"go1.26.2"}
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
					"name":    "obcmd",
					"version": version,
					"go":      gover,
				})
			}
			fmt.Printf("obcmd %s\n", version)
			fmt.Printf("go     %s\n", gover)
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit a single JSON object")
	return cmd
}

func newKeygenCmd() *cobra.Command {
	var (
		count  int
		asJSON bool
	)
	cmd := &cobra.Command{
		Use:   "keygen",
		Short: "Generate base64-encoded 32-byte keys",
		Long: strings.TrimSpace(`
DESCRIPTION
  keygen prints freshly generated 32-byte random keys, base64-encoded.

  obcmd uses three keys, all 32 bytes:
    agent_key    — agent ↔ relay HMAC
    operator_key — operator ↔ relay HMAC
    payload_key  — AES-256-GCM key shared by operator and agent
                   (the relay never sees it)

EXAMPLES
  obcmd keygen                # one key
  obcmd keygen --count 3      # three keys (one per role)
  obcmd keygen --count 3 --json
  # -> {"kind":"keygen","count":3,"keys":["...","...","..."]}

WHERE TO PUT THEM
  relay    /etc/obcmd/obcmdd.toml             agent_key + operator_key
  agent    %PROGRAMDATA%\obcmd\agent.toml      agent_key + payload_key
  operator ~/.config/obcmd/config.toml         operator_key + payload_key
`),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			keys := make([]string, 0, count)
			for i := 0; i < count; i++ {
				k, err := crypto.NewKey()
				if err != nil {
					return err
				}
				keys = append(keys, k)
			}
			if asJSON {
				return emitJSON(map[string]any{
					"kind":  "keygen",
					"count": count,
					"keys":  keys,
				})
			}
			for _, k := range keys {
				fmt.Println(k)
			}
			return nil
		},
	}
	cmd.Flags().IntVarP(&count, "count", "n", 1, "how many keys to print")
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit a single JSON object")
	return cmd
}
