package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/obay/obcmd/internal/api"
	"github.com/obay/obcmd/internal/auth"
	"github.com/obay/obcmd/internal/crypto"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

const agentUserAgent = "obcmd-agent/0.1 (+https://github.com/obay/obcmd)"

func newRunCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "run",
		Short: "Run the agent in the foreground",
		Long: strings.TrimSpace(`
run starts the agent's main loop in the foreground: long-poll the
relay for a command, execute it, post the encrypted result, repeat.

Use this for first-time testing. To run as a Windows service instead,
use 'obcmd-agent install'.

Connection failures back off exponentially (1s → 30s cap). The agent
honors HTTPS_PROXY / HTTP_PROXY env vars so it works behind explicit
corporate proxies.
`),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := initConfig(); err != nil {
				return err
			}
			a, err := newAgent()
			if err != nil {
				return err
			}
			ctx, cancel := context.WithCancel(cmd.Context())
			defer cancel()
			a.loop(ctx)
			return nil
		},
	}
}

type agent struct {
	relayURL   string
	agentID    string
	agentKey   []byte
	payloadKey []byte
	http       *http.Client
	log        *log.Logger
	defShell   string
}

func newAgent() (*agent, error) {
	relayURL := strings.TrimRight(viper.GetString("relay_url"), "/")
	if relayURL == "" {
		return nil, errors.New("config: relay_url is required")
	}
	agentID := viper.GetString("agent_id")
	if agentID == "" {
		return nil, errors.New("config: agent_id is required")
	}
	akB64 := viper.GetString("agent_key")
	pkB64 := viper.GetString("payload_key")
	if akB64 == "" || pkB64 == "" {
		return nil, errors.New("config: agent_key and payload_key are required")
	}
	ak, err := crypto.ParseKey(akB64)
	if err != nil {
		return nil, fmt.Errorf("agent_key: %w", err)
	}
	pk, err := crypto.ParseKey(pkB64)
	if err != nil {
		return nil, fmt.Errorf("payload_key: %w", err)
	}
	logger, err := openLogger(viper.GetString("log_file"))
	if err != nil {
		return nil, err
	}
	return &agent{
		relayURL:   relayURL,
		agentID:    agentID,
		agentKey:   ak,
		payloadKey: pk,
		http:       &http.Client{Timeout: 0, Transport: http.DefaultTransport},
		log:        logger,
		defShell:   viper.GetString("default_shell"),
	}, nil
}

func openLogger(path string) (*log.Logger, error) {
	if path == "" || path == "-" {
		return log.New(os.Stderr, "obcmd-agent ", log.LstdFlags|log.Lmsgprefix), nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create log dir: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open log file: %w", err)
	}
	w := io.MultiWriter(f, os.Stderr)
	return log.New(w, "obcmd-agent ", log.LstdFlags|log.Lmsgprefix), nil
}

func (a *agent) loop(ctx context.Context) {
	a.log.Printf("starting (agent_id=%s relay=%s platform=%s/%s)", a.agentID, a.relayURL, runtime.GOOS, runtime.GOARCH)
	backoff := time.Second
	const maxBackoff = 30 * time.Second
	for {
		if ctx.Err() != nil {
			a.log.Printf("shutting down: %v", ctx.Err())
			return
		}
		cid, env, err := a.pollOnce(ctx)
		if err != nil {
			a.log.Printf("poll error: %v (sleep %s)", err, backoff)
			select {
			case <-time.After(backoff):
			case <-ctx.Done():
				return
			}
			if backoff < maxBackoff {
				backoff *= 2
				if backoff > maxBackoff {
					backoff = maxBackoff
				}
			}
			continue
		}
		backoff = time.Second
		if cid == "" {
			continue // 204 — no command yet
		}
		a.handle(ctx, cid, env)
	}
}

func (a *agent) pollOnce(ctx context.Context) (string, api.Envelope, error) {
	url := fmt.Sprintf("%s/v1/agents/%s/poll", a.relayURL, a.agentID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", api.Envelope{}, err
	}
	req.Header.Set("User-Agent", agentUserAgent)
	req.Header.Set("Accept", "application/json")
	if err := auth.Sign(req, api.IdentityAgent, a.agentKey, nil); err != nil {
		return "", api.Envelope{}, err
	}
	resp, err := a.http.Do(req)
	if err != nil {
		return "", api.Envelope{}, err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusNoContent:
		_, _ = io.Copy(io.Discard, resp.Body)
		return "", api.Envelope{}, nil
	case http.StatusOK:
		var pr api.PollResponse
		if err := json.NewDecoder(resp.Body).Decode(&pr); err != nil {
			return "", api.Envelope{}, fmt.Errorf("decode poll: %w", err)
		}
		return pr.CommandID, pr.Envelope, nil
	default:
		b, _ := io.ReadAll(resp.Body)
		return "", api.Envelope{}, fmt.Errorf("relay returned %s: %s", resp.Status, strings.TrimSpace(string(b)))
	}
}

