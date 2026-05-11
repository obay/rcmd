// Package api defines the wire format for obcmd — header names, kinds,
// and the JSON-serializable structs that flow through the relay.
//
// Everything in this package is intentionally simple: it's the boundary
// between the three binaries and must be cheap to keep stable.
package api

const (
	HeaderTimestamp = "X-Obcmd-Timestamp"
	HeaderNonce     = "X-Obcmd-Nonce"
	HeaderSig       = "X-Obcmd-Sig"
	HeaderIdentity  = "X-Obcmd-Identity"

	IdentityAgent    = "agent"
	IdentityOperator = "operator"

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
	Kind        string `json:"kind"`                   // exec | push | pull
	Cmd         string `json:"cmd,omitempty"`          // exec
	Shell       string `json:"shell,omitempty"`        // exec
	TimeoutSecs int    `json:"timeout_secs,omitempty"` // exec
	Cwd         string `json:"cwd,omitempty"`          // exec
	RemotePath  string `json:"remote_path,omitempty"`  // push, pull
	DataB64     string `json:"data_b64,omitempty"`     // push
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
