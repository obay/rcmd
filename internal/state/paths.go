package state

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
)

// DefaultRelayPath returns the conventional state-file path for the
// relay daemon. Always /etc/rcmd/rcmdd.json on Linux (the only platform
// the relay ships for). Same path is used on macOS for dev.
func DefaultRelayPath() string {
	return "/etc/rcmd/rcmdd.json"
}

// DefaultAgentPath returns the conventional state-file path for the
// Windows agent. On Windows this is %PROGRAMDATA%\rcmd\rcmd-agent.json
// (typically C:\ProgramData\rcmd\rcmd-agent.json). On non-Windows
// (development builds only — the agent never ships for those) it falls
// back to ~/.config/rcmd/rcmd-agent.json.
func DefaultAgentPath() (string, error) {
	if runtime.GOOS == "windows" {
		pd := os.Getenv("PROGRAMDATA")
		if pd == "" {
			pd = `C:\ProgramData`
		}
		return filepath.Join(pd, "rcmd", "rcmd-agent.json"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "rcmd", "rcmd-agent.json"), nil
}

// DefaultOperatorPath returns the conventional state-file path for the
// operator CLI: $XDG_CONFIG_HOME/rcmd/rcmd.json (or ~/.config/rcmd/rcmd.json)
// on Unix; %APPDATA%\rcmd\rcmd.json on Windows.
func DefaultOperatorPath() (string, error) {
	if runtime.GOOS == "windows" {
		appdata := os.Getenv("APPDATA")
		if appdata == "" {
			return "", errors.New("APPDATA not set")
		}
		return filepath.Join(appdata, "rcmd", "rcmd.json"), nil
	}
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "rcmd", "rcmd.json"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "rcmd", "rcmd.json"), nil
}
