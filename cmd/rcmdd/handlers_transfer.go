package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/obay/rcmd/internal/api"
	"github.com/obay/rcmd/internal/transfer"
)

func (s *server) transferRoutes(mux *http.ServeMux) {
	// POST /v1/transfers can be called by either an operator (push) or
	// an agent (pull). Identity goes in seen-list either way.
	mux.HandleFunc("POST /v1/transfers", s.auth(s.recordParty(s.createTransfer)))
	mux.HandleFunc("PUT /v1/transfers/{id}/chunks/{n}", s.auth(s.recordParty(s.putChunk)))
	mux.HandleFunc("GET /v1/transfers/{id}/chunks/{n}", s.auth(s.recordParty(s.getChunk)))
	mux.HandleFunc("GET /v1/transfers/{id}/status", s.auth(s.recordParty(s.transferStatus)))
	mux.HandleFunc("POST /v1/transfers/{id}/complete", s.auth(s.recordParty(s.completeTransfer)))
	mux.HandleFunc("POST /v1/transfers/{id}/done", s.auth(s.recordParty(s.doneTransfer)))
	mux.HandleFunc("DELETE /v1/transfers/{id}", s.auth(s.recordParty(s.deleteTransfer)))
}

// recordParty wraps a handler that may be invoked by either an
// operator or an agent. The relay still records the identity in the
// seen-list for observability, but it doesn't gate by role.
func (s *server) recordParty(h func(w http.ResponseWriter, r *http.Request)) func(http.ResponseWriter, *http.Request, string) {
	return func(w http.ResponseWriter, r *http.Request, identity string) {
		// Could be either an operator or an agent; touch operators
		// preferentially (a name appearing in both lists is fine).
		s.touch(s.state.Operators, identity)
		h(w, r)
	}
}

func (s *server) createTransfer(w http.ResponseWriter, r *http.Request) {
	body := bodyFrom(r.Context())
	var req api.CreateTransferRequest
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if req.TotalChunks <= 0 || req.ChunkSize <= 0 || req.TotalSize < 0 {
		http.Error(w, "invalid sizes", http.StatusBadRequest)
		return
	}
	if req.AgentID == "" {
		http.Error(w, "agent_id required", http.StatusBadRequest)
		return
	}
	if req.Direction != string(transfer.DirectionPush) && req.Direction != string(transfer.DirectionPull) {
		http.Error(w, "direction must be push or pull", http.StatusBadRequest)
		return
	}
	id := newTransferID()
	m, err := s.transfers.Create(transfer.Manifest{
		ID:          id,
		Direction:   transfer.Direction(req.Direction),
		To:          req.AgentID,
		RemotePath:  req.RemotePath,
		TotalSize:   req.TotalSize,
		ChunkSize:   req.ChunkSize,
		TotalChunks: req.TotalChunks,
		SHA256Hex:   req.SHA256Hex,
		Compression: req.Compression,
	})
	if err != nil {
		s.log.Printf("create transfer: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.log.Printf("transfer %s created (%s, agent=%s, %d chunks)", m.ID, m.Direction, m.To, m.TotalChunks)
	writeJSON(w, http.StatusOK, api.CreateTransferResponse{TransferID: m.ID})
}

func (s *server) putChunk(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	n, err := strconv.Atoi(r.PathValue("n"))
	if err != nil {
		http.Error(w, "bad chunk index", http.StatusBadRequest)
		return
	}
	written, err := s.transfers.PutChunk(id, n, r.Body, transfer.MaxChunkBytes)
	if err != nil {
		if errors.Is(err, transfer.ErrNotFound) {
			http.Error(w, "transfer not found", http.StatusNotFound)
			return
		}
		s.log.Printf("put chunk %s/%d: %v", id, n, err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("X-Rcmd-Bytes", strconv.FormatInt(written, 10))
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) getChunk(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	n, err := strconv.Atoi(r.PathValue("n"))
	if err != nil {
		http.Error(w, "bad chunk index", http.StatusBadRequest)
		return
	}
	f, err := s.transfers.GetChunk(id, n)
	if err != nil {
		if errors.Is(err, transfer.ErrNotFound) {
			http.Error(w, "transfer not found", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", "application/octet-stream")
	if st, err := f.Stat(); err == nil {
		w.Header().Set("Content-Length", strconv.FormatInt(st.Size(), 10))
	}
	w.WriteHeader(http.StatusOK)
	if _, err := io.Copy(w, f); err != nil {
		s.log.Printf("get chunk %s/%d: copy: %v", id, n, err)
	}
}

func (s *server) transferStatus(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	m, err := s.transfers.Load(id)
	if err != nil {
		if errors.Is(err, transfer.ErrNotFound) {
			http.Error(w, "transfer not found", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, api.TransferStatusResponse{
		ID:             m.ID,
		Direction:      string(m.Direction),
		State:          string(m.State),
		TotalChunks:    m.TotalChunks,
		ReceivedBitmap: m.ReceivedBitmap,
		RemotePath:     m.RemotePath,
		Compression:    m.Compression,
		SHA256Hex:      m.SHA256Hex,
		TotalSize:      m.TotalSize,
		ChunkSize:      m.ChunkSize,
	})
}

func (s *server) completeTransfer(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.transfers.MarkReady(id); err != nil {
		if errors.Is(err, transfer.ErrNotFound) {
			http.Error(w, "transfer not found", http.StatusNotFound)
			return
		}
		s.log.Printf("complete %s: %v", id, err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.log.Printf("transfer %s ready", id)
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) doneTransfer(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.transfers.MarkComplete(id); err != nil {
		if errors.Is(err, transfer.ErrNotFound) {
			http.Error(w, "transfer not found", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.log.Printf("transfer %s complete", id)
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) deleteTransfer(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.transfers.Delete(id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// startTransferGC sweeps idle transfers off disk on a 1-minute cadence.
func (s *server) startTransferGC() {
	t := time.NewTicker(1 * time.Minute)
	go func() {
		for range t.C {
			if n, err := s.transfers.GC(time.Now(), transfer.IdleTTL); err != nil {
				s.log.Printf("transfer gc: %v", err)
			} else if n > 0 {
				s.log.Printf("transfer gc: removed %d idle transfers", n)
			}
		}
	}()
}

func newTransferID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
