package transfer

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/cenkalti/backoff/v4"
	"github.com/obay/rcmd/internal/api"
	"github.com/obay/rcmd/internal/crypto"
	"golang.org/x/sync/errgroup"
)

// DefaultChunkSize is the per-chunk plaintext size. Each chunk's
// ciphertext on the wire is this plus 16 bytes (AES-GCM tag).
const DefaultChunkSize = 1 * 1024 * 1024

// DefaultParallel is how many chunks to upload/download at once.
const DefaultParallel = 4

// Signer attaches the rcmd HMAC headers to a request whose body is
// the given bytes. Caller knows the identity to claim (operator ID
// or agent ID) and the HMAC subkey.
type Signer func(req *http.Request, body []byte) error

// Uploader streams a plaintext payload to the relay as chunked,
// optionally compressed, AES-GCM-encrypted ciphertext. The same
// type is used by the operator (push) and by the agent (pull).
type Uploader struct {
	HTTP     *http.Client
	RelayURL string
	Sign     Signer
	Parallel int // 0 → DefaultParallel
}

// UploadOpts are the parameters for a single upload session.
type UploadOpts struct {
	Direction    string // "push" or "pull"
	AgentID      string // target agent_id for push; source agent_id for pull
	RemotePath   string
	ChunkSize    int64  // 0 → DefaultChunkSize
	Compression  string // CompressionZstd | CompressionNone | "" (auto)
	Filename     string // used by auto-compression heuristic
	MasterSecret []byte // for deriving the per-transfer AES key
}

// UploadResult is returned on success.
type UploadResult struct {
	TransferID  string
	TotalChunks int
	TotalSize   int64
	ChunkSize   int64
	SHA256Hex   string
	Compression string
}

// Upload sends the plaintext and returns the transfer record the
// downloader needs to fetch the bytes.
func (u *Uploader) Upload(ctx context.Context, plaintext []byte, opts UploadOpts) (*UploadResult, error) {
	if opts.ChunkSize == 0 {
		opts.ChunkSize = DefaultChunkSize
	}
	scheme := opts.Compression
	if scheme == "" {
		sample := plaintext
		if len(sample) > 64*1024 {
			sample = sample[:64*1024]
		}
		scheme = PickCompression(opts.Filename, sample)
	}
	stream, err := Compress(scheme, plaintext)
	if err != nil {
		return nil, fmt.Errorf("compress: %w", err)
	}
	sum := sha256.Sum256(stream)
	sumHex := hex.EncodeToString(sum[:])
	totalSize := int64(len(stream))
	totalChunks := int((totalSize + opts.ChunkSize - 1) / opts.ChunkSize)
	if totalChunks == 0 {
		totalChunks = 1 // empty file still gets one (zero-length) chunk
	}

	// 1. Create the transfer.
	cr := api.CreateTransferRequest{
		Direction:   opts.Direction,
		AgentID:     opts.AgentID,
		RemotePath:  opts.RemotePath,
		TotalSize:   totalSize,
		ChunkSize:   opts.ChunkSize,
		TotalChunks: totalChunks,
		SHA256Hex:   sumHex,
		Compression: scheme,
	}
	body, _ := json.Marshal(cr)
	var cresp api.CreateTransferResponse
	if err := u.doJSON(ctx, http.MethodPost, u.RelayURL+"/v1/transfers", body, &cresp); err != nil {
		return nil, fmt.Errorf("create transfer: %w", err)
	}
	transferID := cresp.TransferID
	key := crypto.DeriveTransferKey(opts.MasterSecret, transferID)

	// 2. Upload chunks in parallel, with per-chunk backoff retry.
	if err := u.uploadChunks(ctx, transferID, key, stream, opts.ChunkSize, totalChunks); err != nil {
		return nil, err
	}

	// 3. Mark ready.
	if err := u.doNoBody(ctx, http.MethodPost, u.RelayURL+"/v1/transfers/"+transferID+"/complete"); err != nil {
		return nil, fmt.Errorf("complete: %w", err)
	}

	return &UploadResult{
		TransferID:  transferID,
		TotalChunks: totalChunks,
		TotalSize:   totalSize,
		ChunkSize:   opts.ChunkSize,
		SHA256Hex:   sumHex,
		Compression: scheme,
	}, nil
}

