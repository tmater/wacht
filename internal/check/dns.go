package check

import (
	"fmt"
	"log"
	"net"
	"time"

	"github.com/tmater/wacht/internal/proto"
)

// DNS resolves target as a hostname and returns a CheckResult.
// target should be a bare hostname, e.g. "example.com".
func DNS(checkID, probeID, target string) proto.CheckResult {
	log.Printf("running DNS check: check_id=%s target=%s", checkID, target)

	start := time.Now()
	addrs, err := net.LookupHost(target)
	latency := time.Since(start)

	result := proto.CheckResult{
		CheckID:   checkID,
		ProbeID:   probeID,
		Type:      proto.CheckDNS,
		Target:    target,
		Timestamp: time.Now(),
		Latency:   latency,
	}

	if err != nil {
		result.Up = false
		result.Error = err.Error()
		log.Printf("DNS check failed: check_id=%s error=%s", checkID, err)
		return result
	}

	if len(addrs) == 0 {
		result.Up = false
		result.Error = "no addresses resolved"
		log.Printf("DNS check failed: check_id=%s error=no addresses resolved", checkID)
		return result
	}

	result.Up = true
	log.Printf("DNS check done: check_id=%s up=true addrs=%d latency=%s", checkID, len(addrs), latency)
	return result
}

// DNSExpect resolves target and checks that expectedAddr appears in the results.
func DNSExpect(checkID, probeID, target, expectedAddr string) proto.CheckResult {
	result := DNS(checkID, probeID, target)
	if !result.Up {
		return result
	}

	addrs, _ := net.LookupHost(target)
	for _, a := range addrs {
		if a == expectedAddr {
			return result
		}
	}

	result.Up = false
	result.Error = fmt.Sprintf("expected address %s not found in DNS response", expectedAddr)
	log.Printf("DNS check failed: check_id=%s error=%s", checkID, result.Error)
	return result
}
