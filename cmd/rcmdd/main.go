// Command rcmdd is the rcmd relay server.
//
// It runs on a Linux host at a domain you own and brokers encrypted
// commands between the operator's CLI and the Windows agent. The relay
// never sees plaintext command text or output — it only relays
// AES-256-GCM envelopes between authenticated parties.
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
