// Package transfer holds the relay-side state for chunked, resumable
// file transfers: an on-disk manifest per transfer and the ciphertext
// chunks themselves. Both the uploader and the downloader use the
// same HTTP endpoints (defined in cmd/rcmdd/handlers_transfer.go) to
// PUT and GET chunks against this storage.
//
// The relay never sees plaintext at the transfer layer — chunks are
// AES-GCM-encrypted by the uploader with a per-transfer key derived
// via HKDF from the master secret, and decrypted only by the
// downloader. (The relay does hold the master secret in v0.2.x, so
// it *could* derive the key and read the chunks; it never does.)
package transfer

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"sync"
	"time"
)

// Direction is the logical flow of bytes through the relay.
type Direction string

const (
	// DirectionPush: operator → relay → agent. Operator uploads, agent downloads.
	DirectionPush Direction = "push"
	// DirectionPull: agent → relay → operator. Agent uploads, operator downloads.
	DirectionPull Direction = "pull"
)

// State enumerates the lifecycle of a transfer record on the relay.
type State string

const (
	StateUploading   State = "uploading"   // uploader still PUTing chunks
	StateReady       State = "ready"       // all chunks present; downloader may pull
	StateDownloading State = "downloading" // at least one chunk fetched
	StateComplete    State = "complete"    // downloader signalled done
	StateFailed      State = "failed"      // checksum mismatch or similar
)

// IdleTTL is how long a transfer with no activity lingers on the
// relay before GC sweeps it. Stale chunks are deleted from disk.
const IdleTTL = 1 * time.Hour

// MaxChunkBytes caps the size of any single uploaded chunk to keep
// memory bounded. Chunks larger than this are rejected at PUT time.
// Default chunk size is 1 MiB; this cap is a safety margin.
const MaxChunkBytes = 8 * 1024 * 1024

// Manifest is the per-transfer record. Stored at
// <root>/<id>/manifest.json; the chunk files live next to it as
// <root>/<id>/<index>.bin.
type Manifest struct {
	ID             string    `json:"id"`
	Direction      Direction `json:"direction"`
	From           string    `json:"from"`           // initiator identity (operator for push, operator-on-behalf-of for pull)
	To             string    `json:"to"`             // target agent_id
	RemotePath     string    `json:"remote_path"`    // path on the agent host
	TotalSize      int64     `json:"total_size"`     // size of plaintext (post-compression-if-any)
	ChunkSize      int64     `json:"chunk_size"`     // bytes of plaintext per chunk
	TotalChunks    int       `json:"total_chunks"`
	SHA256Hex      string    `json:"sha256_hex"`     // sha256 of plaintext stream
	Compression    string    `json:"compression"`    // "zstd" | "none"
	State          State     `json:"state"`
	Created        time.Time `json:"created"`
	LastActivity   time.Time `json:"last_activity"`
	ReceivedBitmap []bool    `json:"received_bitmap"`
}

// Store is the on-disk transfer storage rooted at a directory like
// /var/lib/rcmd/transfers. Safe for concurrent use.
type Store struct {
	root string
	mu   sync.Mutex // guards Manifest writes; chunk files are append-only
}

// NewStore prepares the storage root, creating it if missing.
func NewStore(root string) (*Store, error) {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, fmt.Errorf("create transfer root %s: %w", root, err)
	}
	return &Store{root: root}, nil
}

// Create persists a new manifest, allocates the per-transfer directory,
// and returns the populated manifest. Caller supplies the ID (typically
// a UUID); ReceivedBitmap and timestamps are filled in here.
func (s *Store) Create(m Manifest) (*Manifest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if m.ID == "" {
		return nil, errors.New("transfer ID required")
	}
	if m.TotalChunks <= 0 {
		return nil, errors.New("total_chunks must be > 0")
	}
	dir := s.transferDir(m.ID)
	if _, err := os.Stat(dir); err == nil {
		return nil, fmt.Errorf("transfer %s already exists", m.ID)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create transfer dir: %w", err)
	}
	now := time.Now().UTC()
	m.State = StateUploading
	m.Created = now
	m.LastActivity = now
	m.ReceivedBitmap = make([]bool, m.TotalChunks)
	if err := s.writeManifestLocked(&m); err != nil {
		return nil, err
	}
	return &m, nil
}

// Load reads the manifest for an existing transfer.
func (s *Store) Load(id string) (*Manifest, error) {
	b, err := os.ReadFile(s.manifestPath(id))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("read manifest %s: %w", id, err)
	}
	var m Manifest
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("parse manifest %s: %w", id, err)
	}
	return &m, nil
}

// ErrNotFound indicates the transfer doesn't exist.
var ErrNotFound = errors.New("transfer not found")

