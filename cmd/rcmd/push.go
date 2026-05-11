package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/obay/rcmd/internal/api"
	"github.com/obay/rcmd/internal/transfer"
	"github.com/spf13/cobra"
)

func newPushCmd() *cobra.Command {
	var (
		agentFlag    string
		asJSON       bool
		chunkSize    int64
		parallel     int
		noCompress   bool
		forceCompress bool
	)
	cmd := &cobra.Command{
		Use:          "push [flags] LOCAL REMOTE",
		Short:        "Copy a local file to the remote agent (chunked, resumable, optionally compressed)",
		Args:         cobra.ExactArgs(2),
		SilenceUsage: true,
		Long: strings.TrimSpace(`
push uploads LOCAL on this machine to REMOTE on the agent.

Each push is split into chunks (default 1 MiB), each chunk is
AES-256-GCM-encrypted with a per-transfer key derived from the
master secret via HKDF, and chunks are uploaded to the relay in
parallel with exponential-backoff retry. Once all chunks are
present, the relay signals the agent, which downloads the chunks,
decrypts, verifies the SHA-256 of the plaintext, and writes the
file.

Compression: if zstd shrinks the payload by ~8%+ on a 64-KB sample
(and the filename's extension isn't already-compressed-looking like
.zip / .jpg / .mp4), zstd is applied to the whole payload before
chunking. --no-compress forces it off; --compress forces it on.

EXAMPLES
  rcmd push ./hosts C:\Windows\System32\drivers\etc\hosts
  rcmd push --agent win-host-2 ./build.zip C:\releases\build.zip
  rcmd push --chunk-size 4194304 --parallel 8 ./big.iso C:\images\big.iso
`),
		RunE: func(cmd *cobra.Command, args []string) error {
			local, remote := args[0], args[1]
			data, err := os.ReadFile(local)
			if err != nil {
				return err
			}
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

			ctx := cmd.Context()
			up := &transfer.Uploader{
				HTTP:     c.http,
				RelayURL: c.relayURL,
				Sign:     c.signer(),
				Parallel: parallel,
			}
			res, err := up.Upload(ctx, data, transfer.UploadOpts{
				Direction:    string(transfer.DirectionPush),
				AgentID:      agentID,
				RemotePath:   remote,
				ChunkSize:    chunkSize,
				Compression:  compression,
				Filename:     filepath.Base(local),
				MasterSecret: c.masterSecret,
			})
			if err != nil {
				return fmt.Errorf("upload: %w", err)
			}

			// Tell the agent to fetch the transfer and write the file.
			agentRes, err := c.Run(agentID, api.Command{
				Kind:        api.KindFetchTransfer,
				RemotePath:  remote,
				TimeoutSecs: 600,
				TransferID:  res.TransferID,
				TotalChunks: res.TotalChunks,
				ChunkSize:   res.ChunkSize,
				TotalSize:   res.TotalSize,
				SHA256Hex:   res.SHA256Hex,
				Compression: res.Compression,
			})
			if err != nil {
				return fmt.Errorf("agent dispatch: %w", err)
			}
			if !agentRes.Ok {
				if asJSON {
					_ = emitJSON(map[string]any{
						"kind":        "push_result",
						"ok":          false,
						"path_local":  local,
						"path_remote": remote,
						"transfer_id": res.TransferID,
						"error":       agentRes.Error,
					})
				}
				if agentRes.Error != "" {
					return fmt.Errorf("agent: %s", agentRes.Error)
				}
				return fmt.Errorf("agent reported failure")
			}

			plainSum := sha256.Sum256(data)
			if asJSON {
				return emitJSON(map[string]any{
					"kind":          "push_result",
					"ok":            true,
					"agent_id":      agentID,
					"path_local":    local,
					"path_remote":   remote,
					"transfer_id":   res.TransferID,
					"size":          int64(len(data)),
					"chunks":        res.TotalChunks,
					"chunk_size":    res.ChunkSize,
					"compression":   res.Compression,
					"bytes_written": agentRes.BytesWritten,
					"sha256":        hex.EncodeToString(plainSum[:]),
				})
			}
			fmt.Printf("pushed %d bytes (%d chunks, compression=%s) to %s on %s\n",
				len(data), res.TotalChunks, res.Compression, remote, agentID)
			return nil
		},
	}
	cmd.Flags().StringVar(&agentFlag, "agent", "", "agent ID (default: from state)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit a single JSON object instead of human text")
	cmd.Flags().Int64Var(&chunkSize, "chunk-size", 0, "plaintext bytes per chunk (default: 1 MiB)")
	cmd.Flags().IntVar(&parallel, "parallel", 0, "concurrent chunk uploads (default: 4)")
	cmd.Flags().BoolVar(&noCompress, "no-compress", false, "disable compression even if the file looks compressible")
	cmd.Flags().BoolVar(&forceCompress, "compress", false, "force compression on (skip the auto-skip heuristic)")
	return cmd
}
