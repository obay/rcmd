package transfer

import (
	"bytes"
	"io"
	"path/filepath"
	"strings"

	"github.com/klauspost/compress/zstd"
)

// Compression scheme strings used in manifests and command payloads.
const (
	CompressionNone = "none"
	CompressionZstd = "zstd"
)

// incompressibleExt is an inline blacklist of file extensions whose
// contents are already compressed or otherwise high-entropy. We skip
// the sample-test on these to save a few hundred ms per push.
var incompressibleExt = map[string]bool{
	".7z":   true,
	".bz2":  true,
	".gif":  true,
	".gz":   true,
	".heic": true,
	".heif": true,
	".jpeg": true,
	".jpg":  true,
	".lz4":  true,
	".mkv":  true,
	".mov":  true,
	".mp3":  true,
	".mp4":  true,
	".opus": true,
	".png":  true,
	".tar.gz": true,
	".tar.bz2": true,
	".tar.xz": true,
	".tgz":  true,
	".webm": true,
	".webp": true,
	".xz":   true,
	".zip":  true,
	".zst":  true,
}

// PickCompression decides whether to apply zstd to a payload, given
// the filename (for the extension hint) and the first few KB of the
// plaintext (for a sample compression check). Returns either
// CompressionZstd or CompressionNone.
//
// Heuristic:
//   1. If the extension is in the incompressible blacklist → none.
//   2. Else compress a 64-KiB sample with zstd-fastest. If the
//      compressed sample is smaller than ~92% of the original, use
//      zstd for the real transfer. Otherwise → none.
func PickCompression(filename string, sample []byte) string {
	if isIncompressibleExt(filename) {
		return CompressionNone
	}
	if len(sample) < 1024 {
		// Too small to meaningfully sample; just compress.
		return CompressionZstd
	}
	enc, err := zstd.NewWriter(io.Discard, zstd.WithEncoderLevel(zstd.SpeedFastest))
	if err != nil {
		return CompressionNone
	}
	defer enc.Close()
	var buf bytes.Buffer
	enc.Reset(&buf)
	limit := len(sample)
	if limit > 64*1024 {
		limit = 64 * 1024
	}
	if _, err := enc.Write(sample[:limit]); err != nil {
		return CompressionNone
	}
	if err := enc.Close(); err != nil {
		return CompressionNone
	}
	if buf.Len() < (limit*92)/100 {
		return CompressionZstd
	}
	return CompressionNone
}

func isIncompressibleExt(filename string) bool {
	lower := strings.ToLower(filename)
	// Match double-extensions like .tar.gz first.
	for ext := range incompressibleExt {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	// Fall back to last extension.
	return incompressibleExt[filepath.Ext(lower)]
}

// Compress applies zstd at default speed level. The reverse is
// Decompress. Returns the same input if scheme is CompressionNone.
func Compress(scheme string, plaintext []byte) ([]byte, error) {
	if scheme == CompressionNone {
		return plaintext, nil
	}
	enc, err := zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedDefault))
	if err != nil {
		return nil, err
	}
	out := enc.EncodeAll(plaintext, nil)
	_ = enc.Close()
	return out, nil
}

// Decompress is the inverse of Compress.
func Decompress(scheme string, compressed []byte) ([]byte, error) {
	if scheme == CompressionNone {
		return compressed, nil
	}
	dec, err := zstd.NewReader(nil)
	if err != nil {
		return nil, err
	}
	defer dec.Close()
	return dec.DecodeAll(compressed, nil)
}
