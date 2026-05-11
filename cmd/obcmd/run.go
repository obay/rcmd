package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/obay/obcmd/internal/api"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func newRunCmd() *cobra.Command {
	var (
		shell    string
		timeout  int
		cwd      string
		asJSON   bool
	)
	cmd := &cobra.Command{
		Use:   "run [flags] COMMAND...",
		Short: "Run a command on the remote agent and print its output",
		Long: strings.TrimSpace(`
DESCRIPTION
  run sends COMMAND to the remote agent, waits for it to finish, prints
  stdout and stderr, and exits with the agent-side exit code.

EXAMPLES
  # Basic command, default shell from config (cmd.exe on the Dell):
  obcmd run "ipconfig /all"

  # Pick the shell per-call:
  obcmd run --shell powershell "Get-Process | Sort CPU -desc | Select -First 5"

  # Long-running command with a custom timeout (seconds):
  obcmd run --timeout 300 -- ipconfig /all

  # Working directory on the agent:
  obcmd run --cwd 'C:\Users\Public' "dir"

  # Machine-readable output for scripts and AI agents:
  obcmd run --json "hostname"
  # -> {"kind":"exec_result","exit_code":0,"stdout":"DELL-LAPTOP\r\n",
  #     "stderr":"","duration_ms":42,"truncated":false}

OUTPUT
  Text mode  (default): stdout to stdout, stderr to stderr; process
                        exits with the agent-side exit code.
  JSON mode  (--json):  single JSON object to stdout, no stream split.
                        Process exits 0 if the round-trip succeeded
                        (read exit_code from the JSON to learn the
                        agent-side status); exits 1 only on transport
                        or config errors.

EXIT CODES (text mode)
  0..255  agent-side exit code
  124     command timed out on the agent
  1       transport/config error (CLI side)

NOTES
  Use '--' to stop flag parsing if your command itself starts with '-'.
  The end-to-end payload is AES-256-GCM encrypted; the relay never
  sees command text or output in cleartext.
`),
		Args:         cobra.MinimumNArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cl, err := newClient()
			if err != nil {
				return err
			}
			if shell == "" {
				shell = viper.GetString("default_shell")
			}
			if timeout <= 0 {
				timeout = viper.GetInt("default_timeout_secs")
			}
			payload := api.Command{
				Kind:        api.KindExec,
				Cmd:         strings.Join(args, " "),
				Shell:       shell,
				TimeoutSecs: timeout,
				Cwd:         cwd,
			}
			res, err := cl.Run(payload)
			if err != nil {
				return err
			}
			if asJSON {
				return emitJSON(map[string]any{
					"kind":        "exec_result",
					"agent_id":    cl.agentID,
					"shell":       shell,
					"cmd":         payload.Cmd,
					"exit_code":   res.ExitCode,
					"stdout":      res.Stdout,
					"stderr":      res.Stderr,
					"duration_ms": res.DurationMs,
					"truncated":   res.Truncated,
					"error":       res.Error,
				})
			}
			if res.Stdout != "" {
				fmt.Fprint(os.Stdout, res.Stdout)
				if !strings.HasSuffix(res.Stdout, "\n") {
					fmt.Fprintln(os.Stdout)
				}
			}
			if res.Stderr != "" {
				fmt.Fprint(os.Stderr, res.Stderr)
				if !strings.HasSuffix(res.Stderr, "\n") {
					fmt.Fprintln(os.Stderr)
				}
			}
			if res.Truncated {
				fmt.Fprintln(os.Stderr, "(output truncated to "+humanBytes(api.MaxOutputBytes)+")")
			}
			if res.Error != "" {
				fmt.Fprintln(os.Stderr, "agent error:", res.Error)
			}
			os.Exit(res.ExitCode)
			return nil
		},
	}
	cmd.Flags().StringVar(&shell, "shell", "", "shell to use on the agent (cmd | powershell)")
	cmd.Flags().IntVarP(&timeout, "timeout", "t", 0, "kill the command after this many seconds")
	cmd.Flags().StringVar(&cwd, "cwd", "", "working directory on the agent")
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit a single JSON object instead of streaming text")
	return cmd
}

func humanBytes(n int) string {
	const k = 1024
	if n < k {
		return fmt.Sprintf("%dB", n)
	}
	if n < k*k {
		return fmt.Sprintf("%dKiB", n/k)
	}
	return fmt.Sprintf("%dMiB", n/(k*k))
}

// emitJSON writes v as indented JSON to stdout. Used by all --json paths.
func emitJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
