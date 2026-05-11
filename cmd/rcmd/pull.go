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

func newPullCmd() *cobra.Command {
	var (
		agentFlag string
		asJSON    bool
	)
	cmd := &cobra.Command{
		Use:   "pull [flags] REMOTE LOCAL",
		Short: "Copy a remote file from the agent to a local path",
		Long: strings.TrimSpace(fmt.Sprintf(`
pull reads REMOTE on the agent and writes it to LOCAL on this machine.
Max file size: %s (v1 hard cap). The file is encrypted end-to-end —
the relay only sees ciphertext.

EXAMPLES
  rcmd pull C:\ProgramData\rcmd\agent.log ./agent.log
  rcmd pull --agent win-host-2 C:\hosts.bak ./hosts.bak
`, humanBytes(api.MaxFileBytes))),
		Args:         cobra.ExactArgs(2),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			remote, local := args[0], args[1]
			c, err := newClient()
			if err != nil {
				return err
			}
			agentID, err := c.resolveAgent(agentFlag)
			if err != nil {
				return err
			}
			res, err := c.Run(agentID, api.Command{
				Kind:        api.KindPull,
				RemotePath:  remote,
				TimeoutSecs: 60,
			})
			if err != nil {
				return err
			}
			if !res.Ok {
				if asJSON {
					_ = emitJSON(map[string]any{
						"kind":        "pull_result",
						"ok":          false,
						"path_remote": remote,
						"path_local":  local,
						"error":       res.Error,
					})
				}
				if res.Error != "" {
					return fmt.Errorf("agent: %s", res.Error)
				}
				return fmt.Errorf("agent reported failure")
			}
			data, err := base64.StdEncoding.DecodeString(res.DataB64)
			if err != nil {
				return fmt.Errorf("decode payload: %w", err)
			}
			if err := os.WriteFile(local, data, 0o600); err != nil {
				return err
			}
			sum := sha256.Sum256(data)
			if asJSON {
				return emitJSON(map[string]any{
					"kind":        "pull_result",
					"ok":          true,
					"agent_id":    agentID,
					"size":        res.Size,
					"path_remote": remote,
					"path_local":  local,
					"sha256":      hex.EncodeToString(sum[:]),
				})
			}
			fmt.Printf("read %d bytes from %s on %s -> %s\n", res.Size, remote, agentID, local)
			return nil
		},
	}
	cmd.Flags().StringVar(&agentFlag, "agent", "", "agent ID (default: from state)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit a single JSON object instead of human text")
	return cmd
}
