package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Inspect the agent's configuration",
	}
	cmd.AddCommand(newConfigShowCmd())
	return cmd
}

func newConfigShowCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "show",
		Short: "Print the loaded configuration with secrets redacted",
		Long: strings.TrimSpace(`
DESCRIPTION
  show prints the configuration as resolved by the agent (file + env
  vars + defaults), with secret keys redacted to their last 4 chars.

EXAMPLES
  rcmd-agent config show
  rcmd-agent config show --json
`),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := initConfig(); err != nil {
				return err
			}
			settings := redactSecrets(viper.AllSettings())
			if asJSON {
				return emitJSON(map[string]any{
					"kind":        "config",
					"config_path": viper.ConfigFileUsed(),
					"settings":    settings,
				})
			}
			fmt.Printf("config_path = %s\n\n", viper.ConfigFileUsed())
			keys := make([]string, 0, len(settings))
			for k := range settings {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				fmt.Printf("%-22s = %v\n", k, settings[k])
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit a single JSON object")
	return cmd
}

func redactSecrets(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		if isSecretKey(k) {
			if s, ok := v.(string); ok && s != "" {
				out[k] = fmt.Sprintf("…%s (%d chars)", last4(s), len(s))
				continue
			}
		}
		out[k] = v
	}
	return out
}

func isSecretKey(k string) bool {
	lk := strings.ToLower(k)
	return strings.Contains(lk, "key") || strings.Contains(lk, "secret") || strings.Contains(lk, "password")
}
