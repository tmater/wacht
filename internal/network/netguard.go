package network

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Policy controls which network destinations outbound calls may reach.
type Policy struct {
	AllowPrivateTargets bool
}

// ValidateLiteralHost checks host-level restrictions without doing DNS lookups.
func (p Policy) ValidateLiteralHost(host string) error {
	host = strings.TrimSpace(host)
	if host == "" {
		return fmt.Errorf("host is required")
	}
	if strings.EqualFold(host, "localhost") && !p.AllowPrivateTargets {
		return fmt.Errorf("localhost is not allowed")
	}
	if ip := net.ParseIP(host); ip != nil {
		return p.ValidateIP(ip)
	}
	return nil
}

// ValidateIP reports whether ip is allowed by policy.
func (p Policy) ValidateIP(ip net.IP) error {
	if ip == nil {
		return fmt.Errorf("destination is not allowed")
	}
	if ip.IsUnspecified() || ip.IsMulticast() {
		return fmt.Errorf("destination %s is not allowed", ip.String())
	}
	if p.AllowPrivateTargets {
		return nil
	}
	if ip.IsLoopback() ||
		ip.IsPrivate() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() {
		return fmt.Errorf("destination %s is not allowed", ip.String())
	}
	return nil
}

// ResolveHost resolves host and validates every returned address.
func (p Policy) ResolveHost(ctx context.Context, host string) ([]net.IPAddr, error) {
	host = strings.TrimSpace(host)
	if err := p.ValidateLiteralHost(host); err != nil {
		return nil, err
	}
	if ip := net.ParseIP(host); ip != nil {
		return []net.IPAddr{{IP: ip}}, nil
	}

	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("resolve host %q: %w", host, err)
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("no addresses resolved for %q", host)
	}
	for _, ip := range ips {
		if err := p.ValidateIP(ip.IP); err != nil {
			return nil, fmt.Errorf("destination %q resolved to disallowed address %s", host, ip.IP.String())
		}
	}
	return ips, nil
}

// ValidateHost resolves host and rejects disallowed destinations.
func (p Policy) ValidateHost(ctx context.Context, host string) error {
	_, err := p.ResolveHost(ctx, host)
	return err
}

// ValidateHostPort validates address syntax and rejects disallowed hosts.
func (p Policy) ValidateHostPort(ctx context.Context, address string) error {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("invalid host:port %q: %w", address, err)
	}
	if host == "" {
		return fmt.Errorf("host is required")
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return fmt.Errorf("invalid port %q", port)
	}
	return p.ValidateHost(ctx, host)
}

// DialContext validates address and dials a resolved IP directly.
func (p Policy) DialContext(ctx context.Context, networkName, address string, timeout time.Duration) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, fmt.Errorf("invalid host:port %q: %w", address, err)
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return nil, fmt.Errorf("invalid port %q", port)
	}

	ips, err := p.ResolveHost(ctx, host)
	if err != nil {
		return nil, err
	}

	dialer := &net.Dialer{
		Timeout:   timeout,
		KeepAlive: 30 * time.Second,
	}
	targetPort := strconv.Itoa(n)

	var lastErr error
	for _, ip := range ips {
		conn, err := dialer.DialContext(ctx, networkName, net.JoinHostPort(ip.IP.String(), targetPort))
		if err == nil {
			return conn, nil
		}
		lastErr = err
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("no addresses resolved for %q", host)
}

// NewHTTPClient returns an HTTP client that validates outbound destinations
// before dialing them.
func (p Policy) NewHTTPClient(timeout, dialTimeout time.Duration, followRedirects bool) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = func(ctx context.Context, networkName, address string) (net.Conn, error) {
		return p.DialContext(ctx, networkName, address, dialTimeout)
	}

	client := &http.Client{
		Timeout:   timeout,
		Transport: transport,
	}
	if !followRedirects {
		client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		}
	}
	return client
}