func (a *agent) handle(ctx context.Context, cid string, env api.Envelope) {
	var cmd api.Command
	if err := crypto.Open(a.payloadKey, env, &cmd); err != nil {
		a.log.Printf("decrypt %s: %v", cid, err)
		a.postResult(ctx, cid, api.Result{Ok: false, Error: "decrypt failed"})
		return
	}
	a.log.Printf("command cid=%s kind=%s", cid, cmd.Kind)
	res := a.execute(ctx, cmd)
	a.postResult(ctx, cid, res)
}

func (a *agent) execute(ctx context.Context, cmd api.Command) api.Result {
	switch cmd.Kind {
	case api.KindExec:
		shell := cmd.Shell
		if shell == "" {
			shell = a.defShell
		}
		return execShell(ctx, shell, cmd.Cmd, cmd.Cwd, cmd.TimeoutSecs)
	case api.KindPush:
		return doPush(cmd)
	case api.KindPull:
		return doPull(cmd)
	default:
		return api.Result{Ok: false, Error: "unknown command kind: " + cmd.Kind}
	}
}

func doPush(cmd api.Command) api.Result {
	if cmd.RemotePath == "" {
		return api.Result{Ok: false, Error: "remote_path is required"}
	}
	data, err := base64.StdEncoding.DecodeString(cmd.DataB64)
	if err != nil {
		return api.Result{Ok: false, Error: "decode payload: " + err.Error()}
	}
	if len(data) > api.MaxFileBytes {
		return api.Result{Ok: false, Error: "payload exceeds size limit"}
	}
	if err := os.MkdirAll(filepath.Dir(cmd.RemotePath), 0o755); err != nil {
		return api.Result{Ok: false, Error: "mkdir: " + err.Error()}
	}
	if err := os.WriteFile(cmd.RemotePath, data, 0o600); err != nil {
		return api.Result{Ok: false, Error: "write: " + err.Error()}
	}
	return api.Result{Ok: true, BytesWritten: int64(len(data))}
}

func doPull(cmd api.Command) api.Result {
	if cmd.RemotePath == "" {
		return api.Result{Ok: false, Error: "remote_path is required"}
	}
	info, err := os.Stat(cmd.RemotePath)
	if err != nil {
		return api.Result{Ok: false, Error: err.Error()}
	}
	if info.Size() > int64(api.MaxFileBytes) {
		return api.Result{Ok: false, Error: fmt.Sprintf("file is %d bytes; exceeds limit", info.Size())}
	}
	data, err := os.ReadFile(cmd.RemotePath)
	if err != nil {
		return api.Result{Ok: false, Error: err.Error()}
	}
	return api.Result{
		Ok:      true,
		DataB64: base64.StdEncoding.EncodeToString(data),
		Size:    int64(len(data)),
	}
}

func (a *agent) postResult(ctx context.Context, cid string, res api.Result) {
	env, err := crypto.Seal(a.payloadKey, res)
	if err != nil {
		a.log.Printf("seal result %s: %v", cid, err)
		return
	}
	body, _ := json.Marshal(env)
	url := fmt.Sprintf("%s/v1/agents/%s/results/%s", a.relayURL, a.agentID, cid)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		a.log.Printf("build result req %s: %v", cid, err)
		return
	}
	req.Header.Set("User-Agent", agentUserAgent)
	req.Header.Set("Content-Type", "application/json")
	if err := auth.Sign(req, api.IdentityAgent, a.agentKey, body); err != nil {
		a.log.Printf("sign result %s: %v", cid, err)
		return
	}
	resp, err := a.http.Do(req)
	if err != nil {
		a.log.Printf("post result %s: %v", cid, err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		a.log.Printf("post result %s: relay returned %s: %s", cid, resp.Status, strings.TrimSpace(string(b)))
		return
	}
	a.log.Printf("result cid=%s ok=%v exit=%d", cid, res.Ok, res.ExitCode)
}