// PutChunk writes a ciphertext chunk to disk and marks it in the
// manifest's received bitmap. Idempotent: a re-PUT of an already-
// received chunk replaces the file (useful for retries that succeeded
// twice on the network but appeared to fail).
func (s *Store) PutChunk(id string, index int, body io.Reader, maxBytes int64) (int64, error) {
	if index < 0 {
		return 0, errors.New("negative chunk index")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := s.loadLocked(id)
	if err != nil {
		return 0, err
	}
	if index >= m.TotalChunks {
		return 0, fmt.Errorf("chunk index %d out of range [0,%d)", index, m.TotalChunks)
	}
	if m.State == StateComplete || m.State == StateFailed {
		return 0, fmt.Errorf("transfer is %s; not accepting chunks", m.State)
	}
	tmp := s.chunkPath(id, index) + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return 0, err
	}
	limited := io.LimitReader(body, maxBytes+1)
	n, err := io.Copy(f, limited)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(tmp)
		return n, err
	}
	if n > maxBytes {
		os.Remove(tmp)
		return n, fmt.Errorf("chunk exceeds %d bytes", maxBytes)
	}
	if err := os.Rename(tmp, s.chunkPath(id, index)); err != nil {
		return n, err
	}
	m.ReceivedBitmap[index] = true
	m.LastActivity = time.Now().UTC()
	if err := s.writeManifestLocked(m); err != nil {
		return n, err
	}
	return n, nil
}

// GetChunk opens the named chunk's file for reading by the caller.
// Caller must Close the returned file.
func (s *Store) GetChunk(id string, index int) (*os.File, error) {
	s.mu.Lock()
	m, err := s.loadLocked(id)
	if err == nil && index >= 0 && index < m.TotalChunks && !m.ReceivedBitmap[index] {
		err = fmt.Errorf("chunk %d not yet uploaded", index)
	}
	if err == nil {
		m.LastActivity = time.Now().UTC()
		if m.State == StateReady {
			m.State = StateDownloading
		}
		_ = s.writeManifestLocked(m)
	}
	s.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return os.Open(s.chunkPath(id, index))
}

// MarkComplete is called by the uploader after the last PUT to confirm
// all chunks are present. The relay flips state from Uploading→Ready.
// The downloader can then start fetching.
func (s *Store) MarkReady(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := s.loadLocked(id)
	if err != nil {
		return err
	}
	missing := []int{}
	for i, ok := range m.ReceivedBitmap {
		if !ok {
			missing = append(missing, i)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing chunks: %v", missing)
	}
	m.State = StateReady
	m.LastActivity = time.Now().UTC()
	return s.writeManifestLocked(m)
}

// MarkComplete is the downloader's signal that it has successfully
// pulled every chunk and verified the plaintext checksum. The relay
// removes the transfer's chunk files but keeps the manifest briefly
// so a subsequent status check still returns a sensible response.
func (s *Store) MarkComplete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := s.loadLocked(id)
	if err != nil {
		return err
	}
	m.State = StateComplete
	m.LastActivity = time.Now().UTC()
	if err := s.writeManifestLocked(m); err != nil {
		return err
	}
	for i := 0; i < m.TotalChunks; i++ {
		_ = os.Remove(s.chunkPath(id, i))
	}
	return nil
}

// Delete removes a transfer's entire on-disk record, regardless of state.
func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return os.RemoveAll(s.transferDir(id))
}

// GC removes transfers whose last activity is older than IdleTTL.
// Intended to be called periodically by the relay (e.g., every minute).
func (s *Store) GC(now time.Time, ttl time.Duration) (removed int, err error) {
	entries, err := os.ReadDir(s.root)
	if err != nil {
		return 0, err
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		id := e.Name()
		m, err := s.Load(id)
		if err != nil {
			// Manifest missing or corrupt — nuke the dir.
			_ = s.Delete(id)
			removed++
			continue
		}
		if now.Sub(m.LastActivity) > ttl {
			_ = s.Delete(id)
			removed++
		}
	}
	return removed, nil
}

// List returns every active transfer's manifest, sorted by Created
// (newest first). Used by `rcmdd transfers` (diagnostic).
func (s *Store) List() ([]*Manifest, error) {
	entries, err := os.ReadDir(s.root)
	if err != nil {
		return nil, err
	}
	var out []*Manifest
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if m, err := s.Load(e.Name()); err == nil {
			out = append(out, m)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Created.After(out[j].Created) })
	return out, nil
}

func (s *Store) transferDir(id string) string { return filepath.Join(s.root, id) }
func (s *Store) manifestPath(id string) string {
	return filepath.Join(s.transferDir(id), "manifest.json")
}
func (s *Store) chunkPath(id string, i int) string {
	return filepath.Join(s.transferDir(id), strconv.Itoa(i)+".bin")
}

func (s *Store) loadLocked(id string) (*Manifest, error) {
	b, err := os.ReadFile(s.manifestPath(id))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("read manifest %s: %w", id, err)
	}
	var m Manifest
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("parse manifest %s: %w", id, err)
	}
	return &m, nil
}

func (s *Store) writeManifestLocked(m *Manifest) error {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.manifestPath(m.ID) + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.manifestPath(m.ID))
}
