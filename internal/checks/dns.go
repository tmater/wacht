package checks

import (
	"context"
	"fmt"
	"log"
	"net"
	"strings"
	"time"

	"github.com/tmater/wacht/internal/network"
	"github.com/tmater/wacht/internal/proto"
)

// DNS resolves target as a hostname and returns a CheckResult.
// target should be a bare hostname, e.g. "example.com".
func DNS(checkID, probeID, target string, policy network.Policy) proto.CheckResult {
	log.Printf("running DNS check: check_id=%s target=%s", checkID, target)

	result := proto.CheckResult{
		CheckID:   checkID,
		ProbeID:   probeID,
		Type:      string(CheckDNS),
		Target:    target,
		Timestamp: time.Now().UTC(),
	}

	host, err := network.ParseDNSHostnameTarget(target)
	if err != nil {
		result.Up = false
		result.Error = err.Error()
		log.Printf("DNS check failed: check_id=%s error=%s", checkID, err)
		return result
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	start := time.Now()
	addrs, err := lookupDNSHost(ctx, host, policy)
	result.Latency = time.Since(start)

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
	log.Printf("DNS check done: check_id=%s up=true addrs=%d latency=%s", checkID, len(addrs), result.Latency)
	return result
}

// DNSExpect resolves target and checks that expectedAddr appears in the results.
func DNSExpect(checkID, probeID, target, expectedAddr string, policy network.Policy) proto.CheckResult {
	result := DNS(checkID, probeID, target, policy)
	if !result.Up {
		return result
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	host, err := network.ParseDNSHostnameTarget(target)
	if err != nil {
		result.Up = false
		result.Error = err.Error()
		return result
	}

	addrs, err := lookupDNSHost(ctx, host, policy)
	if err != nil {
		result.Up = false
		result.Error = err.Error()
		return result
	}
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

func lookupDNSHost(ctx context.Context, host string, policy network.Policy) ([]string, error) {
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, strings.TrimSuffix(host, "."))
	if err != nil {
		return nil, err
	}
	addrs := make([]string, 0, len(ips))
	for _, ip := range ips {
		if err := policy.ValidateIP(ip.IP); err != nil {
			return nil, err
		}
		addrs = append(addrs, ip.IP.String())
	}
	return addrs, nil
}
