package network

import "testing"

func TestValidateWebhookURL_RejectsLocalhost(t *testing.T) {
	if err := ValidateWebhookURL("http://localhost/webhook", Policy{}); err == nil {
		t.Fatal("expected localhost webhook to be rejected")
	}
}

func TestValidateWebhookURL_RejectsPrivateIPLiteral(t *testing.T) {
	if err := ValidateWebhookURL("http://127.0.0.1/webhook", Policy{}); err == nil {
		t.Fatal("expected private IP webhook to be rejected")
	}
}

func TestValidateWebhookURL_AllowsPrivateAddressWhenConfigured(t *testing.T) {
	if err := ValidateWebhookURL("http://127.0.0.1/webhook", Policy{AllowPrivateTargets: true}); err != nil {
		t.Fatalf("ValidateWebhookURL() error = %v, want nil", err)
	}
}
