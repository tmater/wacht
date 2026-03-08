package check

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/tmater/wacht/internal/network"
	"github.com/tmater/wacht/internal/proto"
)

// HTTP runs an HTTP check against the given target URL and returns a CheckResult.
func HTTP(checkID, probeID, target string, policy network.Policy) proto.CheckResult {
	log.Printf("running HTTP check: check_id=%s target=%s", checkID, target)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result := proto.CheckResult{
		CheckID:   checkID,
		ProbeID:   probeID,
		Type:      proto.CheckHTTP,
		Target:    target,
		Timestamp: time.Now().UTC(),
	}

	if _, err := network.ParseHTTPURLTarget(target); err != nil {
		result.Up = false
		result.Error = err.Error()
		log.Printf("HTTP check failed: check_id=%s error=%s", checkID, err)
		return result
	}

	client := policy.NewHTTPClient(10*time.Second, 10*time.Second, true)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		result.Up = false
		result.Error = err.Error()
		log.Printf("HTTP check failed: check_id=%s error=%s", checkID, err)
		return result
	}

	start := time.Now()
	resp, err := client.Do(req)
	result.Latency = time.Since(start)

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

	log.Printf("HTTP check done: check_id=%s status=%d up=%v latency=%s", checkID, resp.StatusCode, result.Up, result.Latency)
	return result
}
