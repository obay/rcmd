package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"strings"
	"time"

	"github.com/obay/obcmd/internal/api"
	"github.com/obay/obcmd/internal/auth"
	"github.com/obay/obcmd/internal/crypto"
	"github.com/spf13/viper"
)

const userAgent = "obcmd/0.1 (+https://github.com/obay/obcmd)"

type client struct {
	relayURL    string
	agentID     string
	operatorKey []byte
	payloadKey  []byte
	http        *http.Client
}

func newClient() (*client, error) {
	if err := initConfig(); err != nil {
		return nil, err
	}
	relayURL := strings.TrimRight(viper.GetString("relay_url"), "/")
	if relayURL == "" {
		return nil, errors.New("config: relay_url is required")
	}
	agentID := viper.GetString("agent_id")
	if agentID == "" {
		return nil, errors.New("config: agent_id is required")
	}
	opKeyB64 := viper.GetString("operator_key")
	plKeyB64 := viper.GetString("payload_key")
	if opKeyB64 == "" || plKeyB64 == "" {
		return nil, errors.New("config: operator_key and payload_key are required")
	}
	opKey, err := crypto.ParseKey(opKeyB64)
	if err != nil {
		return nil, fmt.Errorf("operator_key: %w", err)
	}
	plKey, err := crypto.ParseKey(plKeyB64)
	if err != nil {
		return nil, fmt.Errorf("payload_key: %w", err)
	}
	return &client{
		relayURL:    relayURL,
		agentID:     agentID,
		operatorKey: opKey,
		payloadKey:  plKey,
		// Long enough to cover poll+result phases; the CLI handles its own
		// per-step timeouts via context if we need finer control.
		http: &http.Client{Timeout: 0, Transport: defaultTransport()},
	}, nil
}

func defaultTransport() http.RoundTripper {
	// http.DefaultTransport honors HTTP_PROXY/HTTPS_PROXY env vars,
	// which matters if the corporate firewall forces a proxy. We use
	// the system trust store (default), so MITM TLS inspection just
	// works at the transport layer; confidentiality comes from AEAD.
	return http.DefaultTransport
}

// Run submits the command and waits for the result.
func (c *client) Run(cmd api.Command) (*api.Result, error) {
	env, err := crypto.Seal(c.payloadKey, cmd)
	if err != nil {
		return nil, fmt.Errorf("seal: %w", err)
	}
	body, _ := json.Marshal(env)
	url := fmt.Sprintf("%s/v1/agents/%s/commands", c.relayURL, c.agentID)
	var sub api.SubmitCommandResponse
	if err := c.doJSON(http.MethodPost, url, body, &sub); err != nil {
		return nil, fmt.Errorf("submit: %w", err)
	}
	// Long-poll for the result; loop in case of 202 timeouts.
	resultURL := fmt.Sprintf("%s/v1/agents/%s/commands/%s/result", c.relayURL, c.agentID, sub.CommandID)
	deadline := time.Now().Add(time.Duration(cmd.TimeoutSecs+30) * time.Second)
	for {
		var rr api.ResultResponse
		status, err := c.doJSONRaw(http.MethodGet, resultURL, nil, &rr)
		if err != nil {
			return nil, fmt.Errorf("wait result: %w", err)
		}
		if status == http.StatusAccepted {
			if time.Now().After(deadline) {
				return nil, fmt.Errorf("timed out waiting for agent result")
			}
			continue
		}
		var res api.Result
		if err := crypto.Open(c.payloadKey, rr.Envelope, &res); err != nil {
			return nil, fmt.Errorf("open result: %w", err)
		}
		return &res, nil
	}
}

func (c *client) doJSON(method, url string, body []byte, into any) error {
	status, err := c.doJSONRaw(method, url, body, into)
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("http %d", status)
	}
	return nil
}

func (c *client) doJSONRaw(method, url string, body []byte, into any) (int, error) {
	var rdr io.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, url, rdr)
	if err != nil {
		return 0, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if err := auth.Sign(req, api.IdentityOperator, c.operatorKey, body); err != nil {
		return 0, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusAccepted {
		_, _ = io.Copy(io.Discard, resp.Body)
		return resp.StatusCode, nil
	}
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, fmt.Errorf("server: %s: %s", resp.Status, strings.TrimSpace(string(b)))
	}
	if into != nil {
		if err := json.NewDecoder(resp.Body).Decode(into); err != nil {
			return resp.StatusCode, fmt.Errorf("decode: %w", err)
		}
	} else {
		_, _ = io.Copy(io.Discard, resp.Body)
	}
	return resp.StatusCode, nil
}

// platformTag is included in some error messages for clarity.
func platformTag() string { return runtime.GOOS + "/" + runtime.GOARCH }
