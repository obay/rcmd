package main

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/obay/rcmd/internal/api"
	"github.com/obay/rcmd/internal/auth"
	"github.com/obay/rcmd/internal/queue"
	"github.com/obay/rcmd/internal/state"
	"github.com/obay/rcmd/internal/transfer"
)

type server struct {
	state     *state.RelayState
	store     *queue.Store
	transfers *transfer.Store
	hmacKey   []byte
	nonces    *auth.NonceCache
	log       *log.Logger

	dirtyMu *sync.Mutex
	dirty   bool
	seenMu  sync.Mutex // guards state.Agents / state.Operators
}

func (s *server) routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /healthz", s.healthz)
	// Operator-initiated:
	mux.HandleFunc("GET /v1/agents", s.auth(s.recordOperator(s.listAgents)))
	mux.HandleFunc("POST /v1/agents/{id}/commands", s.auth(s.recordOperator(s.submitCommand)))
	mux.HandleFunc("GET /v1/agents/{id}/commands/{cid}/result", s.auth(s.recordOperator(s.getResult)))
	// Agent-initiated:
	mux.HandleFunc("GET /v1/agents/{id}/poll", s.auth(s.recordAgent(s.poll)))
	mux.HandleFunc("POST /v1/agents/{id}/results/{cid}", s.auth(s.recordAgent(s.postResult)))
	// Transfers (used by both operator and agent depending on direction):
	s.transferRoutes(mux)
}

func (s *server) listAgents(w http.ResponseWriter, r *http.Request) {
	s.seenMu.Lock()
	out := make([]string, 0, len(s.state.Agents))
	for name := range s.state.Agents {
		out = append(out, name)
	}
	s.seenMu.Unlock()
	sortStrings(out)
	writeJSON(w, http.StatusOK, api.ListAgentsResponse{Agents: out})
}

func sortStrings(xs []string) {
	for i := 1; i < len(xs); i++ {
		for j := i; j > 0 && xs[j-1] > xs[j]; j-- {
			xs[j-1], xs[j] = xs[j], xs[j-1]
		}
	}
}

func (s *server) healthz(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, "ok\n")
}

// auth wraps a handler with HMAC verification. On success, stashes the
// request body bytes and the self-declared identity in the context so
// the inner handler can read them.
func (s *server) auth(h func(w http.ResponseWriter, r *http.Request, identity string)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, api.MaxFileBytes+1024*1024)
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "body too large or unreadable", http.StatusRequestEntityTooLarge)
			return
		}
		identity, err := auth.Verify(r, body, s.hmacKey, s.nonces)
		if err != nil {
			s.log.Printf("auth reject %s %s: %v", r.Method, r.URL.Path, err)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		ctx := withBody(r.Context(), body)
		h(w, r.WithContext(ctx), identity)
	}
}

// recordOperator notes that the named operator showed up. Pure
// observability; does not affect routing.
func (s *server) recordOperator(h func(w http.ResponseWriter, r *http.Request)) func(http.ResponseWriter, *http.Request, string) {
	return func(w http.ResponseWriter, r *http.Request, identity string) {
		s.touch(s.state.Operators, identity)
		h(w, r)
	}
}

// recordAgent notes that the named agent showed up.
func (s *server) recordAgent(h func(w http.ResponseWriter, r *http.Request)) func(http.ResponseWriter, *http.Request, string) {
	return func(w http.ResponseWriter, r *http.Request, identity string) {
		s.touch(s.state.Agents, identity)
		h(w, r)
	}
}

func (s *server) touch(m map[string]state.Identity, name string) {
	s.seenMu.Lock()
	defer s.seenMu.Unlock()
	id, ok := m[name]
	now := time.Now().UTC()
	if !ok {
		id.FirstSeen = now
	}
	id.LastSeen = now
	m[name] = id
	s.dirtyMu.Lock()
	s.dirty = true
	s.dirtyMu.Unlock()
}

func (s *server) submitCommand(w http.ResponseWriter, r *http.Request) {
	body := bodyFrom(r.Context())
	var env api.Envelope
	if err := json.Unmarshal(body, &env); err != nil {
		http.Error(w, "bad envelope", http.StatusBadRequest)
		return
	}
	cid := s.store.Submit(r.PathValue("id"), env)
	s.log.Printf("submit agent=%s command=%s", r.PathValue("id"), cid)
	writeJSON(w, http.StatusOK, api.SubmitCommandResponse{CommandID: cid})
}

func (s *server) getResult(w http.ResponseWriter, r *http.Request) {
	env, ok := s.store.WaitResult(r.PathValue("id"), r.PathValue("cid"),
		time.Duration(api.ResultTimeoutSeconds)*time.Second)
	if !ok {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	writeJSON(w, http.StatusOK, api.ResultResponse{Status: "done", Envelope: env})
}

func (s *server) poll(w http.ResponseWriter, r *http.Request) {
	cid, env, ok := s.store.Poll(r.PathValue("id"),
		time.Duration(api.PollTimeoutSeconds)*time.Second)
	if !ok {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	writeJSON(w, http.StatusOK, api.PollResponse{CommandID: cid, Envelope: env})
}

func (s *server) postResult(w http.ResponseWriter, r *http.Request) {
	body := bodyFrom(r.Context())
	var env api.Envelope
	if err := json.Unmarshal(body, &env); err != nil {
		http.Error(w, "bad envelope", http.StatusBadRequest)
		return
	}
	if !s.store.CompleteResult(r.PathValue("id"), r.PathValue("cid"), env) {
		http.Error(w, "unknown command", http.StatusNotFound)
		return
	}
	s.log.Printf("result agent=%s command=%s", r.PathValue("id"), r.PathValue("cid"))
	w.WriteHeader(http.StatusNoContent)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
