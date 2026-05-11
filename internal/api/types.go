// Package api defines the wire format for rcmd — header names, kinds,
// and the JSON-serializable structs that flow through the relay.
//
// Everything in this package is intentionally simple: it's the boundary
// between the three binaries and must be cheap to keep stable.
package api

const (
	HeaderTimestamp = "X-Rcmd-Timestamp"
	HeaderNonce     = "X-Rcmd-Nonce"
	HeaderSig       = "X-Rcmd-Sig"
	HeaderIdentity  = "X-Rcmd-Identity"

	KindExec = "exec"
	KindPush = "push"
	KindPull = "pull"

	ShellCmd        = "cmd"
	ShellPowerShell = "powershell"

	// MaxFileBytes is the v1 hard cap for push/pull payloads (16 MiB).
	MaxFileBytes = 16 * 1024 * 1024

	// MaxOutputBytes caps captured stdout+stderr per command (8 MiB).
	MaxOutputBytes = 8 * 1024 * 1024

	// PollTimeout is how long the relay holds an empty poll before
	// returning 204. Keep below typical proxy idle timeouts.
	PollTimeoutSeconds = 25

	// ResultTimeout is how long the operator's result long-poll waits
	// before returning 202.
	ResultTimeoutSeconds = 25

	// ClockSkewSeconds is the allowed ±window for request timestamps.
	ClockSkewSeconds = 60
)

// Envelope is the wire body for encrypted payloads. Both fields are
// base64-std-encoded. The relay only sees envelopes; it cannot read
// command text or results.
type Envelope struct {
	Nonce      string `json:"nonce"`
	Ciphertext string `json:"ciphertext"`
}

// Command is the cleartext payload sealed into an Envelope by the
// operator and opened by the agent.
type Command struct {
	Kind        string `json:"kind"`                   // exec | push | pull | fetch_transfer | produce_transfer
	Cmd         string `json:"cmd,omitempty"`          // exec
	Shell       string `json:"shell,omitempty"`        // exec
	TimeoutSecs int    `json:"timeout_secs,omitempty"` // exec
	Cwd         string `json:"cwd,omitempty"`          // exec
	RemotePath  string `json:"remote_path,omitempty"`  // push, pull, *transfer
	DataB64     string `json:"data_b64,omitempty"`     // push (single-shot, legacy)
	// Transfer-mode fields. Used when Kind is fetch_transfer or
	// produce_transfer. The agent uses the transfer-related URLs on
	// the relay (/v1/transfers/{id}/...) to move bytes; the relay
	// holds the ciphertext between the two parties.
	TransferID  string `json:"transfer_id,omitempty"`  // fetch_transfer, produce_transfer
	TotalChunks int    `json:"total_chunks,omitempty"` // fetch_transfer
	ChunkSize   int64  `json:"chunk_size,omitempty"`   // fetch_transfer, produce_transfer
	TotalSize   int64  `json:"total_size,omitempty"`   // fetch_transfer
	SHA256Hex   string `json:"sha256_hex,omitempty"`   // fetch_transfer
	Compression string `json:"compression,omitempty"`  // fetch_transfer, produce_transfer
}

// Result is the cleartext payload sealed by the agent and opened by
// the operator. Fields are populated based on Command.Kind.
type Result struct {
	// exec
	ExitCode   int    `json:"exit_code"`
	Stdout     string `json:"stdout,omitempty"`
	Stderr     string `json:"stderr,omitempty"`
	DurationMs int64  `json:"duration_ms,omitempty"`
	Truncated  bool   `json:"truncated,omitempty"`
	// push
	BytesWritten int64 `json:"bytes_written,omitempty"`
	// pull
	DataB64 string `json:"data_b64,omitempty"`
	Size    int64  `json:"size,omitempty"`
	// common
	Ok    bool   `json:"ok,omitempty"`
	Error string `json:"error,omitempty"`
}

