package main

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/obay/rcmd/internal/api"
	"github.com/obay/rcmd/internal/auth"
	"github.com/obay/rcmd/internal/queue"
)

type server struct {
	store    *queue.Store
	keys     map[string][]byte // identity -> hmac key
	nonces   *auth.NonceCache
	agentIDs map[string]bool // allowed agent IDs
	log      *log.Logger
}

func (s *server) routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /healthz", s.healthz)
	mux.HandleFunc("POST /v1/agents/{id}/commands", s.requireOperator(s.submitCommand))
	mux.HandleFunc("GET /v1/agents/{id}/commands/{cid}/result", s.requireOperator(s.getResult))
	mux.HandleFunc("GET /v1/agents/{id}/poll", s.requireAgent(s.poll))
	mux.HandleFunc("POST /v1/agents/{id}/results/{cid}", s.requireAgent(s.postResult))
}

func (s *server) healthz(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, "ok\n")
}

func (s *server) requireOperator(h http.HandlerFunc) http.HandlerFunc {
	return s.requireIdentity(api.IdentityOperator, h)
}

func (s *server) requireAgent(h http.HandlerFunc) http.HandlerFunc {
	return s.requireIdentity(api.IdentityAgent, h)
}

func (s *server) requireIdentity(want string, h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Read body; bounded by Content-Length and our caps.
		r.Body = http.MaxBytesReader(w, r.Body, api.MaxFileBytes+1024*1024)
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "body too large or unreadable", http.StatusRequestEntityTooLarge)
			return
		}
		identity, err := auth.Verify(r, body, s.keys, s.nonces)
		if err != nil {
			s.log.Printf("auth reject %s %s: %v", r.Method, r.URL.Path, err)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if identity != want {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		agentID := r.PathValue("id")
		if !s.agentIDs[agentID] {
			http.Error(w, "unknown agent", http.StatusNotFound)
			return
		}
		// Stash body for handler.
		ctx := withBody(r.Context(), body)
		h(w, r.WithContext(ctx))
	}
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
		w.WriteHeader(http.StatusAccepted) // 202 still running
		return
	}
	writeJSON(w, http.StatusOK, api.ResultResponse{Status: "done", Envelope: env})
}

func (s *server) poll(w http.ResponseWriter, r *http.Request) {
	cid, env, ok := s.store.Poll(r.PathValue("id"),
		time.Duration(api.PollTimeoutSeconds)*time.Second)
	if !ok {
		w.WriteHeader(http.StatusNoContent) // 204
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
