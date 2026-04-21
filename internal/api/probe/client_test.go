package probe

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/tmater/wacht/internal/proto"
)

func TestFetchChecksIncludesHeadersAndDecodesPayload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != PathChecks {
			t.Fatalf("path = %s, want %s", r.URL.Path, PathChecks)
		}
		if got := r.Header.Get(HeaderProbeID); got != "probe-1" {
			t.Fatalf("%s = %q, want probe-1", HeaderProbeID, got)
		}
		if got := r.Header.Get(HeaderProbeSecret); got != "secret-1" {
			t.Fatalf("%s = %q, want secret-1", HeaderProbeSecret, got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"id":"check-1","type":"http","target":"https://example.com","interval":45}]`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "probe-1", "secret-1", nil)
	got, err := client.FetchChecks(context.Background())
	if err != nil {
		t.Fatalf("FetchChecks() error = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len(got) = %d, want 1", len(got))
	}
	if got[0] != (proto.ProbeCheck{ID: "check-1", Type: "http", Target: "https://example.com", Interval: 45}) {
		t.Fatalf("got[0] = %#v, want probe payload", got[0])
	}
}

func TestProbeServerAPIRequestsFailOnUnexpectedStatus(t *testing.T) {
	tests := []struct {
		name string
		path string
		run  func(*Client) error
	}{
		{
			name: "register",
			path: PathRegister,
			run: func(client *Client) error {
				return client.Register(context.Background(), "dev")
			},
		},
		{
			name: "heartbeat",
			path: PathHeartbeat,
			run: func(client *Client) error {
				return client.Heartbeat(context.Background())
			},
		},
		{
			name: "check-sync",
			path: PathChecks,
			run: func(client *Client) error {
				_, err := client.FetchChecks(context.Background())
				return err
			},
		},
		{
			name: "result-post",
			path: PathResults,
			run: func(client *Client) error {
				return client.PostResult(context.Background(), proto.CheckResult{
					CheckID: "check-1",
					ProbeID: "probe-1",
					Up:      true,
				})
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != tt.path {
					t.Fatalf("path = %s, want %s", r.URL.Path, tt.path)
				}
				w.WriteHeader(http.StatusServiceUnavailable)
			}))
			defer server.Close()

			client := NewClient(server.URL, "probe-1", "secret-1", nil)
			err := tt.run(client)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), "got 503 Service Unavailable") {
				t.Fatalf("error = %v, want actual status text", err)
			}
			if !strings.Contains(err.Error(), tt.path) {
				t.Fatalf("error = %v, want request path", err)
			}
		})
	}
}

func TestProbeServerAPIRequestsTimeout(t *testing.T) {
	httpClient := &http.Client{Timeout: 20 * time.Millisecond}

	tests := []struct {
		name string
		run  func(*Client) error
	}{
		{
			name: "register",
			run: func(client *Client) error {
				return client.Register(context.Background(), "dev")
			},
		},
		{
			name: "heartbeat",
			run: func(client *Client) error {
				return client.Heartbeat(context.Background())
			},
		},
		{
			name: "check-sync",
			run: func(client *Client) error {
				_, err := client.FetchChecks(context.Background())
				return err
			},
		},
		{
			name: "result-post",
			run: func(client *Client) error {
				return client.PostResult(context.Background(), proto.CheckResult{
					CheckID: "check-1",
					ProbeID: "probe-1",
					Up:      true,
				})
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				time.Sleep(100 * time.Millisecond)
				w.WriteHeader(http.StatusNoContent)
			}))
			defer server.Close()

			client := NewClient(server.URL, "probe-1", "secret-1", httpClient)
			err := tt.run(client)
			if err == nil {
				t.Fatal("expected timeout error, got nil")
			}
			var netErr net.Error
			if !errors.As(err, &netErr) || !netErr.Timeout() {
				t.Fatalf("error = %v, want timeout", err)
			}
		})
	}
}

func TestPostResultsEncodesBatchPayload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != PathResults {
			t.Fatalf("path = %s, want %s", r.URL.Path, PathResults)
		}
		var req ResultBatchRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode batch payload: %v", err)
		}
		if len(req.Results) != 2 {
			t.Fatalf("results len = %d, want 2", len(req.Results))
		}
		if req.Results[0].CheckID != "check-1" || !req.Results[0].Up {
			t.Fatalf("results[0] = %#v, want check-1 up", req.Results[0])
		}
		if req.Results[1].CheckID != "check-2" || req.Results[1].Up {
			t.Fatalf("results[1] = %#v, want check-2 down", req.Results[1])
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := NewClient(server.URL, "probe-1", "secret-1", nil)
	err := client.PostResults(context.Background(), []proto.CheckResult{
		{CheckID: "check-1", ProbeID: "probe-1", Up: true},
		{CheckID: "check-2", ProbeID: "probe-1", Up: false, Error: "timeout"},
	})
	if err != nil {
		t.Fatalf("PostResults() error = %v", err)
	}
}

func TestIsRetryablePostResultsError(t *testing.T) {
	tests := []struct {
		name      string
		err       error
		retryable bool
	}{
		{
			name: "unauthorized response",
			err: &ResponseError{
				Method:     "POST",
				Path:       PathResults,
				StatusCode: http.StatusUnauthorized,
				Status:     "401 Unauthorized",
				Expected:   http.StatusNoContent,
			},
			retryable: false,
		},
		{
			name: "server error response",
			err: &ResponseError{
				Method:     "POST",
				Path:       PathResults,
				StatusCode: http.StatusServiceUnavailable,
				Status:     "503 Service Unavailable",
				Expected:   http.StatusNoContent,
			},
			retryable: true,
		},
		{
			name:      "generic local error",
			err:       errors.New("temporary failure"),
			retryable: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsRetryablePostResultsError(tt.err); got != tt.retryable {
				t.Fatalf("IsRetryablePostResultsError(%v) = %t, want %t", tt.err, got, tt.retryable)
			}
		})
	}
}
