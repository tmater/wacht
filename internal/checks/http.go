package checks

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/tmater/wacht/internal/logx"
	"github.com/tmater/wacht/internal/network"
	"github.com/tmater/wacht/internal/proto"
)

// HTTP runs an HTTP check against the given target URL and returns a CheckResult.
func HTTP(checkID, probeID, target string, policy network.Policy) proto.CheckResult {
	slog.Default().Debug("http check started", "component", "check_http", "check_id", checkID, "probe_id", probeID, "target_host", logx.TargetHost(target))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result := proto.CheckResult{
		CheckID:   checkID,
		ProbeID:   probeID,
		Type:      string(CheckHTTP),
		Target:    target,
		Timestamp: time.Now().UTC(),
	}

	if _, err := network.ParseHTTPURLTarget(target); err != nil {
		result.Up = false
		result.Error = err.Error()
		slog.Default().Warn("http check failed", "component", "check_http", "check_id", checkID, "probe_id", probeID, "target_host", logx.TargetHost(target), "err", err)
		return result
	}

	client := policy.NewHTTPClient(10*time.Second, 10*time.Second, true)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		result.Up = false
		result.Error = err.Error()
		slog.Default().Warn("http check failed", "component", "check_http", "check_id", checkID, "probe_id", probeID, "target_host", logx.TargetHost(target), "err", err)
		return result
	}

	start := time.Now()
	resp, err := client.Do(req)
	result.Latency = time.Since(start)

	if err != nil {
		result.Up = false
		result.Error = err.Error()
		slog.Default().Warn("http check failed", "component", "check_http", "check_id", checkID, "probe_id", probeID, "target_host", logx.TargetHost(target), "err", err)
		return result
	}
	defer resp.Body.Close()

	result.Up = resp.StatusCode >= 200 && resp.StatusCode < 400
	if !result.Up {
		result.Error = fmt.Sprintf("unexpected status code: %d", resp.StatusCode)
	}

	slog.Default().Debug("http check finished", "component", "check_http", "check_id", checkID, "probe_id", probeID, "target_host", logx.TargetHost(target), "status_code", resp.StatusCode, "up", result.Up, "latency_ms", result.Latency.Milliseconds())
	return result
}
