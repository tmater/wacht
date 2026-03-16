package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/tmater/wacht/internal/proto"
)

func TestFetchChecksDecodesProbePayload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/probes/checks" {
			t.Fatalf("path = %s, want /api/probes/checks", r.URL.Path)
		}
		if got := r.Header.Get(probeIDHeader); got != "probe-1" {
			t.Fatalf("%s = %q, want probe-1", probeIDHeader, got)
		}
		if got := r.Header.Get(probeSecretHeader); got != "secret-1" {
			t.Fatalf("%s = %q, want secret-1", probeSecretHeader, got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"id":"check-1","type":"http","target":"https://example.com","interval":45}]`))
	}))
	defer server.Close()

	got, err := fetchChecks(server.URL, "secret-1", "probe-1")
	if err != nil {
		t.Fatalf("fetchChecks() error = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len(got) = %d, want 1", len(got))
	}
	if got[0].ID != "check-1" {
		t.Fatalf("got[0].ID = %q, want check-1", got[0].ID)
	}
	if got[0].Type != "http" {
		t.Fatalf("got[0].Type = %q, want http", got[0].Type)
	}
	if got[0].Target != "https://example.com" {
		t.Fatalf("got[0].Target = %q, want target", got[0].Target)
	}
	if got[0].Interval != 45 {
		t.Fatalf("got[0].Interval = %d, want 45", got[0].Interval)
	}
	if got[0] != (proto.ProbeCheck{ID: "check-1", Type: "http", Target: "https://example.com", Interval: 45}) {
		t.Fatalf("got[0] = %#v, want probe payload", got[0])
	}
}
