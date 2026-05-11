// Command obcmdd is the obcmd relay server.
//
// It runs on a small VPS at a domain you own (e.g. ai.obay.cloud) and
// brokers encrypted commands between the operator's Mac CLI and the
// Dell agent. The relay never sees plaintext command text or output —
// it only relays AES-256-GCM envelopes between authenticated parties.
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
