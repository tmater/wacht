package network

import (
	"context"
	"testing"
)

func TestPolicyValidateLiteralHost_RejectsLocalhost(t *testing.T) {
	policy := Policy{}
	if err := policy.ValidateLiteralHost("localhost"); err == nil {
		t.Fatal("expected localhost to be rejected")
	}
}

func TestPolicyValidateLiteralHost_RejectsPrivateIP(t *testing.T) {
	policy := Policy{}
	if err := policy.ValidateLiteralHost("127.0.0.1"); err == nil {
		t.Fatal("expected private IP literal to be rejected")
	}
}

func TestPolicyValidateLiteralHost_AllowsPrivateIPWhenConfigured(t *testing.T) {
	policy := Policy{AllowPrivateTargets: true}
	if err := policy.ValidateLiteralHost("127.0.0.1"); err != nil {
		t.Fatalf("expected private IP literal to be allowed, got %v", err)
	}
}

func TestPolicyValidateHostPort_RejectsInvalidPort(t *testing.T) {
	policy := Policy{}
	if err := policy.ValidateHostPort(context.Background(), "example.com:not-a-port"); err == nil {
		t.Fatal("expected invalid port to be rejected")
	}
}

func TestPolicyValidateHostPort_RejectsPrivateLiteral(t *testing.T) {
	policy := Policy{}
	if err := policy.ValidateHostPort(context.Background(), "127.0.0.1:5432"); err == nil {
		t.Fatal("expected private host:port to be rejected")
	}
}
