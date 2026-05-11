package main

import (
	"context"
	"net/http"
	"os"
	"path/filepath"

	"github.com/obay/rcmd/internal/api"
	"github.com/obay/rcmd/internal/auth"
	"github.com/obay/rcmd/internal/transfer"
)

// signer returns the agent's HMAC-attaching closure for the chunked
// transfer client.
func (a *agent) signer() func(req *http.Request, body []byte) error {
	return func(req *http.Request, body []byte) error {
		return auth.Sign(req, a.agentID, a.hmacKey, body)
	}
}

// handleFetchTransfer is the agent's side of an operator-initiated
// push. The operator has uploaded ciphertext chunks to the relay; the
// agent now downloads them, decrypts, verifies checksum, and writes
// the file at the requested remote path.
func (a *agent) handleFetchTransfer(ctx context.Context, master []byte, cmd api.Command) api.Result {
	if cmd.TransferID == "" || cmd.RemotePath == "" {
		return api.Result{Ok: false, Error: "fetch_transfer: transfer_id and remote_path required"}
	}
	down := &transfer.Downloader{
		HTTP:     a.http,
		RelayURL: a.relayURL,
		Sign:     a.signer(),
	}
	dr, err := down.Download(ctx, cmd.TransferID, master)
	if err != nil {
		return api.Result{Ok: false, Error: "download: " + err.Error()}
	}
	if err := os.MkdirAll(filepath.Dir(cmd.RemotePath), 0o755); err != nil {
		return api.Result{Ok: false, Error: "mkdir: " + err.Error()}
	}
	if err := os.WriteFile(cmd.RemotePath, dr.Plaintext, 0o600); err != nil {
		return api.Result{Ok: false, Error: "write: " + err.Error()}
	}
	// Tell the relay we're done so the chunk files get GC'd quickly.
	_ = down.MarkDone(ctx, cmd.TransferID)
	a.log.Printf("fetch_transfer %s -> %s (%d bytes)", cmd.TransferID, cmd.RemotePath, len(dr.Plaintext))
	return api.Result{Ok: true, BytesWritten: int64(len(dr.Plaintext))}
}

// handleProduceTransfer is the agent's side of an operator-initiated
// pull. The agent reads the local file, chunks/encrypts it, uploads
// to the relay as a "pull" transfer, and returns the transfer_id in
// the result so the operator can download.
func (a *agent) handleProduceTransfer(ctx context.Context, master []byte, cmd api.Command) api.Result {
	if cmd.RemotePath == "" {
		return api.Result{Ok: false, Error: "produce_transfer: remote_path required"}
	}
	data, err := os.ReadFile(cmd.RemotePath)
	if err != nil {
		return api.Result{Ok: false, Error: err.Error()}
	}
	up := &transfer.Uploader{
		HTTP:     a.http,
		RelayURL: a.relayURL,
		Sign:     a.signer(),
	}
	res, err := up.Upload(ctx, data, transfer.UploadOpts{
		Direction:    string(transfer.DirectionPull),
		AgentID:      a.agentID,
		RemotePath:   cmd.RemotePath,
		ChunkSize:    cmd.ChunkSize,
		Compression:  cmd.Compression,
		Filename:     filepath.Base(cmd.RemotePath),
		MasterSecret: master,
	})
	if err != nil {
		return api.Result{Ok: false, Error: "upload: " + err.Error()}
	}
	a.log.Printf("produce_transfer %s for %s (%d bytes, %d chunks)", res.TransferID, cmd.RemotePath, res.TotalSize, res.TotalChunks)
	return api.Result{
		Ok:         true,
		TransferID: res.TransferID,
		Size:       int64(len(data)),
	}
}
