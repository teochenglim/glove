package decision

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/teochenglim/glove/internal/adapter"
	"github.com/teochenglim/glove/pkg/contract"
)

// ErrModelUnavailable means glove serve could not be consulted (down,
// degraded, timeout, or refused). The caller falls back to the deterministic
// verdict per policy (PRD §3.2 — fail-closed, never an open gate).
var ErrModelUnavailable = errors.New("laya unavailable")

// DefaultEndpoint is glove serve's loopback address (PRD §3.2).
func DefaultEndpoint() string {
	if v := os.Getenv("GLOVE_SERVE_ENDPOINT"); v != "" {
		return v
	}
	return "http://127.0.0.1:8799"
}

// LayaClient consults glove serve (PRD §7 0.4.0). Zero-network invariant
// (PRD §3.3): the endpoint must be loopback — a non-loopback endpoint is a
// configuration error, refused before any dial.
type LayaClient struct {
	Endpoint string
	Timeout  time.Duration
	HTTP     *http.Client
}

// NewLayaClient returns a client for endpoint with the given per-request
// timeout (policy `[laya] hook_timeout_ms`).
func NewLayaClient(endpoint string, timeout time.Duration) *LayaClient {
	if endpoint == "" {
		endpoint = DefaultEndpoint()
	}
	return &LayaClient{
		Endpoint: endpoint,
		Timeout:  timeout,
		HTTP:     &http.Client{Timeout: timeout},
	}
}

// ensureLoopback refuses non-loopback endpoints (PRD §3.3 zero-network).
func ensureLoopback(endpoint string) error {
	u, err := url.Parse(endpoint)
	if err != nil {
		return fmt.Errorf("laya endpoint %q: %w", endpoint, err)
	}
	host := u.Hostname()
	if host == "localhost" {
		return nil
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return nil
	}
	return fmt.Errorf("laya endpoint %q must be loopback (127.0.0.1, ::1 or localhost) — the decision path makes zero non-loopback network calls (PRD §3.3)", endpoint)
}

// Ask consults serve for a Layer 2 verdict. Any failure surfaces as
// ErrModelUnavailable; the deterministic verdict always stands on its own.
func (c *LayaClient) Ask(ctx context.Context, in adapter.HookInput) (contract.Verdict, error) {
	if err := ensureLoopback(c.Endpoint); err != nil {
		return contract.Verdict{}, fmt.Errorf("%w: %v", ErrModelUnavailable, err)
	}
	body, err := json.Marshal(in)
	if err != nil {
		return contract.Verdict{}, fmt.Errorf("%w: %v", ErrModelUnavailable, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Endpoint+"/evaluate", bytes.NewReader(body))
	if err != nil {
		return contract.Verdict{}, fmt.Errorf("%w: %v", ErrModelUnavailable, err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return contract.Verdict{}, fmt.Errorf("%w: %v", ErrModelUnavailable, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return contract.Verdict{}, fmt.Errorf("%w: serve answered %d", ErrModelUnavailable, resp.StatusCode)
	}
	var v contract.Verdict
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		return contract.Verdict{}, fmt.Errorf("%w: bad serve payload: %v", ErrModelUnavailable, err)
	}
	v.Deterministic = false
	v.ModelAvailable = true
	return v, nil
}