// SubmitCommandResponse is what the relay returns when the operator
// POSTs a new command.
type SubmitCommandResponse struct {
	CommandID string `json:"command_id"`
}

// PollResponse is what the relay returns from /poll when a command
// is waiting.
type PollResponse struct {
	CommandID string   `json:"command_id"`
	Envelope  Envelope `json:"envelope"`
}

// ResultResponse is what the relay returns from /commands/{cid}/result
// when the result has arrived.
type ResultResponse struct {
	Status   string   `json:"status"` // "done"
	Envelope Envelope `json:"envelope"`
}

// ListAgentsResponse is what the relay returns from GET /v1/agents.
// Names only — no metadata, no keys. Used by 'rcmd list-agents' so an
// operator can see which agents have been seen by the relay.
type ListAgentsResponse struct {
	Agents []string `json:"agents"`
}

// ---- Transfers ----
//
// All transfer requests are signed exactly like every other rcmd
// request: HMAC headers + the usual replay protection. Chunk bodies
// are raw ciphertext (AES-GCM with per-chunk nonce + AAD = chunk
// index, key derived from master via HKDF). The relay never decrypts.

const (
	// Kinds of commands enqueued by the operator (via the existing
	// queue) to drive transfer flows on the agent side.
	KindFetchTransfer   = "fetch_transfer"   // agent: download a "push" transfer + write file
	KindProduceTransfer = "produce_transfer" // agent: read a file + upload as "pull" transfer
)

// CreateTransferRequest is the operator → relay body that opens a new
// transfer. Sent in cleartext (not in an Envelope) because the relay
// needs to read the fields for storage routing. Sensitive contents
// (the actual file bytes) are never in this struct.
type CreateTransferRequest struct {
	Direction   string `json:"direction"`   // "push" | "pull"
	AgentID     string `json:"agent_id"`    // target agent
	RemotePath  string `json:"remote_path"` // path on the agent
	TotalSize   int64  `json:"total_size"`  // plaintext bytes
	ChunkSize   int64  `json:"chunk_size"`
	TotalChunks int    `json:"total_chunks"`
	SHA256Hex   string `json:"sha256_hex"`   // sha256 of plaintext (post-compression-if-any)
	Compression string `json:"compression"`  // "zstd" | "none"
}

// CreateTransferResponse carries the relay's allocated transfer ID.
type CreateTransferResponse struct {
	TransferID string `json:"transfer_id"`
}

// TransferStatusResponse returns the live manifest. ReceivedBitmap
// tells the uploader/downloader exactly which chunk indices are
// present, supporting resume.
type TransferStatusResponse struct {
	ID             string `json:"id"`
	Direction      string `json:"direction"`
	State          string `json:"state"`
	TotalChunks    int    `json:"total_chunks"`
	ReceivedBitmap []bool `json:"received_bitmap"`
	RemotePath     string `json:"remote_path"`
	Compression    string `json:"compression"`
	SHA256Hex      string `json:"sha256_hex"`
	TotalSize      int64  `json:"total_size"`
	ChunkSize      int64  `json:"chunk_size"`
}

// FetchTransferCmd is the inner command body for KindFetchTransfer.
// Sealed inside the existing Envelope so the agent decrypts it like
// any other command.
type FetchTransferCmd struct {
	TransferID  string `json:"transfer_id"`
	RemotePath  string `json:"remote_path"`
	TotalChunks int    `json:"total_chunks"`
	ChunkSize   int64  `json:"chunk_size"`
	TotalSize   int64  `json:"total_size"`
	SHA256Hex   string `json:"sha256_hex"`
	Compression string `json:"compression"`
}

// ProduceTransferCmd is the inner command body for KindProduceTransfer.
type ProduceTransferCmd struct {
	TransferID  string `json:"transfer_id"`
	RemotePath  string `json:"remote_path"`
	ChunkSize   int64  `json:"chunk_size"`
	Compression string `json:"compression"`
}
