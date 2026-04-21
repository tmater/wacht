package probe

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/tmater/wacht/internal/proto"
)

const DefaultRequestTimeout = 10 * time.Second

// Client wraps the probe-server API used by the probe binary.
type Client struct {
	baseURL    string
	probeID    string
	secret     string
	httpClient *http.Client
}

// NewClient builds a probe-server API client with default timeouts if needed.
func NewClient(baseURL, probeID, secret string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: DefaultRequestTimeout}
	}
	return &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		probeID:    probeID,
		secret:     secret,
		httpClient: httpClient,
	}
}

// Register announces a probe startup and records its version on the server.
func (c *Client) Register(ctx context.Context, version string) error {
	reqBody := RegisterRequest{
		ProbeID: c.probeID,
		Version: version,
	}
	req, err := c.newRequest(ctx, http.MethodPost, PathRegister, reqBody)
	if err != nil {
		return err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", req.Method, req.URL.Path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("%s %s: expected status %d, got %s", req.Method, req.URL.Path, http.StatusNoContent, resp.Status)
	}
	return nil
}

// Heartbeat refreshes the probe's liveness state on the server.
func (c *Client) Heartbeat(ctx context.Context) error {
	reqBody := HeartbeatRequest{ProbeID: c.probeID}
	req, err := c.newRequest(ctx, http.MethodPost, PathHeartbeat, reqBody)
	if err != nil {
		return err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", req.Method, req.URL.Path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("%s %s: expected status %d, got %s", req.Method, req.URL.Path, http.StatusNoContent, resp.Status)
	}
	return nil
}

// FetchChecks reads the current check set assigned to the authenticated probe.
func (c *Client) FetchChecks(ctx context.Context) ([]proto.ProbeCheck, error) {
	req, err := c.newRequest(ctx, http.MethodGet, PathChecks, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", req.Method, req.URL.Path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s %s: expected status %d, got %s", req.Method, req.URL.Path, http.StatusOK, resp.Status)
	}

	var payload []proto.ProbeCheck
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}
	return payload, nil
}

// PostResult submits one executed check result back to the server.
func (c *Client) PostResult(ctx context.Context, result proto.CheckResult) error {
	return c.PostResults(ctx, []proto.CheckResult{result})
}

// PostResults submits one flushed batch of executed check results back to the
// server.
func (c *Client) PostResults(ctx context.Context, results []proto.CheckResult) error {
	req, err := c.newRequest(ctx, http.MethodPost, PathResults, ResultBatchRequest{Results: results})
	if err != nil {
		return err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", req.Method, req.URL.Path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("%s %s: expected status %d, got %s", req.Method, req.URL.Path, http.StatusNoContent, resp.Status)
	}
	return nil
}

func (c *Client) newRequest(ctx context.Context, method, path string, body any) (*http.Request, error) {
	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(payload)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set(HeaderProbeID, c.probeID)
	req.Header.Set(HeaderProbeSecret, c.secret)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req, nil
}
