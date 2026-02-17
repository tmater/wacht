package check

import (
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/tmater/wacht/internal/proto"
)

// HTTP runs an HTTP check against the given target URL and returns a CheckResult.
func HTTP(checkID, probeID, target string) proto.CheckResult {
	log.Printf("running HTTP check: check_id=%s target=%s", checkID, target)

	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	start := time.Now()
	resp, err := client.Get(target)
	latency := time.Since(start)

	result := proto.CheckResult{
		CheckID:   checkID,
		ProbeID:   probeID,
		Type:      proto.CheckHTTP,
		Target:    target,
		Timestamp: time.Now(),
		Latency:   latency,
	}

	if err != nil {
		result.Up = false
		result.Error = err.Error()
		log.Printf("HTTP check failed: check_id=%s error=%s", checkID, err)
		return result
	}
	defer resp.Body.Close()

	result.Up = resp.StatusCode >= 200 && resp.StatusCode < 400
	if !result.Up {
		result.Error = fmt.Sprintf("unexpected status code: %d", resp.StatusCode)
	}

	log.Printf("HTTP check done: check_id=%s status=%d up=%v latency=%s", checkID, resp.StatusCode, result.Up, latency)
	return result
}
