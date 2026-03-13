package network

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
)

// ValidateCheckTarget checks target syntax and rejects disallowed destinations.
func ValidateCheckTarget(ctx context.Context, checkType, target string, policy Policy) error {
	switch NormalizeCheckType(checkType) {
	case "http":
		u, err := ParseHTTPURLTarget(target)
		if err != nil {
			return err
		}
		return policy.ValidateHost(ctx, u.Hostname())
	case "tcp":
		host, _, err := ParseTCPAddressTarget(target)
		if err != nil {
			return err
		}
		return policy.ValidateHost(ctx, host)
	case "dns":
		host, err := ParseDNSHostnameTarget(target)
		if err != nil {
			return err
		}
		return policy.ValidateHost(ctx, host)
	default:
		return fmt.Errorf("unsupported check type %q", checkType)
	}
}

func NormalizeCheckType(checkType string) string {
	if strings.TrimSpace(checkType) == "" {
		return "http"
	}
	return strings.ToLower(strings.TrimSpace(checkType))
}

func ParseHTTPURLTarget(target string) (*url.URL, error) {
	u, err := url.Parse(target)
	if err != nil {
		return nil, fmt.Errorf("http target: invalid URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("http target: unsupported URL scheme %q", u.Scheme)
	}
	if u.Hostname() == "" {
		return nil, fmt.Errorf("http target: host is required")
	}
	return u, nil
}

// ValidateWebhookURL checks webhook syntax and rejects destinations disallowed
// by the provided outbound policy.
func ValidateWebhookURL(rawURL string, policy Policy) error {
	if rawURL == "" {
		return nil
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("webhook: invalid URL: %w", err)
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return fmt.Errorf("webhook: unsupported URL scheme %q", u.Scheme)
	}
	if u.Host == "" {
		return fmt.Errorf("webhook: host is required")
	}
	if u.User != nil {
		return fmt.Errorf("webhook: userinfo is not allowed")
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("webhook: host is required")
	}
	if err := policy.ValidateLiteralHost(host); err != nil {
		return fmt.Errorf("webhook: %w", err)
	}
	return nil
}

func ParseTCPAddressTarget(target string) (string, string, error) {
	host, port, err := net.SplitHostPort(target)
	if err != nil {
		return "", "", fmt.Errorf("tcp target: must be host:port: %w", err)
	}
	if host == "" {
		return "", "", fmt.Errorf("tcp target: host is required")
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return "", "", fmt.Errorf("tcp target: invalid port %q", port)
	}
	return host, port, nil
}

func ParseDNSHostnameTarget(target string) (string, error) {
	host := strings.TrimSpace(strings.TrimSuffix(target, "."))
	if host == "" {
		return "", fmt.Errorf("dns target: hostname is required")
	}
	if strings.Contains(host, "://") || strings.Contains(host, "/") {
		return "", fmt.Errorf("dns target: bare hostname required")
	}
	if strings.Contains(host, ":") {
		return "", fmt.Errorf("dns target: bare hostname required")
	}
	if net.ParseIP(host) != nil {
		return "", fmt.Errorf("dns target: hostname required")
	}
	return host, nil
}
