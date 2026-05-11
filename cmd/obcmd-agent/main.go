// Command obcmd-agent is the obcmd remote-execution agent.
//
// It runs on the target Windows host and polls the relay over HTTPS
// for encrypted commands.
// When a command arrives it executes locally, captures output, and
// posts an encrypted result back. The agent only ever makes outbound
// HTTPS connections — never accepts inbound — so it works through
// aggressive deny-by-default firewalls that block SSH and inspect TLS.
//
// On Windows it can also run as a service (see 'obcmd-agent install').
// On macOS / Linux it runs as a foreground process useful for testing.
package main

import (
	"fmt"
	"os"
)

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
