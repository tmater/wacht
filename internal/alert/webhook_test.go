package alert

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
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
		CheckID:     "check-google",
		Target:      "https://google.com",
		Status:      "down",
		ProbesDown:  2,
		ProbesTotal: 3,
	}

	if err := Fire(srv.URL, payload); err != nil {
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

	err := Fire(srv.URL, AlertPayload{CheckID: "x", Target: "y", Status: "down"})
	if err == nil {
		t.Fatal("expected error for non-2xx response, got nil")
	}
}
