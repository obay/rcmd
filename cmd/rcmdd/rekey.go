package main

import (
	"bufio"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/obay/rcmd/internal/crypto"
	"github.com/obay/rcmd/internal/state"
	"github.com/obay/rcmd/internal/token"
	"github.com/spf13/cobra"
)

func newRekeyCmd() *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:          "rekey",
		Short:        "Rotate the master secret (invalidates ALL existing agents and operators)",
		SilenceUsage: true,
		Long: strings.TrimSpace(`
rekey generates a fresh master secret, persists it, and prints the new
join token. After rekey:

  - The previous token is dead. Anyone using it will fail HMAC
    verification immediately.
  - Every operator and every agent must re-join with the new token.
  - The relay must be restarted (systemctl restart rcmdd) for the
    new key to take effect.

Refuses to run without confirmation. Pass --yes to skip the prompt
(useful from scripts).
`),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := state.LoadRelay(statePath)
			if err != nil {
				return err
			}
			if !yes {
				fmt.Fprintln(os.Stderr, "About to rotate the master secret.")
				fmt.Fprintln(os.Stderr, "All currently-joined agents and operators will be locked out.")
				fmt.Fprint(os.Stderr, "Type 'rekey' to confirm: ")
				rd := bufio.NewReader(os.Stdin)
				line, _ := rd.ReadString('\n')
				if strings.TrimSpace(line) != "rekey" {
					return errors.New("aborted")
				}
			}

			master, err := crypto.NewMasterSecret()
			if err != nil {
				return fmt.Errorf("generate master secret: %w", err)
			}
			s.MasterSecret = base64.StdEncoding.EncodeToString(master)
			s.Agents = map[string]state.Identity{}
			s.Operators = map[string]state.Identity{}
			if err := state.SaveRelay(statePath, s); err != nil {
				return fmt.Errorf("save state: %w", err)
			}

			tok, err := token.Mint(token.Token{RelayURL: tokenURL(s), MasterSecret: s.MasterSecret})
			if err != nil {
				return fmt.Errorf("mint token: %w", err)
			}
			fmt.Println("Master secret rotated.")
			fmt.Println()
			fmt.Println("  1. Restart the relay:")
			fmt.Println()
			fmt.Println("       sudo systemctl restart rcmdd")
			fmt.Println()
			fmt.Println("  2. On each Windows agent (elevated PowerShell):")
			fmt.Println()
			fmt.Printf("       rcmd-agent join %s --force\n", tok)
			fmt.Println()
			fmt.Println("  3. On each operator machine:")
			fmt.Println()
			fmt.Printf("       rcmd join %s --force\n", tok)
			return nil
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "skip the confirmation prompt")
	return cmd
}
