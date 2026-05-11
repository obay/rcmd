package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"strings"

	"github.com/obay/rcmd/internal/api"
	"github.com/obay/rcmd/internal/transfer"
	"github.com/spf13/cobra"
)

func newPullCmd() *cobra.Command {
	var (
		agentFlag     string
		asJSON        bool
		chunkSize     int64
		parallel      int
		noCompress    bool
		forceCompress bool
	)
	cmd := &cobra.Command{
		Use:          "pull [flags] REMOTE LOCAL",
		Short:        "Copy a remote file from the agent to a local path (chunked, resumable)",
		Args:         cobra.ExactArgs(2),
		SilenceUsage: true,
		Long: strings.TrimSpace(`
pull downloads REMOTE on the agent to LOCAL on this machine.

The operator sends a 'produce_transfer' command to the agent. The
agent reads REMOTE, optionally compresses it (zstd, with the same
auto-skip heuristic as push), AES-256-GCM-encrypts chunks, and
uploads them to the relay. The operator then downloads each chunk
in parallel, decrypts, decompresses, verifies SHA-256, and writes
LOCAL.

EXAMPLES
  rcmd pull C:\ProgramData\rcmd\agent.log ./agent.log
  rcmd pull --agent win-host-2 C:\big.iso ./big.iso
`),
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

			compression := ""
			switch {
			case noCompress && forceCompress:
				return fmt.Errorf("--no-compress and --compress are mutually exclusive")
			case noCompress:
				compression = transfer.CompressionNone
			case forceCompress:
				compression = transfer.CompressionZstd
			}
			if chunkSize == 0 {
				chunkSize = transfer.DefaultChunkSize
			}

			// Ask the agent to produce a transfer from REMOTE.
			agentRes, err := c.Run(agentID, api.Command{
				Kind:        api.KindProduceTransfer,
				RemotePath:  remote,
				TimeoutSecs: 600,
				ChunkSize:   chunkSize,
				Compression: compression,
			})
			if err != nil {
				return fmt.Errorf("agent dispatch: %w", err)
			}
			if !agentRes.Ok {
				if agentRes.Error != "" {
					return fmt.Errorf("agent: %s", agentRes.Error)
				}
				return fmt.Errorf("agent reported failure")
			}
			if agentRes.TransferID == "" {
				return fmt.Errorf("agent did not return a transfer_id")
			}

			ctx := cmd.Context()
			down := &transfer.Downloader{
				HTTP:     c.http,
				RelayURL: c.relayURL,
				Sign:     c.signer(),
				Parallel: parallel,
			}
			dr, err := down.Download(ctx, agentRes.TransferID, c.masterSecret)
			if err != nil {
				return fmt.Errorf("download: %w", err)
			}
			if err := os.WriteFile(local, dr.Plaintext, 0o600); err != nil {
				return err
			}
			plainSum := sha256.Sum256(dr.Plaintext)
			if err := down.MarkDone(ctx, agentRes.TransferID); err != nil {
				// Non-fatal: relay GC will clean up.
				fmt.Fprintf(os.Stderr, "warn: mark done: %v\n", err)
			}

			if asJSON {
				return emitJSON(map[string]any{
					"kind":         "pull_result",
					"ok":           true,
					"agent_id":     agentID,
					"path_remote":  remote,
					"path_local":   local,
					"transfer_id":  agentRes.TransferID,
					"size":         int64(len(dr.Plaintext)),
					"compression":  dr.Compression,
					"sha256":       hex.EncodeToString(plainSum[:]),
				})
			}
			fmt.Printf("pulled %d bytes from %s on %s -> %s (compression=%s)\n",
				len(dr.Plaintext), remote, agentID, local, dr.Compression)
			return nil
		},
	}
	cmd.Flags().StringVar(&agentFlag, "agent", "", "agent ID (default: from state)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit a single JSON object instead of human text")
	cmd.Flags().Int64Var(&chunkSize, "chunk-size", 0, "plaintext bytes per chunk (default: 1 MiB)")
	cmd.Flags().IntVar(&parallel, "parallel", 0, "concurrent chunk downloads (default: 4)")
	cmd.Flags().BoolVar(&noCompress, "no-compress", false, "disable compression even if the file looks compressible")
	cmd.Flags().BoolVar(&forceCompress, "compress", false, "force compression on (skip the auto-skip heuristic)")
	return cmd
}
