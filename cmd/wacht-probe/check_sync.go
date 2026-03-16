package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/tmater/wacht/internal/proto"
)

// checkSyncLoop polls the server for the current check set and hands the
// result to the scheduler without coupling that work to heartbeats.
func checkSyncLoop(serverURL, secret, probeID string, interval time.Duration, onChecks func([]proto.ProbeCheck)) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for range ticker.C {
		updated, err := fetchChecks(serverURL, secret, probeID)
		if err != nil {
			log.Printf("probe: failed to refresh checks: %s", err)
			continue
		}
		log.Printf("probe: refreshed %d checks from server", len(updated))
		onChecks(updated)
	}
}

// fetchChecks reads the full probe-visible check list from the server so the
// scheduler can reconcile local workers against the latest desired state.
func fetchChecks(serverURL, secret, probeID string) ([]proto.ProbeCheck, error) {
	req, err := http.NewRequest("GET", serverURL+"/api/probes/checks", nil)
	if err != nil {
		return nil, err
	}
	setProbeHeaders(req, probeID, secret)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	var payload []proto.ProbeCheck
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}
	return payload, nil
}
