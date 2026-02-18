package alert

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
)

// AlertPayload is the JSON body sent to a webhook URL on a state transition.
type AlertPayload struct {
	CheckID     string `json:"check_id"`
	Target      string `json:"target"`
	Status      string `json:"status"` // "down" or "up"
	ProbesDown  int    `json:"probes_down"`
	ProbesTotal int    `json:"probes_total"`
}

// Fire POSTs payload as JSON to url. Returns an error if the request fails or
// the server responds with a non-2xx status.
func Fire(url string, payload AlertPayload) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	resp, err := http.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhook: unexpected status %d from %s", resp.StatusCode, url)
	}
	return nil
}
