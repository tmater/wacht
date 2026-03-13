package main

import (
	"log"
	"net/http"
	"time"
)

// heartbeatLoop only reports probe liveness; config sync and scheduling live
// elsewhere so a heartbeat cannot accidentally trigger extra check runs.
func heartbeatLoop(serverURL, secret, probeID string, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for range ticker.C {
		req, err := http.NewRequest("POST", serverURL+"/api/probes/heartbeat", nil)
		if err != nil {
			log.Printf("probe: heartbeat error: %s", err)
			continue
		}
		setProbeHeaders(req, probeID, secret)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			log.Printf("probe: heartbeat error: %s", err)
			continue
		}
		resp.Body.Close()
		log.Printf("probe: heartbeat sent probe_id=%s status=%d", probeID, resp.StatusCode)
	}
}
