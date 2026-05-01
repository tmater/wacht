package alert

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/tmater/wacht/internal/network"
)

func TestFire(t *testing.T) {
	var received AlertPayload
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("expected Content-Type application/json, got %s", ct)
		}
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Errorf("failed to decode body: %s", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	payload := AlertPayload{
		CheckID:     "00000000-0000-0000-0000-000000000901",
		CheckName:   "check-google",
		Target:      "https://google.com",
		Status:      "down",
		ProbesDown:  2,
		ProbesTotal: 3,
	}

	if err := Fire(http.DefaultClient, srv.URL, mustMarshal(t, payload)); err != nil {
		t.Fatalf("Fire returned error: %s", err)
	}

	if received.CheckID != payload.CheckID {
		t.Errorf("check_id: got %q, want %q", received.CheckID, payload.CheckID)
	}
	if received.Status != "down" {
		t.Errorf("status: got %q, want \"down\"", received.Status)
	}
	if received.ProbesDown != 2 {
		t.Errorf("probes_down: got %d, want 2", received.ProbesDown)
	}
}

func TestFire_NonSuccessStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	err := Fire(http.DefaultClient, srv.URL, mustMarshal(t, AlertPayload{CheckID: "x", Target: "y", Status: "down"}))
	if err == nil {
		t.Fatal("expected error for non-2xx response, got nil")
	}
}

func TestFire_DoesNotFollowRedirects(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://example.com/webhook", http.StatusFound)
	}))
	defer srv.Close()

	client := &http.Client{
		Transport: http.DefaultTransport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	err := Fire(client, srv.URL, mustMarshal(t, AlertPayload{CheckID: "x", Target: "y", Status: "down"}))
	if err == nil {
		t.Fatal("expected redirect response to be treated as an error")
	}
}

func TestFire_RejectsPrivateAddressWithGuardedClient(t *testing.T) {
	client := network.Policy{}.NewHTTPClient(webhookTimeout, 3*time.Second, false)

	err := Fire(client, "http://127.0.0.1/webhook", mustMarshal(t, AlertPayload{CheckID: "x", Target: "y", Status: "down"}))
	if err == nil {
		t.Fatal("expected private destination to be rejected")
	}
}

func mustMarshal(t *testing.T, payload AlertPayload) []byte {
	t.Helper()

	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	return body
}
