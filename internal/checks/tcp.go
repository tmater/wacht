package checks

import (
	"context"
	"log"
	"time"

	"github.com/tmater/wacht/internal/network"
	"github.com/tmater/wacht/internal/proto"
)

// TCP attempts to open a TCP connection to target (host:port) and returns a CheckResult.
func TCP(checkID, probeID, target string, policy network.Policy) proto.CheckResult {
	log.Printf("running TCP check: check_id=%s target=%s", checkID, target)

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
		log.Printf("TCP check failed: check_id=%s error=%s", checkID, err)
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
		log.Printf("TCP check failed: check_id=%s error=%s", checkID, err)
		return result
	}
	conn.Close()

	result.Up = true
	log.Printf("TCP check done: check_id=%s up=true latency=%s", checkID, result.Latency)
	return result
}
