// Package state defines the JSON state files owned by each rcmd binary.
//
// Users never write these files by hand; the binaries' `init` and `join`
// subcommands create them, the `serve` / `run` / operator commands read
// them, and day-2 ops (rekey, forget, ...) modify them. Files are atomic
// on Linux/macOS (write-tmp-then-rename) and on Windows (same-volume
// rename). Permissions are 0600 where the filesystem honors POSIX modes.
package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// SchemaVersion is bumped whenever the on-disk JSON format changes
// incompatibly. Each binary's Load() refuses files with a newer schema
// than it knows about.
const SchemaVersion = 1

// RelayState is what's persisted at /etc/rcmd/rcmdd.json.
type RelayState struct {
	SchemaVersion int                 `json:"schema_version"`
	Domain        string              `json:"domain,omitempty"`
	TLSMode       string              `json:"tls_mode"` // "autocert" | "insecure"
	ListenAddr    string              `json:"listen_addr"`
	InsecureAddr  string              `json:"insecure_addr,omitempty"`
	ACMECacheDir  string              `json:"acme_cache_dir,omitempty"`
	ACMEEmail     string              `json:"acme_email,omitempty"`
	MasterSecret  string              `json:"master_secret"`         // base64 32-byte
	Agents        map[string]Identity `json:"agents,omitempty"`      // self-declared agent IDs the relay has seen
	Operators     map[string]Identity `json:"operators,omitempty"`   // self-declared operator IDs the relay has seen
}

// Identity is the per-party observational record. Pure metadata: there
// is no per-party key in v2 — everyone shares the master secret.
type Identity struct {
	FirstSeen time.Time `json:"first_seen,omitempty"`
	LastSeen  time.Time `json:"last_seen,omitempty"`
}

// AgentState is what's persisted at %PROGRAMDATA%\rcmd\rcmd-agent.json.
type AgentState struct {
	SchemaVersion int    `json:"schema_version"`
	RelayURL      string `json:"relay_url"`
	AgentID       string `json:"agent_id"`
	MasterSecret  string `json:"master_secret"` // base64 32-byte
	DefaultShell  string `json:"default_shell"`
	LogFile       string `json:"log_file,omitempty"`
}

// OperatorState is what's persisted at ~/.config/rcmd/rcmd.json on Unix
// or %APPDATA%\rcmd\rcmd.json on Windows.
type OperatorState struct {
	SchemaVersion      int    `json:"schema_version"`
	RelayURL           string `json:"relay_url"`
	OperatorID         string `json:"operator_id"`
	MasterSecret       string `json:"master_secret"`
	DefaultAgent       string `json:"default_agent,omitempty"`
	DefaultShell       string `json:"default_shell"`
	DefaultTimeoutSecs int    `json:"default_timeout_secs"`
}

// LoadRelay reads and parses a RelayState from path.
func LoadRelay(path string) (*RelayState, error) {
	var s RelayState
	if err := loadJSON(path, &s); err != nil {
		return nil, err
	}
	if err := checkSchema(s.SchemaVersion, "relay", path); err != nil {
		return nil, err
	}
	return &s, nil
}

// SaveRelay atomically writes s to path with mode 0600.
func SaveRelay(path string, s *RelayState) error {
	s.SchemaVersion = SchemaVersion
	return saveJSON(path, s)
}

// LoadAgent reads and parses an AgentState from path.
func LoadAgent(path string) (*AgentState, error) {
	var s AgentState
	if err := loadJSON(path, &s); err != nil {
		return nil, err
	}
	if err := checkSchema(s.SchemaVersion, "agent", path); err != nil {
		return nil, err
	}
	return &s, nil
}

// SaveAgent atomically writes s to path with mode 0600.
func SaveAgent(path string, s *AgentState) error {
	s.SchemaVersion = SchemaVersion
	return saveJSON(path, s)
}

// LoadOperator reads and parses an OperatorState from path.
func LoadOperator(path string) (*OperatorState, error) {
	var s OperatorState
	if err := loadJSON(path, &s); err != nil {
		return nil, err
	}
	if err := checkSchema(s.SchemaVersion, "operator", path); err != nil {
		return nil, err
	}
	return &s, nil
}

// SaveOperator atomically writes s to path with mode 0600.
func SaveOperator(path string, s *OperatorState) error {
	s.SchemaVersion = SchemaVersion
	return saveJSON(path, s)
}

// Exists reports whether a state file is present at path.
func Exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func loadJSON(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("state file not found: %s (run init or join first)", path)
		}
		return fmt.Errorf("read %s: %w", path, err)
	}
	if err := json.Unmarshal(b, v); err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	return nil
}

func saveJSON(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create state dir: %w", err)
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create tmp: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return fmt.Errorf("write tmp: %w", err)
	}
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return fmt.Errorf("chmod tmp: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("fsync tmp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close tmp: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("rename %s -> %s: %w", tmpPath, path, err)
	}
	return nil
}

func checkSchema(got int, role, path string) error {
	if got == 0 {
		return fmt.Errorf("%s state %s: missing schema_version (corrupt or pre-v1 file)", role, path)
	}
	if got > SchemaVersion {
		return fmt.Errorf("%s state %s: schema_version=%d newer than this binary supports (%d)", role, path, got, SchemaVersion)
	}
	return nil
}
