package main

import (
	"fmt"
	"strings"

	"github.com/obay/rcmd/internal/crypto"
	"github.com/spf13/cobra"
)

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

  rcmd uses three keys, all 32 bytes:
    agent_key    — agent ↔ relay HMAC
    operator_key — operator ↔ relay HMAC
    payload_key  — AES-256-GCM key shared by operator and agent
                   (the relay never sees it)

EXAMPLES
  rcmdd keygen --count 2     # one agent_key, one operator_key
  rcmdd keygen --count 3 --json
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
