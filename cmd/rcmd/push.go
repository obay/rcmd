package main

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"os"
	"strings"

	"github.com/obay/rcmd/internal/api"
	"github.com/spf13/cobra"
)

func newPushCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "push LOCAL REMOTE",
		Short: "Copy a local file to the remote agent",
		Long: strings.TrimSpace(fmt.Sprintf(`
DESCRIPTION
  push reads LOCAL on this machine and writes it to REMOTE on the agent.
  Max file size: %s (v1 hard cap). The file is encrypted end-to-end —
  the relay only sees ciphertext.

EXAMPLES
  rcmd push ./hosts C:\Windows\System32\drivers\etc\hosts
  rcmd push --json ./hosts C:\hosts.bak
  # -> {"kind":"push_result","ok":true,"bytes_written":237,
  #     "path_local":"./hosts","path_remote":"C:\\hosts.bak",
  #     "sha256":"<hex>"}

EXIT CODES
  0   success
  1   local-read, transport, or agent-side error
`, humanBytes(api.MaxFileBytes))),
		Args:         cobra.ExactArgs(2),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			local, remote := args[0], args[1]
			info, err := os.Stat(local)
			if err != nil {
				return err
			}
			if info.Size() > int64(api.MaxFileBytes) {
				return fmt.Errorf("file is %d bytes; exceeds %s limit", info.Size(), humanBytes(api.MaxFileBytes))
			}
			data, err := os.ReadFile(local)
			if err != nil {
				return err
			}
			sum := sha256.Sum256(data)
			cl, err := newClient()
			if err != nil {
				return err
			}
			res, err := cl.Run(api.Command{
				Kind:        api.KindPush,
				RemotePath:  remote,
				DataB64:     base64.StdEncoding.EncodeToString(data),
				TimeoutSecs: 60,
			})
			if err != nil {
				return err
			}
			if !res.Ok {
				if asJSON {
					_ = emitJSON(map[string]any{
						"kind":        "push_result",
						"ok":          false,
						"path_local":  local,
						"path_remote": remote,
						"error":       res.Error,
					})
				}
				if res.Error != "" {
					return fmt.Errorf("agent: %s", res.Error)
				}
				return fmt.Errorf("agent reported failure")
			}
			if asJSON {
				return emitJSON(map[string]any{
					"kind":          "push_result",
					"ok":            true,
					"agent_id":      cl.agentID,
					"bytes_written": res.BytesWritten,
					"path_local":    local,
					"path_remote":   remote,
					"sha256":        hex.EncodeToString(sum[:]),
				})
			}
			fmt.Printf("wrote %d bytes to %s on %s\n", res.BytesWritten, remote, cl.agentID)
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit a single JSON object instead of human text")
	return cmd
}
