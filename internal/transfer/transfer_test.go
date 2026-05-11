package transfer

import (
	"bytes"
	"io"
	"strings"
	"testing"
	"time"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return s
}

func TestCreateLoadRoundtrip(t *testing.T) {
	s := newTestStore(t)
	want := Manifest{
		ID:          "abc-123",
		Direction:   DirectionPush,
		From:        "alice",
		To:          "win-1",
		RemotePath:  `C:\foo`,
		TotalSize:   3 * 1024,
		ChunkSize:   1024,
		TotalChunks: 3,
		SHA256Hex:   "deadbeef",
		Compression: "none",
	}
	got, err := s.Create(want)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if got.State != StateUploading {
		t.Errorf("State: got %s, want %s", got.State, StateUploading)
	}
	if len(got.ReceivedBitmap) != 3 {
		t.Errorf("bitmap len: %d, want 3", len(got.ReceivedBitmap))
	}

	loaded, err := s.Load("abc-123")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.SHA256Hex != "deadbeef" {
		t.Errorf("sha256 lost: %s", loaded.SHA256Hex)
	}
}

func TestPutChunkAndMarkReady(t *testing.T) {
	s := newTestStore(t)
	_, err := s.Create(Manifest{ID: "t1", TotalChunks: 2, ChunkSize: 5})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := s.PutChunk("t1", 0, strings.NewReader("AAAAA"), 1024); err != nil {
		t.Fatalf("PutChunk 0: %v", err)
	}
	if err := s.MarkReady("t1"); err == nil {
		t.Errorf("expected MarkReady to refuse with chunk 1 missing")
	}
	if _, err := s.PutChunk("t1", 1, strings.NewReader("BBBBB"), 1024); err != nil {
		t.Fatalf("PutChunk 1: %v", err)
	}
	if err := s.MarkReady("t1"); err != nil {
		t.Errorf("MarkReady after all chunks: %v", err)
	}
	m, _ := s.Load("t1")
	if m.State != StateReady {
		t.Errorf("state: %s, want ready", m.State)
	}
}

func TestPutChunkExceedsMax(t *testing.T) {
	s := newTestStore(t)
	_, err := s.Create(Manifest{ID: "t2", TotalChunks: 1, ChunkSize: 10})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	body := strings.NewReader(strings.Repeat("x", 200))
	if _, err := s.PutChunk("t2", 0, body, 100); err == nil {
		t.Error("expected error on oversized chunk")
	}
}

func TestGetChunkRoundtrip(t *testing.T) {
	s := newTestStore(t)
	_, err := s.Create(Manifest{ID: "t3", TotalChunks: 1, ChunkSize: 5})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	want := []byte("hello")
	if _, err := s.PutChunk("t3", 0, bytes.NewReader(want), 1024); err != nil {
		t.Fatalf("PutChunk: %v", err)
	}
	_ = s.MarkReady("t3")
	f, err := s.GetChunk("t3", 0)
	if err != nil {
		t.Fatalf("GetChunk: %v", err)
	}
	defer f.Close()
	got, _ := io.ReadAll(f)
	if !bytes.Equal(got, want) {
		t.Errorf("chunk bytes: %v, want %v", got, want)
	}
	m, _ := s.Load("t3")
	if m.State != StateDownloading {
		t.Errorf("state after first GET: %s, want downloading", m.State)
	}
}

func TestGCRemovesIdleTransfers(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.Create(Manifest{ID: "old", TotalChunks: 1, ChunkSize: 1})
	_, _ = s.Create(Manifest{ID: "fresh", TotalChunks: 1, ChunkSize: 1})

	// Backdate "old".
	m, _ := s.Load("old")
	m.LastActivity = time.Now().Add(-2 * time.Hour)
	_ = s.writeManifestLocked(m)

	removed, err := s.GC(time.Now(), IdleTTL)
	if err != nil {
		t.Fatalf("GC: %v", err)
	}
	if removed != 1 {
		t.Errorf("removed: %d, want 1", removed)
	}
	if _, err := s.Load("old"); err == nil {
		t.Error("expected old to be gone")
	}
	if _, err := s.Load("fresh"); err != nil {
		t.Error("expected fresh to survive")
	}
}

func TestDeleteAndNotFound(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.Create(Manifest{ID: "ephemeral", TotalChunks: 1, ChunkSize: 1})
	if err := s.Delete("ephemeral"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := s.Load("ephemeral"); err != ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}
