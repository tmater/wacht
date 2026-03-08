package alert

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/tmater/wacht/internal/network"
)

// AlertPayload is the JSON body sent to a webhook URL on a state transition.
type AlertPayload struct {
	CheckID     string `json:"check_id"`
	Target      string `json:"target"`
	Status      string `json:"status"` // "down" or "up"
	ProbesDown  int    `json:"probes_down"`
	ProbesTotal int    `json:"probes_total"`
}

const webhookTimeout = 5 * time.Second

var webhookPolicy = network.Policy{}
var webhookClient = newWebhookClient()

func newWebhookClient() *http.Client {
	return webhookPolicy.NewHTTPClient(webhookTimeout, 3*time.Second, false)
}

// ValidateWebhookURL reports whether rawURL is a syntactically valid webhook
// target when checks are created or updated.
func ValidateWebhookURL(rawURL string) error {
	if rawURL == "" {
		return nil
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("webhook: invalid URL: %w", err)
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return fmt.Errorf("webhook: unsupported URL scheme %q", u.Scheme)
	}
	if u.Host == "" {
		return fmt.Errorf("webhook: host is required")
	}
	if u.User != nil {
		return fmt.Errorf("webhook: userinfo is not allowed")
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("webhook: host is required")
	}
	if err := webhookPolicy.ValidateLiteralHost(host); err != nil {
		return fmt.Errorf("webhook: %w", err)
	}
	return nil
}

// Fire POSTs payload as JSON to url. Runtime destination checks are enforced by
// the guarded webhook client before any outbound connection is made.
func Fire(url string, payload AlertPayload) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := webhookClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhook: unexpected status %d from %s", resp.StatusCode, url)
	}
	return nil
}