func (u *Uploader) uploadChunks(ctx context.Context, transferID string, key []byte, stream []byte, chunkSize int64, totalChunks int) error {
	parallel := u.Parallel
	if parallel == 0 {
		parallel = DefaultParallel
	}
	sem := make(chan struct{}, parallel)
	g, gctx := errgroup.WithContext(ctx)
	for idx := 0; idx < totalChunks; idx++ {
		idx := idx
		start := int64(idx) * chunkSize
		end := start + chunkSize
		if end > int64(len(stream)) {
			end = int64(len(stream))
		}
		var chunk []byte
		if start < end {
			chunk = stream[start:end]
		} else {
			chunk = []byte{}
		}
		sem <- struct{}{}
		g.Go(func() error {
			defer func() { <-sem }()
			ct, err := crypto.SealChunk(key, uint32(idx), chunk)
			if err != nil {
				return fmt.Errorf("seal chunk %d: %w", idx, err)
			}
			return u.putChunkWithRetry(gctx, transferID, idx, ct)
		})
	}
	return g.Wait()
}

func (u *Uploader) putChunkWithRetry(ctx context.Context, transferID string, idx int, ciphertext []byte) error {
	url := fmt.Sprintf("%s/v1/transfers/%s/chunks/%d", u.RelayURL, transferID, idx)
	op := func() error {
		req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(ciphertext))
		if err != nil {
			return backoff.Permanent(err)
		}
		req.Header.Set("Content-Type", "application/octet-stream")
		// Auth headers cover the body hash, but the body hash is over
		// the raw ciphertext exactly as the relay will read it.
		if err := u.Sign(req, ciphertext); err != nil {
			return backoff.Permanent(err)
		}
		resp, err := u.HTTP.Do(req)
		if err != nil {
			return err // retryable
		}
		defer resp.Body.Close()
		if resp.StatusCode == http.StatusNoContent || resp.StatusCode == http.StatusOK {
			return nil
		}
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		if resp.StatusCode >= 400 && resp.StatusCode < 500 {
			return backoff.Permanent(fmt.Errorf("chunk %d: %s: %s", idx, resp.Status, strings.TrimSpace(string(b))))
		}
		return fmt.Errorf("chunk %d: %s", idx, resp.Status)
	}
	return backoff.Retry(op, withDefaultBackoff(ctx))
}

// Downloader fetches every chunk of a transfer, decrypts and
// reassembles them into the plaintext stream. The caller decides
// what to do with the resulting bytes.
type Downloader struct {
	HTTP     *http.Client
	RelayURL string
	Sign     Signer
	Parallel int
}

// DownloadResult is the reassembled, decompressed payload.
type DownloadResult struct {
	Plaintext   []byte
	Compression string
	SHA256Hex   string // sha256 of the post-compression stream, as advertised by the manifest
}

