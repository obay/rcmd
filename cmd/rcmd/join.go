package main

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/obay/rcmd/internal/crypto"
	"github.com/obay/rcmd/internal/state"
	"github.com/obay/rcmd/internal/token"
	"github.com/spf13/cobra"
)

func newJoinCmd() *cobra.Command {
	var (
		asName        string
		defaultAgent  string
		defaultShell  string
		defaultTimeout int
		force         bool
	)
	cmd := &cobra.Command{
		Use:          "join TOKEN",
		Short:        "Join a relay using a token (writes operator state)",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		Long: strings.TrimSpace(`
join consumes the join token printed by 'rcmdd init' (or 'rcmdd token')
and configures this machine as an rcmd operator.

State is written to ~/.config/rcmd/rcmd.json on macOS/Linux, or
%APPDATA%\rcmd\rcmd.json on Windows. Refuses to overwrite an existing
state file unless --force is given.

The operator ID defaults to $USER@$HOSTNAME (lowercased). Override
with --as NAME.
`),
		RunE: func(cmd *cobra.Command, args []string) error {
			tok, err := token.Parse(args[0])
			if err != nil {
				return err
			}
			master, err := base64.StdEncoding.DecodeString(tok.MasterSecret)
			if err != nil || len(master) != crypto.MasterSecretBytes {
				return errors.New("token: master_secret is missing or malformed")
			}

			if state.Exists(statePath) && !force {
				return fmt.Errorf("state file already exists at %s — use --force to overwrite, or 'rcmd leave' first", statePath)
			}

			operatorID := asName
			if operatorID == "" {
				operatorID = defaultOperatorID()
			}
			if defaultShell == "" {
				defaultShell = "cmd"
			}
			if defaultTimeout <= 0 {
				defaultTimeout = 60
			}

			s := &state.OperatorState{
				RelayURL:           strings.TrimRight(tok.RelayURL, "/"),
				OperatorID:         operatorID,
				MasterSecret:       tok.MasterSecret,
				DefaultAgent:       defaultAgent,
				DefaultShell:       defaultShell,
				DefaultTimeoutSecs: defaultTimeout,
			}
			if err := state.SaveOperator(statePath, s); err != nil {
				return fmt.Errorf("save state: %w", err)
			}
			fmt.Printf("Wrote %s (operator_id=%s)\n", statePath, operatorID)
			if defaultAgent != "" {
				fmt.Printf("Default agent: %s\n", defaultAgent)
			} else {
				fmt.Println("No default agent set. Use --agent NAME per command, or:")
				fmt.Println("  rcmd list-agents             # see what's available")
				fmt.Println("  rcmd set-default-agent NAME  # pin one")
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&asName, "as", "", "operator identity (default: $USER@$HOSTNAME)")
	cmd.Flags().StringVar(&defaultAgent, "default-agent", "", "set the default agent for subsequent commands")
	cmd.Flags().StringVar(&defaultShell, "default-shell", "cmd", "default shell for 'rcmd run' (cmd | powershell)")
	cmd.Flags().IntVar(&defaultTimeout, "default-timeout", 60, "default command timeout in seconds")
	cmd.Flags().BoolVar(&force, "force", false, "overwrite existing state file")
	return cmd
}

func defaultOperatorID() string {
	user := strings.ToLower(os.Getenv("USER"))
	if user == "" {
		if runtime.GOOS == "windows" {
			user = strings.ToLower(os.Getenv("USERNAME"))
		}
	}
	if user == "" {
		user = "operator"
	}
	host, err := os.Hostname()
	if err != nil || host == "" {
		host = "host"
	}
	return user + "@" + strings.ToLower(host)
}
