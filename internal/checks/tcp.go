package checks

import (
	"context"
	"log/slog"
	"time"

	"github.com/tmater/wacht/internal/logx"
	"github.com/tmater/wacht/internal/network"
	"github.com/tmater/wacht/internal/proto"
)

// TCP attempts to open a TCP connection to target (host:port) and returns a CheckResult.
func TCP(checkID, probeID, target string, policy network.Policy) proto.CheckResult {
	slog.Default().Debug("tcp check started", "component", "check_tcp", "check_id", checkID, "probe_id", probeID, "target_host", logx.TargetHost(target))

	result := proto.CheckResult{
		CheckID:   checkID,
		ProbeID:   probeID,
		Type:      string(CheckTCP),
		Target:    target,
		Timestamp: time.Now().UTC(),
	}

	if _, _, err := network.ParseTCPAddressTarget(target); err != nil {
		result.Up = false
		result.Error = err.Error()
		slog.Default().Warn("tcp check failed", "component", "check_tcp", "check_id", checkID, "probe_id", probeID, "target_host", logx.TargetHost(target), "err", err)
		return result
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	start := time.Now()
	conn, err := policy.DialContext(ctx, "tcp", target, 10*time.Second)
	result.Latency = time.Since(start)

	if err != nil {
		result.Up = false
		result.Error = err.Error()
		slog.Default().Warn("tcp check failed", "component", "check_tcp", "check_id", checkID, "probe_id", probeID, "target_host", logx.TargetHost(target), "err", err)
		return result
	}
	conn.Close()

	result.Up = true
	slog.Default().Debug("tcp check finished", "component", "check_tcp", "check_id", checkID, "probe_id", probeID, "target_host", logx.TargetHost(target), "up", true, "latency_ms", result.Latency.Milliseconds())
	return result
}
