package alert

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// AlertPayload is the JSON body sent to a webhook URL on a state transition.
type AlertPayload struct {
	CheckID     string `json:"check_id"`
	Target      string `json:"target"`
	Status      string `json:"status"` // "down" or "up"
	ProbesDown  int    `json:"probes_down"`
	ProbesTotal int    `json:"probes_total"`
}

const webhookTimeout = 5 * time.Second

var webhookClient = newWebhookClient()

func newWebhookClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	dialer := &net.Dialer{
		Timeout:   3 * time.Second,
		KeepAlive: 30 * time.Second,
	}
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		if err := validateWebhookHost(ctx, host); err != nil {
			return nil, err
		}
		return dialer.DialContext(ctx, network, address)
	}
	return &http.Client{
		Timeout:   webhookTimeout,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// ValidateWebhookURL reports whether rawURL is a syntactically valid webhook
// target. Network-level restrictions are enforced again at request time.
func ValidateWebhookURL(rawURL string) error {
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
	if strings.EqualFold(host, "localhost") {
		return fmt.Errorf("webhook: localhost is not allowed")
	}
	if ip := net.ParseIP(host); ip != nil && isBlockedWebhookIP(ip) {
		return fmt.Errorf("webhook: destination %s is not allowed", host)
	}
	return nil
}

func validateWebhookHost(ctx context.Context, host string) error {
	if strings.EqualFold(host, "localhost") {
		return fmt.Errorf("webhook: localhost is not allowed")
	}
	if ip := net.ParseIP(host); ip != nil {
		if isBlockedWebhookIP(ip) {
			return fmt.Errorf("webhook: destination %s is not allowed", host)
		}
		return nil
	}

	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return fmt.Errorf("webhook: resolve host %q: %w", host, err)
	}
	if len(ips) == 0 {
		return fmt.Errorf("webhook: no addresses resolved for %q", host)
	}
	for _, ip := range ips {
		if isBlockedWebhookIP(ip.IP) {
			return fmt.Errorf("webhook: destination %s resolved to disallowed address %s", host, ip.IP.String())
		}
	}
	return nil
}

func isBlockedWebhookIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	return ip.IsLoopback() ||
		ip.IsPrivate() ||
		ip.IsMulticast() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() ||
		ip.IsUnspecified()
}

// Fire POSTs payload as JSON to url. Returns an error if the request fails or
// the server responds with a non-2xx status.
func Fire(url string, payload AlertPayload) error {
	if err := ValidateWebhookURL(url); err != nil {
		return err
	}
	return fireWithClient(webhookClient, url, payload)
}

func fireWithClient(client *http.Client, url string, payload AlertPayload) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhook: unexpected status %d from %s", resp.StatusCode, url)
	}
	return nil
}
