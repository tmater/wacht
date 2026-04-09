package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRateLimitedSkipsWhenLimiterIsNil(t *testing.T) {
	h := &Handler{}
	called := false
	limited := h.rateLimited(nil, func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/public/status/demo", nil)
	rec := httptest.NewRecorder()
	limited(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	if !called {
		t.Fatal("expected wrapped handler to be called when limiter is nil")
	}
}

func TestPublicStatusRateLimiterUsesClientIP(t *testing.T) {
	h := &Handler{publicLimiter: newRateLimiter(1, time.Minute)}
	limited := h.rateLimited(h.publicLimiter, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	req1 := httptest.NewRequest(http.MethodGet, "/api/public/status/demo", nil)
	req1.RemoteAddr = "198.51.100.10:1234"
	rec1 := httptest.NewRecorder()
	limited(rec1, req1)
	if rec1.Code != http.StatusNoContent {
		t.Fatalf("first status = %d, want 204", rec1.Code)
	}

	req2 := httptest.NewRequest(http.MethodGet, "/api/public/status/demo", nil)
	req2.RemoteAddr = "198.51.100.10:5678"
	rec2 := httptest.NewRecorder()
	limited(rec2, req2)
	if rec2.Code != http.StatusTooManyRequests {
		t.Fatalf("second status = %d, want 429", rec2.Code)
	}

	req3 := httptest.NewRequest(http.MethodGet, "/api/public/status/demo", nil)
	req3.RemoteAddr = "198.51.100.11:9999"
	rec3 := httptest.NewRecorder()
	limited(rec3, req3)
	if rec3.Code != http.StatusNoContent {
		t.Fatalf("third status = %d, want 204 for different IP", rec3.Code)
	}
}
