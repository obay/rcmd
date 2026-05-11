package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"

	"github.com/obay/rcmd/internal/crypto"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

const ServiceName = "rcmd-agent"

var cfgFile string

func defaultConfigPath() string {
	switch runtime.GOOS {
	case "windows":
		pd := os.Getenv("PROGRAMDATA")
		if pd == "" {
			pd = `C:\ProgramData`
		}
		return filepath.Join(pd, "rcmd", "agent.toml")
	case "darwin":
		return "/usr/local/etc/rcmd/agent.toml"
	default:
		return "/etc/rcmd/agent.toml"
	}
}

func defaultLogPath() string {
	switch runtime.GOOS {
	case "windows":
		pd := os.Getenv("PROGRAMDATA")
		if pd == "" {
			pd = `C:\ProgramData`
		}
		return filepath.Join(pd, "rcmd", "agent.log")
	case "darwin":
		return "/usr/local/var/log/rcmd-agent.log"
	default:
		return "/var/log/rcmd-agent.log"
	}
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "rcmd-agent",
		Short: "rcmd remote-execution agent",
		Long: strings.TrimSpace(fmt.Sprintf(`
rcmd-agent runs on the target Windows host and polls the rcmd relay
over HTTPS for encrypted commands. Outbound connections only — no
inbound listening.

Quick start:
  rcmd-agent keygen --count 2     # one agent_key, one payload_key
  $EDITOR %s
  rcmd-agent run                  # run in foreground (good for first-time testing)

Windows service install:
  rcmd-agent install              # registers and starts the Windows service
  rcmd-agent uninstall            # stops and removes the service

Config (key = value):
  relay_url      = "https://relay.example.com"
  agent_id       = "win-host"
  agent_key      = "<base64 32B>"   # shared with relay
  payload_key    = "<base64 32B>"   # shared with operator
  log_file       = "%s"
  default_shell  = "cmd"            # cmd | powershell (Windows); sh | pwsh elsewhere
`, defaultConfigPath(), defaultLogPath())),
		SilenceUsage: true,
	}
	root.PersistentFlags().StringVar(&cfgFile, "config", "",
		fmt.Sprintf("config file (default %s)", defaultConfigPath()))

	root.AddCommand(
		newRunCmd(),
		newDoctorCmd(),
		newConfigCmd(),
		newKeygenCmd(),
		newVersionCmd(),
	)
	addServiceCommands(root)
	return root
}

func initConfig() error {
	if cfgFile != "" {
		viper.SetConfigFile(cfgFile)
	} else {
		viper.SetConfigFile(defaultConfigPath())
	}
	viper.SetEnvPrefix("RCMD_AGENT")
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	viper.AutomaticEnv()

	viper.SetDefault("agent_id", "win-host")
	viper.SetDefault("log_file", defaultLogPath())
	if runtime.GOOS == "windows" {
		viper.SetDefault("default_shell", "cmd")
	} else {
		viper.SetDefault("default_shell", "sh")
	}

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
  Print binary version, Go toolchain version, and platform tag.

EXAMPLES
  rcmd-agent version
  rcmd-agent version --json
  # -> {"kind":"version","name":"rcmd-agent","version":"v0.1.0",
  #     "go":"go1.26.2","platform":"windows/amd64"}
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
			platform := runtime.GOOS + "/" + runtime.GOARCH
			if asJSON {
				return emitJSON(map[string]any{
					"kind":     "version",
					"name":     "rcmd-agent",
					"version":  version,
					"go":       gover,
					"platform": platform,
				})
			}
			fmt.Printf("rcmd-agent %s\n", version)
			fmt.Printf("go           %s\n", gover)
			fmt.Printf("platform     %s\n", platform)
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
  Print one or more freshly generated 32-byte random keys, base64-encoded.
  The agent needs two: agent_key (shared with the relay) and
  payload_key (shared with the operator).

EXAMPLES
  rcmd-agent keygen --count 2
  rcmd-agent keygen --count 2 --json
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
