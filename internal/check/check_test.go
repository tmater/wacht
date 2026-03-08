package check

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/tmater/wacht/internal/network"
	"github.com/tmater/wacht/internal/proto"
)

// HTTP tests

func TestHTTP_Up(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	result := HTTP("check-1", "probe-1", srv.URL, network.Policy{AllowPrivateTargets: true})
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

	result := HTTP("check-1", "probe-1", srv.URL, network.Policy{AllowPrivateTargets: true})
	if result.Up {
		t.Error("expected Up=false for 500 response")
	}
	if result.Error == "" {
		t.Error("expected non-empty Error for 500 response")
	}
}

func TestHTTP_Down_Unreachable(t *testing.T) {
	result := HTTP("check-1", "probe-1", "http://127.0.0.1:1", network.Policy{AllowPrivateTargets: true})
	if result.Up {
		t.Error("expected Up=false for unreachable target")
	}
	if result.Error == "" {
		t.Error("expected non-empty Error for unreachable target")
	}
}

func TestHTTP_RejectsBlockedTarget(t *testing.T) {
	result := HTTP("check-1", "probe-1", "http://127.0.0.1:1", network.Policy{})
	if result.Up {
		t.Error("expected Up=false for blocked target")
	}
	if result.Error == "" {
		t.Error("expected non-empty Error for blocked target")
	}
}

// TCP tests

func TestTCP_Up(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start listener: %v", err)
	}
	defer ln.Close()

	result := TCP("check-1", "probe-1", ln.Addr().String(), network.Policy{AllowPrivateTargets: true})
	if !result.Up {
		t.Errorf("expected Up=true, got false (error: %s)", result.Error)
	}
	if result.Type != proto.CheckTCP {
		t.Errorf("expected type %q, got %q", proto.CheckTCP, result.Type)
	}
}

func TestTCP_Down_Unreachable(t *testing.T) {
	result := TCP("check-1", "probe-1", "127.0.0.1:1", network.Policy{AllowPrivateTargets: true})
	if result.Up {
		t.Error("expected Up=false for unreachable target")
	}
	if result.Error == "" {
		t.Error("expected non-empty Error for unreachable target")
	}
}

func TestTCP_RejectsBlockedTarget(t *testing.T) {
	result := TCP("check-1", "probe-1", "127.0.0.1:1", network.Policy{})
	if result.Up {
		t.Error("expected Up=false for blocked target")
	}
	if result.Error == "" {
		t.Error("expected non-empty Error for blocked target")
	}
}

func TestValidateTarget_RejectsPrivateHTTPDestination(t *testing.T) {
	err := network.ValidateCheckTarget(context.Background(), "http", "http://127.0.0.1:8080", network.Policy{})
	if err == nil {
		t.Fatal("expected private HTTP target to be rejected")
	}
}

func TestValidateTarget_AllowsPrivateHTTPDestinationWhenConfigured(t *testing.T) {
	err := network.ValidateCheckTarget(context.Background(), "http", "http://127.0.0.1:8080", network.Policy{AllowPrivateTargets: true})
	if err != nil {
		t.Fatalf("expected private HTTP target to be allowed, got %v", err)
	}
}

func TestValidateTarget_RejectsIPForDNS(t *testing.T) {
	err := network.ValidateCheckTarget(context.Background(), "dns", "127.0.0.1", network.Policy{AllowPrivateTargets: true})
	if err == nil {
		t.Fatal("expected DNS IP literal to be rejected")
	}
}
