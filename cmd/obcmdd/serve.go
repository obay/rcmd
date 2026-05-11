package main

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/obay/obcmd/internal/auth"
	"github.com/obay/obcmd/internal/crypto"
	"github.com/obay/obcmd/internal/queue"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"golang.org/x/crypto/acme"
	"golang.org/x/crypto/acme/autocert"
)

func newServeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Run the relay HTTP server",
		Long: strings.TrimSpace(`
serve starts the relay on the configured listen address.

By default it serves HTTPS on :443 using Let's Encrypt (autocert) for
the configured domain. Cert provisioning uses TLS-ALPN-01 on the same
port, so port 80 is not required.
Use --insecure for plain HTTP on a custom port (testing only).

Required config keys:
  domain         — fully qualified hostname (e.g. relay.example.com)
  agent_key      — base64 32-byte HMAC key for agents
  operator_key   — base64 32-byte HMAC key for operators

See 'obcmdd keygen --help' to generate keys.
`),
		RunE: runServe,
	}
}

func runServe(cmd *cobra.Command, args []string) error {
	if err := initConfig(); err != nil {
		return err
	}

	agentKeyB64 := viper.GetString("agent_key")
	operatorKeyB64 := viper.GetString("operator_key")
	if agentKeyB64 == "" || operatorKeyB64 == "" {
		return errors.New("config: agent_key and operator_key are required")
	}
	agentKey, err := crypto.ParseKey(agentKeyB64)
	if err != nil {
		return fmt.Errorf("agent_key: %w", err)
	}
	operatorKey, err := crypto.ParseKey(operatorKeyB64)
	if err != nil {
		return fmt.Errorf("operator_key: %w", err)
	}

	insecure := viper.GetBool("insecure")
	domain := viper.GetString("domain")
	if !insecure && domain == "" {
		return errors.New("config: domain is required (or set insecure=true for testing)")
	}

	srv := &server{
		store: queue.New(),
		keys: map[string][]byte{
			"agent":    agentKey,
			"operator": operatorKey,
		},
		nonces:   auth.NewNonceCache(),
		agentIDs: stringSet(viper.GetStringSlice("agent_ids")),
		log:      log.New(os.Stdout, "obcmdd ", log.LstdFlags|log.Lmsgprefix),
	}

	mux := http.NewServeMux()
	srv.routes(mux)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if insecure {
		addr := viper.GetString("insecure_addr")
		httpSrv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 10 * time.Second}
		srv.log.Printf("listening (insecure) on %s", addr)
		go func() {
			<-ctx.Done()
			shutdown(httpSrv, srv.log)
		}()
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	}

	mgr := &autocert.Manager{
		Prompt:     autocert.AcceptTOS,
		Cache:      autocert.DirCache(viper.GetString("acme_cache_dir")),
		HostPolicy: autocert.HostWhitelist(domain),
		Email:      viper.GetString("acme_email"),
	}
	tlsCfg := mgr.TLSConfig()
	tlsCfg.MinVersion = tls.VersionTLS12
	if !containsString(tlsCfg.NextProtos, acme.ALPNProto) {
		tlsCfg.NextProtos = append(tlsCfg.NextProtos, acme.ALPNProto)
	}
	httpsSrv := &http.Server{
		Addr:              viper.GetString("listen_addr"),
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		TLSConfig:         tlsCfg,
	}
	srv.log.Printf("listening on %s (https for %s, ACME TLS-ALPN-01)", httpsSrv.Addr, domain)

	go func() {
		<-ctx.Done()
		shutdown(httpsSrv, srv.log)
	}()
	if err := httpsSrv.ListenAndServeTLS("", ""); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func containsString(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

func shutdown(s *http.Server, l *log.Logger) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.Shutdown(ctx); err != nil {
		l.Printf("shutdown error: %v", err)
	}
}

func stringSet(xs []string) map[string]bool {
	m := make(map[string]bool, len(xs))
	for _, x := range xs {
		m[x] = true
	}
	return m
}
