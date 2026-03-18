package alert

import (
	"bytes"
	"fmt"
	"net/http"
	"time"
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

// Fire POSTs a pre-rendered JSON payload using the provided guarded client.
func Fire(client *http.Client, url string, body []byte) error {
	if client == nil {
		return fmt.Errorf("webhook: client is required")
	}
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhook: unexpected status %d from %s", resp.StatusCode, url)
	}
	return nil
}
