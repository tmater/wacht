package proto

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestProbeCheckJSONOmitsWebhookField(t *testing.T) {
	body, err := json.Marshal(ProbeCheck{
		ID:       "check-1",
		Type:     "http",
		Target:   "https://example.com",
		Interval: 45,
	})
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	if strings.Contains(string(body), "webhook") {
		t.Fatalf("encoded payload leaked webhook field: %s", body)
	}
}
