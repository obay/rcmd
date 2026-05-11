// Package queue holds the relay's in-memory command queue and result
// store. Each agent has a queue of pending commands and a map of
// command-id → result slot.
//
// Both operations support long-polling via channels: a waiter blocks
// on a channel and gets signaled the moment something is available.
package queue

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"

	"github.com/obay/obcmd/internal/api"
)

const (
	// PendingTTL is how long a queued (un-delivered) command lingers
	// before being garbage-collected. Operators see "no agent" in
	// practice via the result long-poll timing out.
	PendingTTL = 5 * time.Minute

	// ResultTTL is how long a delivered result stays available for
	// the operator to fetch after the agent submits it.
	ResultTTL = 10 * time.Minute
)

type pending struct {
	commandID string
	envelope  api.Envelope
	enqueued  time.Time
}

type resultSlot struct {
	envelope  api.Envelope
	delivered time.Time
	done      chan struct{} // closed when the result arrives
}

// Store is the relay's per-agent state. Safe for concurrent use.
type Store struct {
	mu      sync.Mutex
	agents  map[string]*agentState
	created time.Time
}

type agentState struct {
	pending  []pending
	results  map[string]*resultSlot // commandID -> slot
	notify   chan struct{}          // closed and replaced on enqueue
	lastSeen time.Time
}

func New() *Store {
	s := &Store{
		agents:  make(map[string]*agentState),
		created: time.Now(),
	}
	go s.gcLoop()
	return s
}

func (s *Store) get(agentID string) *agentState {
	a, ok := s.agents[agentID]
	if !ok {
		a = &agentState{
			results: make(map[string]*resultSlot),
			notify:  make(chan struct{}),
		}
		s.agents[agentID] = a
	}
	return a
}

// Submit enqueues a command for an agent and pre-creates its result slot.
// Returns the freshly minted command ID.
func (s *Store) Submit(agentID string, env api.Envelope) string {
	cid := newID()
	s.mu.Lock()
	defer s.mu.Unlock()
	a := s.get(agentID)
	a.pending = append(a.pending, pending{
		commandID: cid,
		envelope:  env,
		enqueued:  time.Now(),
	})
	a.results[cid] = &resultSlot{done: make(chan struct{})}
	// Wake up any waiting pollers.
	close(a.notify)
	a.notify = make(chan struct{})
	return cid
}

// Poll waits up to timeout for a pending command for agentID. Returns
// (commandID, envelope, true) if one was dequeued, or ("", _, false)
// on timeout.
func (s *Store) Poll(agentID string, timeout time.Duration) (string, api.Envelope, bool) {
	deadline := time.Now().Add(timeout)
	for {
		s.mu.Lock()
		a := s.get(agentID)
		a.lastSeen = time.Now()
		if len(a.pending) > 0 {
			p := a.pending[0]
			a.pending = a.pending[1:]
			s.mu.Unlock()
			return p.commandID, p.envelope, true
		}
		wait := a.notify
		s.mu.Unlock()
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return "", api.Envelope{}, false
		}
		select {
		case <-wait:
			// New command — loop and try to dequeue.
		case <-time.After(remaining):
			return "", api.Envelope{}, false
		}
	}
}

// CompleteResult records the result envelope for a command and wakes
// up any operator waiting on it.
func (s *Store) CompleteResult(agentID, commandID string, env api.Envelope) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	a := s.get(agentID)
	slot, ok := a.results[commandID]
	if !ok {
		return false
	}
	slot.envelope = env
	slot.delivered = time.Now()
	close(slot.done)
	return true
}

// WaitResult blocks up to timeout for the result of commandID. Returns
// (envelope, true) on success, or (_, false) on timeout or unknown id.
func (s *Store) WaitResult(agentID, commandID string, timeout time.Duration) (api.Envelope, bool) {
	s.mu.Lock()
	a := s.get(agentID)
	slot, ok := a.results[commandID]
	s.mu.Unlock()
	if !ok {
		return api.Envelope{}, false
	}
	select {
	case <-slot.done:
		s.mu.Lock()
		env := slot.envelope
		s.mu.Unlock()
		return env, true
	case <-time.After(timeout):
		// Was it delivered just-now while we were timing out?
		s.mu.Lock()
		defer s.mu.Unlock()
		select {
		case <-slot.done:
			return slot.envelope, true
		default:
			return api.Envelope{}, false
		}
	}
}

func (s *Store) gcLoop() {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for range t.C {
		s.gc()
	}
}

func (s *Store) gc() {
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, a := range s.agents {
		// Drop stale pending commands.
		fresh := a.pending[:0]
		for _, p := range a.pending {
			if now.Sub(p.enqueued) < PendingTTL {
				fresh = append(fresh, p)
			}
		}
		a.pending = fresh
		// Drop stale delivered results.
		for cid, slot := range a.results {
			if !slot.delivered.IsZero() && now.Sub(slot.delivered) > ResultTTL {
				delete(a.results, cid)
			}
		}
	}
}

func newID() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
