package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os/exec"
	"time"

	"github.com/obay/obcmd/internal/api"
)

// runCapture starts cmd with stdout+stderr piped through size-bounded
// writers, waits for completion or context cancellation, and returns a
// Result. Used by both Windows and Unix exec paths.
func runCapture(ctx context.Context, cmd *exec.Cmd, timeoutSecs int) api.Result {
	stdoutBuf := newBoundedBuffer(api.MaxOutputBytes / 2)
	stderrBuf := newBoundedBuffer(api.MaxOutputBytes / 2)
	cmd.Stdout = stdoutBuf
	cmd.Stderr = stderrBuf

	start := time.Now()
	if err := cmd.Start(); err != nil {
		return api.Result{
			ExitCode:   127,
			Stderr:     err.Error() + "\n",
			DurationMs: time.Since(start).Milliseconds(),
			Error:      err.Error(),
		}
	}
	err := cmd.Wait()
	dur := time.Since(start)

	res := api.Result{
		Stdout:     stdoutBuf.String(),
		Stderr:     stderrBuf.String(),
		DurationMs: dur.Milliseconds(),
		Truncated:  stdoutBuf.Truncated() || stderrBuf.Truncated(),
	}

	switch {
	case err == nil:
		res.ExitCode = 0
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		res.ExitCode = 124 // canonical timeout exit code
		res.Error = "timeout after " + (time.Duration(timeoutSecs) * time.Second).String()
	default:
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			res.ExitCode = exitErr.ExitCode()
		} else {
			res.ExitCode = 1
			res.Error = err.Error()
		}
	}
	return res
}

// boundedBuffer is an io.Writer that caps its size and tracks whether
// it has been truncated.
type boundedBuffer struct {
	buf       *bytes.Buffer
	cap       int
	truncated bool
}

func newBoundedBuffer(cap int) *boundedBuffer {
	return &boundedBuffer{buf: &bytes.Buffer{}, cap: cap}
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	remaining := b.cap - b.buf.Len()
	if remaining <= 0 {
		b.truncated = true
		return len(p), nil // pretend success so the child doesn't block
	}
	if len(p) > remaining {
		b.buf.Write(p[:remaining])
		b.truncated = true
		return len(p), nil
	}
	return b.buf.Write(p)
}

func (b *boundedBuffer) String() string  { return b.buf.String() }
func (b *boundedBuffer) Truncated() bool { return b.truncated }

// ensure io.Writer satisfied
var _ io.Writer = (*boundedBuffer)(nil)
