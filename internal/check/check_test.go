package check

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/tmater/wacht/internal/proto"
)

// HTTP tests

func TestHTTP_Up(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	result := HTTP("check-1", "probe-1", srv.URL)
	if !result.Up {
		t.Errorf("expected Up=true, got false (error: %s)", result.Error)
	}
	if result.Type != proto.CheckHTTP {
		t.Errorf("expected type %q, got %q", proto.CheckHTTP, result.Type)
	}
}

func TestHTTP_Down_Non2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	result := HTTP("check-1", "probe-1", srv.URL)
	if result.Up {
		t.Error("expected Up=false for 500 response")
	}
	if result.Error == "" {
		t.Error("expected non-empty Error for 500 response")
	}
}

func TestHTTP_Down_Unreachable(t *testing.T) {
	result := HTTP("check-1", "probe-1", "http://127.0.0.1:1")
	if result.Up {
		t.Error("expected Up=false for unreachable target")
	}
	if result.Error == "" {
		t.Error("expected non-empty Error for unreachable target")
	}
}

// TCP tests

func TestTCP_Up(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start listener: %v", err)
	}
	defer ln.Close()

	result := TCP("check-1", "probe-1", ln.Addr().String())
	if !result.Up {
		t.Errorf("expected Up=true, got false (error: %s)", result.Error)
	}
	if result.Type != proto.CheckTCP {
		t.Errorf("expected type %q, got %q", proto.CheckTCP, result.Type)
	}
}

func TestTCP_Down_Unreachable(t *testing.T) {
	result := TCP("check-1", "probe-1", "127.0.0.1:1")
	if result.Up {
		t.Error("expected Up=false for unreachable target")
	}
	if result.Error == "" {
		t.Error("expected non-empty Error for unreachable target")
	}
}
