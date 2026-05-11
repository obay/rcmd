package main

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"github.com/obay/rcmd/internal/crypto"
	"github.com/obay/rcmd/internal/state"
	"github.com/obay/rcmd/internal/token"
	"github.com/spf13/cobra"
)

func newInitCmd() *cobra.Command {
	var (
		domain       string
		acmeEmail    string
		acmeCacheDir string
		listenAddr   string
		insecure     bool
		insecureAddr string
		publicURL    string
		force        bool
	)
	cmd := &cobra.Command{
		Use:          "init",
		Short:        "First-time relay setup: generate keys, write state, print join token",
		SilenceUsage: true,
		Long: strings.TrimSpace(`
init creates the relay state file at /etc/rcmd/rcmdd.json (or the path
given via --state). It generates a fresh master secret, persists the
relay's domain + TLS settings, and prints the join token that you paste
into 'rcmd-agent join' and 'rcmd join' on the other machines.

Refuses to run if a state file already exists; pass --force to overwrite
(which is destructive — all existing agents and operators will be
locked out until they re-join with the new token).

Examples:
  sudo rcmdd init --domain relay.example.com
  sudo rcmdd init --domain relay.example.com --acme-email you@example.com
  sudo rcmdd init --insecure
`),
		RunE: func(cmd *cobra.Command, args []string) error {
			if insecure && domain != "" {
				return errors.New("--insecure and --domain are mutually exclusive")
			}
			if !insecure && domain == "" {
				return errors.New("--domain is required (or pass --insecure for plain-HTTP mode)")
			}
			if state.Exists(statePath) && !force {
				return fmt.Errorf("state file already exists at %s — use --force to overwrite (destructive)", statePath)
			}

			master, err := crypto.NewMasterSecret()
			if err != nil {
				return fmt.Errorf("generate master secret: %w", err)
			}
			masterB64 := base64.StdEncoding.EncodeToString(master)

			s := &state.RelayState{
				MasterSecret: masterB64,
				PublicURL:    publicURL,
				Agents:       map[string]state.Identity{},
				Operators:    map[string]state.Identity{},
			}
			if insecure {
				s.TLSMode = "insecure"
				s.InsecureAddr = insecureAddr
			} else {
				s.TLSMode = "autocert"
				s.Domain = domain
				s.ListenAddr = listenAddr
				s.ACMECacheDir = acmeCacheDir
				s.ACMEEmail = acmeEmail
			}

			if err := state.SaveRelay(statePath, s); err != nil {
				return fmt.Errorf("save state: %w", err)
			}
			// When init is run as root (typical: `sudo rcmdd init` after
			// installing the .deb), the state file is created with root
			// ownership and mode 0600. The systemd unit runs as user
			// `rcmd`, which would then fail to read it. Hand off
			// ownership now if that user exists.
			handoffStateToService(statePath)

			joinURL := tokenURL(s)
			tok, err := token.Mint(token.Token{RelayURL: joinURL, MasterSecret: masterB64})
			if err != nil {
				return fmt.Errorf("mint token: %w", err)
			}

			fmt.Printf("Wrote %s\n", statePath)
			fmt.Println()
			fmt.Println("Next steps:")
			fmt.Println()
			fmt.Println("  1. Start the relay:")
			fmt.Println()
			fmt.Println("       sudo systemctl enable --now rcmdd")
			fmt.Println()
			fmt.Println("  2. On each Windows agent (elevated PowerShell):")
			fmt.Println()
			fmt.Printf("       rcmd-agent join %s\n", tok)
			fmt.Println()
			fmt.Println("  3. On each operator machine:")
			fmt.Println()
			fmt.Printf("       rcmd join %s\n", tok)
			fmt.Println()
			fmt.Println("The string above is the join token. Anyone who has it can join")
			fmt.Println("the relay; treat it like a secret. Re-print at any time with:")
			fmt.Println("  sudo rcmdd token")
			return nil
		},
	}
	cmd.Flags().StringVar(&domain, "domain", "", "public hostname for autocert (e.g. relay.example.com)")
	cmd.Flags().StringVar(&acmeEmail, "acme-email", "", "contact email passed to Let's Encrypt (optional)")
	cmd.Flags().StringVar(&acmeCacheDir, "acme-cache-dir", "/var/lib/rcmd/autocert", "directory for the Let's Encrypt cert cache")
	cmd.Flags().StringVar(&listenAddr, "listen-addr", ":443", "HTTPS listen address (autocert mode)")
	cmd.Flags().BoolVar(&insecure, "insecure", false, "plain-HTTP mode (no TLS, no domain) — testing / trusted networks only")
	cmd.Flags().StringVar(&insecureAddr, "insecure-addr", ":8080", "plain-HTTP listen address when --insecure is set")
	cmd.Flags().StringVar(&publicURL, "public-url", "", "URL to embed in the join token (overrides the default; useful in --insecure mode where the listen address is not externally reachable)")
	cmd.Flags().BoolVar(&force, "force", false, "overwrite an existing state file (destructive)")
	return cmd
}

// tokenURL builds the URL to embed in the join token. Precedence:
//
//  1. state.PublicURL (explicit --public-url at init time)
//  2. https://<domain> for autocert mode
//  3. http://<insecure_addr> for insecure mode if the addr has a host
//     component (e.g. "127.0.0.1:8080")
//  4. http://<this-host><insecure_addr> placeholder otherwise, which
//     the user must substitute manually
func tokenURL(s *state.RelayState) string {
	if s.PublicURL != "" {
		return s.PublicURL
	}
	if s.TLSMode == "autocert" {
		return "https://" + s.Domain
	}
	addr := s.InsecureAddr
	if addr != "" && !strings.HasPrefix(addr, ":") {
		return "http://" + addr
	}
	return "http://<this-host>" + addr
}
