package check

import (
	"log"
	"net"
	"time"

	"github.com/tmater/wacht/internal/proto"
)

// TCP attempts to open a TCP connection to target (host:port) and returns a CheckResult.
func TCP(checkID, probeID, target string) proto.CheckResult {
	log.Printf("running TCP check: check_id=%s target=%s", checkID, target)

	start := time.Now()
	conn, err := net.DialTimeout("tcp", target, 10*time.Second)
	latency := time.Since(start)

	result := proto.CheckResult{
		CheckID:   checkID,
		ProbeID:   probeID,
		Type:      proto.CheckTCP,
		Target:    target,
		Timestamp: time.Now(),
		Latency:   latency,
	}

	if err != nil {
		result.Up = false
		result.Error = err.Error()
		log.Printf("TCP check failed: check_id=%s error=%s", checkID, err)
		return result
	}
	conn.Close()

	result.Up = true
	log.Printf("TCP check done: check_id=%s up=true latency=%s", checkID, latency)
	return result
}
