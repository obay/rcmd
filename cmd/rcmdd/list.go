package main

import (
	"fmt"
	"os"
	"sort"
	"text/tabwriter"
	"time"

	"github.com/obay/rcmd/internal/state"
	"github.com/spf13/cobra"
)

func newListCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:          "list",
		Short:        "Show seen agents and operators",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := state.LoadRelay(statePath)
			if err != nil {
				return err
			}
			if asJSON {
				return emitJSON(map[string]any{
					"kind":      "list",
					"agents":    s.Agents,
					"operators": s.Operators,
				})
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "AGENT\tFIRST SEEN\tLAST SEEN")
			if len(s.Agents) == 0 {
				fmt.Fprintln(w, "(none)\t\t")
			} else {
				for _, name := range sortedKeys(s.Agents) {
					id := s.Agents[name]
					fmt.Fprintf(w, "%s\t%s\t%s\n", name, fmtTime(id.FirstSeen), fmtTime(id.LastSeen))
				}
			}
			fmt.Fprintln(w)
			fmt.Fprintln(w, "OPERATOR\tFIRST SEEN\tLAST SEEN")
			if len(s.Operators) == 0 {
				fmt.Fprintln(w, "(none)\t\t")
			} else {
				for _, name := range sortedKeys(s.Operators) {
					id := s.Operators[name]
					fmt.Fprintf(w, "%s\t%s\t%s\n", name, fmtTime(id.FirstSeen), fmtTime(id.LastSeen))
				}
			}
			return w.Flush()
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit a single JSON object")
	return cmd
}

func sortedKeys(m map[string]state.Identity) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func fmtTime(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.Format(time.RFC3339)
}
