package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"strings"
	"time"

	"github.com/obay/rcmd/internal/api"
	"github.com/obay/rcmd/internal/auth"
	"github.com/obay/rcmd/internal/crypto"
	"github.com/obay/rcmd/internal/state"
)

const userAgent = "rcmd/0.1 (+https://github.com/obay/rcmd)"

type client struct {
	state      *state.OperatorState
	relayURL   string
	operatorID string
	hmacKey    []byte
	payloadKey []byte
	http       *http.Client
}

func newClient() (*client, error) {
	s, err := state.LoadOperator(statePath)
	if err != nil {
		return nil, err
	}
	if s.RelayURL == "" {
		return nil, errors.New("state: relay_url is empty")
	}
	if s.OperatorID == "" {
		return nil, errors.New("state: operator_id is empty")
	}
	master, err := base64.StdEncoding.DecodeString(s.MasterSecret)
	if err != nil || len(master) != crypto.MasterSecretBytes {
		return nil, errors.New("state: master_secret is missing or malformed")
	}
	return &client{
		state:      s,
		relayURL:   strings.TrimRight(s.RelayURL, "/"),
		operatorID: s.OperatorID,
		hmacKey:    crypto.DeriveHMACSubkey(master),
		payloadKey: crypto.DeriveAEADSubkey(master),
		// Long enough to cover poll+result phases; per-call timeouts via context.
		http: &http.Client{Timeout: 0, Transport: defaultTransport()},
	}, nil
}

func defaultTransport() http.RoundTripper {
	return http.DefaultTransport
}

// resolveAgent picks an agent ID: explicit flag wins, otherwise state's
// default_agent. Returns an actionable error if neither is set.
func (c *client) resolveAgent(flagValue string) (string, error) {
	if flagValue != "" {
		return flagValue, nil
	}
	if c.state.DefaultAgent != "" {
		return c.state.DefaultAgent, nil
	}
	return "", errors.New("no agent specified — pass --agent NAME or set a default with 'rcmd set-default-agent NAME'")
}

// Run submits a command to the named agent and waits for the result.
func (c *client) Run(agentID string, cmd api.Command) (*api.Result, error) {
	env, err := crypto.Seal(c.payloadKey, cmd)
	if err != nil {
		return nil, fmt.Errorf("seal: %w", err)
	}
	body, _ := json.Marshal(env)
	url := fmt.Sprintf("%s/v1/agents/%s/commands", c.relayURL, agentID)
	var sub api.SubmitCommandResponse
	if err := c.doJSON(http.MethodPost, url, body, &sub); err != nil {
		return nil, fmt.Errorf("submit: %w", err)
	}
	resultURL := fmt.Sprintf("%s/v1/agents/%s/commands/%s/result", c.relayURL, agentID, sub.CommandID)
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

// ListAgents fetches the relay's seen-agents list.
func (c *client) ListAgents() ([]string, error) {
	var out api.ListAgentsResponse
	if err := c.doJSON(http.MethodGet, c.relayURL+"/v1/agents", nil, &out); err != nil {
		return nil, err
	}
	return out.Agents, nil
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
	if err := auth.Sign(req, c.operatorID, c.hmacKey, body); err != nil {
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
