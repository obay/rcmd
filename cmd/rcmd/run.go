package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/obay/rcmd/internal/api"
	"github.com/spf13/cobra"
)

func newRunCmd() *cobra.Command {
	var (
		agentFlag string
		shell     string
		timeout   int
		cwd       string
		asJSON    bool
	)
	cmd := &cobra.Command{
		Use:   "run [flags] COMMAND...",
		Short: "Run a command on a remote agent and print its output",
		Long: strings.TrimSpace(`
DESCRIPTION
  run sends COMMAND to a remote agent, waits for it to finish, prints
  stdout and stderr, and exits with the agent-side exit code.

EXAMPLES
  rcmd run "hostname"                                # default agent
  rcmd run --agent win-host-1 "ipconfig /all"
  rcmd run --shell powershell "Get-Process | Sort CPU -desc | Select -First 5"
  rcmd run --timeout 300 -- ipconfig /all
  rcmd run --cwd 'C:\Users\Public' "dir"
  rcmd run --json "hostname"

EXIT CODES (text mode)
  0..255  agent-side exit code
  124     command timed out on the agent
  1       transport/config error (CLI side)
`),
		Args:         cobra.MinimumNArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newClient()
			if err != nil {
				return err
			}
			agentID, err := c.resolveAgent(agentFlag)
			if err != nil {
				return err
			}
			if shell == "" {
				shell = c.state.DefaultShell
			}
			if timeout <= 0 {
				timeout = c.state.DefaultTimeoutSecs
			}
			payload := api.Command{
				Kind:        api.KindExec,
				Cmd:         strings.Join(args, " "),
				Shell:       shell,
				TimeoutSecs: timeout,
				Cwd:         cwd,
			}
			res, err := c.Run(agentID, payload)
			if err != nil {
				return err
			}
			if asJSON {
				return emitJSON(map[string]any{
					"kind":        "exec_result",
					"agent_id":    agentID,
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
	cmd.Flags().StringVar(&agentFlag, "agent", "", "agent ID (default: from state)")
	cmd.Flags().StringVar(&shell, "shell", "", "shell on the agent (cmd | powershell)")
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
