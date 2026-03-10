package checks

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tmater/wacht/internal/network"
	"gopkg.in/yaml.v3"
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
	if result.Type != string(CheckHTTP) {
		t.Errorf("expected type %q, got %q", CheckHTTP, result.Type)
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
	if result.Type != string(CheckTCP) {
		t.Errorf("expected type %q, got %q", CheckTCP, result.Type)
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

func TestCheckNormalizeAndValidateDefaultsHTTPAndInterval(t *testing.T) {
	check, err := NewCheck("  api-check  ", "", " https://1.1.1.1 ", " https://hooks.example.com/wacht ", 0).
		NormalizeAndValidate(context.Background(), network.Policy{}, true)
	if err != nil {
		t.Fatalf("NormalizeAndValidate() error = %v", err)
	}
	if check.ID != "api-check" {
		t.Fatalf("ID = %q, want trimmed id", check.ID)
	}
	if check.Type != CheckHTTP {
		t.Fatalf("Type = %q, want %q", check.Type, CheckHTTP)
	}
	if check.Target != "https://1.1.1.1" {
		t.Fatalf("Target = %q, want trimmed target", check.Target)
	}
	if check.Webhook != "https://hooks.example.com/wacht" {
		t.Fatalf("Webhook = %q, want trimmed webhook", check.Webhook)
	}
	if check.Interval != DefaultInterval {
		t.Fatalf("Interval = %d, want %d", check.Interval, DefaultInterval)
	}
}

func TestCheckNormalizeAndValidateCanonicalizesMixedCaseType(t *testing.T) {
	check, err := NewCheck("api-check", "HtTp", "https://1.1.1.1", "", 45).
		NormalizeAndValidate(context.Background(), network.Policy{}, true)
	if err != nil {
		t.Fatalf("NormalizeAndValidate() error = %v", err)
	}
	if check.Type != CheckHTTP {
		t.Fatalf("Type = %q, want %q", check.Type, CheckHTTP)
	}
}

func TestCheckNormalizeAndValidateRejectsMissingID(t *testing.T) {
	_, err := NewCheck("", "http", "https://1.1.1.1", "", 30).
		NormalizeAndValidate(context.Background(), network.Policy{}, true)
	if err == nil {
		t.Fatal("NormalizeAndValidate() error = nil, want missing id error")
	}
	if err.Error() != "id is required" {
		t.Fatalf("error = %q, want missing id error", err)
	}
}

func TestCheckNormalizeAndValidateRejectsNegativeInterval(t *testing.T) {
	_, err := NewCheck("api-check", "http", "https://1.1.1.1", "", -1).
		NormalizeAndValidate(context.Background(), network.Policy{}, true)
	if err == nil {
		t.Fatal("NormalizeAndValidate() error = nil, want interval error")
	}
	if err.Error() != "interval must be between 0 and 86400 seconds" {
		t.Fatalf("error = %q, want interval error", err)
	}
}

func TestCheckNormalizeAndValidateRejectsInvalidWebhook(t *testing.T) {
	_, err := NewCheck("api-check", "http", "https://1.1.1.1", "http://127.0.0.1/webhook", 30).
		NormalizeAndValidate(context.Background(), network.Policy{}, true)
	if err == nil {
		t.Fatal("NormalizeAndValidate() error = nil, want webhook error")
	}
	if !strings.Contains(err.Error(), "webhook:") {
		t.Fatalf("error = %q, want webhook validation error", err)
	}
}

func TestCheckJSONUsesLowercaseFieldNames(t *testing.T) {
	check := NewCheck("api-check", "http", "https://example.com", "https://hooks.example.com", 45)

	data, err := json.Marshal(check)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	if got["id"] != "api-check" {
		t.Fatalf("id = %#v, want api-check", got["id"])
	}
	if got["interval"] != float64(45) {
		t.Fatalf("interval = %#v, want 45", got["interval"])
	}
	if _, ok := got["ID"]; ok {
		t.Fatalf("unexpected uppercase ID field in %#v", got)
	}
}

func TestCheckYAMLUsesConfigFieldNames(t *testing.T) {
	var check Check
	data := []byte("id: api-check\ntype: http\ntarget: https://example.com\nwebhook: https://hooks.example.com\ninterval: 45\n")

	if err := yaml.Unmarshal(data, &check); err != nil {
		t.Fatalf("yaml.Unmarshal() error = %v", err)
	}

	if check.ID != "api-check" {
		t.Fatalf("ID = %q, want api-check", check.ID)
	}
	if check.Interval != 45 {
		t.Fatalf("Interval = %d, want 45", check.Interval)
	}
}
