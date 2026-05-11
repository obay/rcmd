package main

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/obay/rcmd/internal/auth"
	"github.com/obay/rcmd/internal/crypto"
	"github.com/obay/rcmd/internal/queue"
	"github.com/obay/rcmd/internal/state"
	"github.com/obay/rcmd/internal/transfer"
	"github.com/spf13/cobra"
	"golang.org/x/crypto/acme"
	"golang.org/x/crypto/acme/autocert"
)

func newServeCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "serve",
		Short:        "Run the relay (invoked by systemd)",
		SilenceUsage: true,
		RunE:         runServe,
	}
}

func runServe(cmd *cobra.Command, args []string) error {
	st, err := state.LoadRelay(statePath)
	if err != nil {
		return err
	}
	master, err := base64.StdEncoding.DecodeString(st.MasterSecret)
	if err != nil || len(master) != crypto.MasterSecretBytes {
		return errors.New("state: master_secret is missing or malformed")
	}
	hmacKey := crypto.DeriveHMACSubkey(master)

	if st.Agents == nil {
		st.Agents = map[string]state.Identity{}
	}
	if st.Operators == nil {
		st.Operators = map[string]state.Identity{}
	}

	transferRoot := filepath.Join(filepath.Dir(statePath), "..", "lib", "rcmd", "transfers")
	if v := os.Getenv("RCMDD_TRANSFER_ROOT"); v != "" {
		transferRoot = v
	} else if st.ACMECacheDir != "" {
		// /var/lib/rcmd/autocert is conventional; put transfers next to it.
		transferRoot = filepath.Join(filepath.Dir(st.ACMECacheDir), "transfers")
	}
	transfers, err := transfer.NewStore(transferRoot)
	if err != nil {
		return err
	}

	srv := &server{
		state:     st,
		store:     queue.New(),
		transfers: transfers,
		hmacKey:   hmacKey,
		nonces:    auth.NewNonceCache(),
		log:       log.New(os.Stdout, "rcmdd ", log.LstdFlags|log.Lmsgprefix),
		dirtyMu:   &sync.Mutex{},
	}
	srv.startFlusher()
	srv.startTransferGC()

	mux := http.NewServeMux()
	srv.routes(mux)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if st.TLSMode == "insecure" {
		addr := st.InsecureAddr
		if addr == "" {
			addr = ":8080"
		}
		httpSrv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 10 * time.Second}
		srv.log.Printf("listening (insecure) on %s", addr)
		go func() {
			<-ctx.Done()
			shutdown(httpSrv, srv.log)
			srv.flushNow()
		}()
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	}

	if st.Domain == "" {
		return errors.New("state: domain is required for autocert mode")
	}
	mgr := &autocert.Manager{
		Prompt:     autocert.AcceptTOS,
		Cache:      autocert.DirCache(st.ACMECacheDir),
		HostPolicy: autocert.HostWhitelist(st.Domain),
		Email:      st.ACMEEmail,
	}
	tlsCfg := mgr.TLSConfig()
	tlsCfg.MinVersion = tls.VersionTLS12
	if !containsString(tlsCfg.NextProtos, acme.ALPNProto) {
		tlsCfg.NextProtos = append(tlsCfg.NextProtos, acme.ALPNProto)
	}
	httpsSrv := &http.Server{
		Addr:              st.ListenAddr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		TLSConfig:         tlsCfg,
	}
	srv.log.Printf("listening on %s (https for %s, ACME TLS-ALPN-01)", httpsSrv.Addr, st.Domain)

	go func() {
		<-ctx.Done()
		shutdown(httpsSrv, srv.log)
		srv.flushNow()
	}()
	if err := httpsSrv.ListenAndServeTLS("", ""); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func shutdown(s *http.Server, l *log.Logger) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.Shutdown(ctx); err != nil {
		l.Printf("shutdown error: %v", err)
	}
}

func containsString(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

// startFlusher kicks off a goroutine that periodically writes the
// state file when there are recent observability updates. Crash
// resilience for the seen-list at minimal cost.
func (s *server) startFlusher() {
	t := time.NewTicker(30 * time.Second)
	go func() {
		for range t.C {
			s.flushNow()
		}
	}()
}

func (s *server) flushNow() {
	s.dirtyMu.Lock()
	dirty := s.dirty
	s.dirty = false
	s.dirtyMu.Unlock()
	if !dirty {
		return
	}
	if err := state.SaveRelay(statePath, s.state); err != nil {
		s.log.Printf("flush state error: %v", err)
	}
}
