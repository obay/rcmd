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
		asName    string
		shell     string
		logFile   string
		force     bool
		skipSvc   bool
	)
	cmd := &cobra.Command{
		Use:          "join TOKEN",
		Short:        "Join a relay using a token (writes state, installs Windows service)",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		Long: strings.TrimSpace(`
join consumes the join token printed by 'rcmdd init' (or 'rcmdd token')
and configures this host to act as an agent.

What it does:
  1. Parses the token (relay URL + master secret).
  2. Writes the state file at %PROGRAMDATA%\rcmd\rcmd-agent.json
     (or ~/.config/rcmd/rcmd-agent.json on non-Windows dev builds).
  3. On Windows, registers the SCM service and starts it. Use
     --skip-service to write state only and start later by hand.

The agent ID defaults to this host's name (lowercased). Override with
--as NAME.

Refuses to overwrite an existing state file unless --force is given.
`),
		RunE: func(cmd *cobra.Command, args []string) error {
			tok, err := token.Parse(args[0])
			if err != nil {
				return err
			}
			// Validate master secret size early.
			master, err := base64.StdEncoding.DecodeString(tok.MasterSecret)
			if err != nil || len(master) != crypto.MasterSecretBytes {
				return errors.New("token: master_secret is missing or malformed")
			}

			if state.Exists(statePath) && !force {
				return fmt.Errorf("state file already exists at %s — use --force to overwrite, or 'rcmd-agent leave' first", statePath)
			}

			agentID := asName
			if agentID == "" {
				h, err := os.Hostname()
				if err != nil {
					return fmt.Errorf("hostname: %w", err)
				}
				agentID = strings.ToLower(h)
			}
			if shell == "" {
				if runtime.GOOS == "windows" {
					shell = "cmd"
				} else {
					shell = "sh"
				}
			}
			if logFile == "" {
				logFile = defaultLogPath()
			}

			s := &state.AgentState{
				RelayURL:     strings.TrimRight(tok.RelayURL, "/"),
				AgentID:      agentID,
				MasterSecret: tok.MasterSecret,
				DefaultShell: shell,
				LogFile:      logFile,
			}
			if err := state.SaveAgent(statePath, s); err != nil {
				return fmt.Errorf("save state: %w", err)
			}
			fmt.Printf("Wrote %s (agent_id=%s)\n", statePath, agentID)

			if skipSvc {
				fmt.Println("Skipped service install (--skip-service).")
				return nil
			}
			if runtime.GOOS != "windows" {
				fmt.Println("Non-Windows host: skipping service install. Use 'rcmd-agent run' to start the agent loop in the foreground.")
				return nil
			}
			exe, err := os.Executable()
			if err != nil {
				return fmt.Errorf("locate executable: %w", err)
			}
			return installService(exe)
		},
	}
	cmd.Flags().StringVar(&asName, "as", "", "agent identity (default: lowercased hostname)")
	cmd.Flags().StringVar(&shell, "default-shell", "", "default shell for exec commands (default: cmd on Windows, sh elsewhere)")
	cmd.Flags().StringVar(&logFile, "log-file", "", "log file path (default: platform-conventional)")
	cmd.Flags().BoolVar(&force, "force", false, "overwrite existing state file")
	cmd.Flags().BoolVar(&skipSvc, "skip-service", false, "write state only; do not install the Windows service")
	return cmd
}