// Download fetches the full transfer (post-decompression). It looks
// up the manifest first to discover the chunk count, key, scheme,
// and expected checksum.
func (d *Downloader) Download(ctx context.Context, transferID string, masterSecret []byte) (*DownloadResult, error) {
	// 1. Fetch manifest.
	var manifest api.TransferStatusResponse
	if err := d.doJSON(ctx, http.MethodGet, d.RelayURL+"/v1/transfers/"+transferID+"/status", nil, &manifest); err != nil {
		return nil, fmt.Errorf("status: %w", err)
	}
	if manifest.TotalChunks <= 0 {
		return nil, errors.New("transfer has no chunks")
	}

	key := crypto.DeriveTransferKey(masterSecret, transferID)
	chunks := make([][]byte, manifest.TotalChunks)

	// 2. Fetch chunks in parallel with retry.
	parallel := d.Parallel
	if parallel == 0 {
		parallel = DefaultParallel
	}
	sem := make(chan struct{}, parallel)
	g, gctx := errgroup.WithContext(ctx)
	for idx := 0; idx < manifest.TotalChunks; idx++ {
		idx := idx
		sem <- struct{}{}
		g.Go(func() error {
			defer func() { <-sem }()
			ct, err := d.getChunkWithRetry(gctx, transferID, idx)
			if err != nil {
				return err
			}
			pt, err := crypto.OpenChunk(key, uint32(idx), ct)
			if err != nil {
				return fmt.Errorf("decrypt chunk %d: %w", idx, err)
			}
			chunks[idx] = pt
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}

	// 3. Concatenate in order.
	var totalLen int
	for _, c := range chunks {
		totalLen += len(c)
	}
	stream := make([]byte, 0, totalLen)
	for _, c := range chunks {
		stream = append(stream, c...)
	}

	// 4. Verify checksum (against the compressed stream).
	sum := sha256.Sum256(stream)
	if hex.EncodeToString(sum[:]) != manifest.SHA256Hex {
		return nil, fmt.Errorf("checksum mismatch: got %x, want %s", sum, manifest.SHA256Hex)
	}

	// 5. Decompress.
	plaintext, err := Decompress(manifest.Compression, stream)
	if err != nil {
		return nil, fmt.Errorf("decompress: %w", err)
	}

	return &DownloadResult{
		Plaintext:   plaintext,
		Compression: manifest.Compression,
		SHA256Hex:   manifest.SHA256Hex,
	}, nil
}

func (d *Downloader) getChunkWithRetry(ctx context.Context, transferID string, idx int) ([]byte, error) {
	url := fmt.Sprintf("%s/v1/transfers/%s/chunks/%d", d.RelayURL, transferID, idx)
	var out []byte
	op := func() error {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return backoff.Permanent(err)
		}
		if err := d.Sign(req, nil); err != nil {
			return backoff.Permanent(err)
		}
		resp, err := d.HTTP.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
			if resp.StatusCode >= 400 && resp.StatusCode < 500 {
				return backoff.Permanent(fmt.Errorf("chunk %d: %s: %s", idx, resp.Status, strings.TrimSpace(string(b))))
			}
			return fmt.Errorf("chunk %d: %s", idx, resp.Status)
		}
		buf, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}
		out = buf
		return nil
	}
	if err := backoff.Retry(op, withDefaultBackoff(ctx)); err != nil {
		return nil, err
	}
	return out, nil
}

// MarkDone signals the relay that the downloader has successfully
// pulled and verified the transfer, so it can delete the chunk files.
func (d *Downloader) MarkDone(ctx context.Context, transferID string) error {
	return doNoBody(ctx, d.HTTP, d.Sign, http.MethodPost, d.RelayURL+"/v1/transfers/"+transferID+"/done")
}

// --- small request helpers ---

func (u *Uploader) doJSON(ctx context.Context, method, url string, body []byte, into any) error {
	return doJSON(ctx, u.HTTP, u.Sign, method, url, body, into)
}

func (u *Uploader) doNoBody(ctx context.Context, method, url string) error {
	return doNoBody(ctx, u.HTTP, u.Sign, method, url)
}

func (d *Downloader) doJSON(ctx context.Context, method, url string, body []byte, into any) error {
	return doJSON(ctx, d.HTTP, d.Sign, method, url, body, into)
}

func doJSON(ctx context.Context, hc *http.Client, sign Signer, method, url string, body []byte, into any) error {
	var rdr io.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, rdr)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	if err := sign(req, body); err != nil {
		return err
	}
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("%s: %s", resp.Status, strings.TrimSpace(string(b)))
	}
	if into != nil {
		return json.NewDecoder(resp.Body).Decode(into)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}

func doNoBody(ctx context.Context, hc *http.Client, sign Signer, method, url string) error {
	req, err := http.NewRequestWithContext(ctx, method, url, nil)
	if err != nil {
		return err
	}
	if err := sign(req, nil); err != nil {
		return err
	}
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("%s: %s", resp.Status, strings.TrimSpace(string(b)))
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}

func withDefaultBackoff(ctx context.Context) backoff.BackOffContext {
	b := backoff.NewExponentialBackOff()
	b.InitialInterval = 500 * time.Millisecond
	b.MaxInterval = 16 * time.Second
	b.MaxElapsedTime = 5 * time.Minute
	b.Multiplier = 2
	return backoff.WithContext(b, ctx)
}

// Make sure unused vars don't warn on stripped builds.
var _ = sync.Mutex{}
