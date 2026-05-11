// Command rcmd is the rcmd operator CLI.
//
// It talks to the relay server (rcmdd) over HTTPS to ship encrypted
// commands and file transfers to a remote rcmd-agent (Windows host)
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
