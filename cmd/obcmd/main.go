// Command obcmd is the obcmd operator CLI.
//
// It talks to the relay server (obcmdd) over HTTPS to ship encrypted
// commands and file transfers to a remote obcmd-agent (Windows host)
// and to fetch results back.
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
