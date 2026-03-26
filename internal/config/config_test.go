package config

import (
	"net/netip"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadServer_ParsesAllowPrivateTargets(t *testing.T) {
	path := filepath.Join(t.TempDir(), "server.yaml")
	data := []byte("allow_private_targets: true\nprobes:\n  - id: probe-1\n    secret: s3cr3t\n")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg, err := LoadServer(path)
	if err != nil {
		t.Fatalf("LoadServer: %v", err)
	}
	if !cfg.AllowPrivateTargets {
		t.Fatal("expected allow_private_targets to be true")
	}
}

func TestLoadProbe_ParsesAllowPrivateTargets(t *testing.T) {
	path := filepath.Join(t.TempDir(), "probe.yaml")
	data := []byte("secret: s3cr3t\nserver: http://server:8080\nprobe_id: probe-1\nallow_private_targets: true\n")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg, err := LoadProbe(path)
	if err != nil {
		t.Fatalf("LoadProbe: %v", err)
	}
	if !cfg.AllowPrivateTargets {
		t.Fatal("expected allow_private_targets to be true")
	}
}

func TestLoadServer_ParsesAuthRateLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "server.yaml")
	data := []byte("auth_rate_limit:\n  requests: 42\n  window: 2m\nprobes:\n  - id: probe-1\n    secret: s3cr3t\n")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg, err := LoadServer(path)
	if err != nil {
		t.Fatalf("LoadServer: %v", err)
	}
	if cfg.AuthRateLimit.Requests != 42 {
		t.Fatalf("AuthRateLimit.Requests = %d, want 42", cfg.AuthRateLimit.Requests)
	}
	if cfg.AuthRateLimit.Window != 2*time.Minute {
		t.Fatalf("AuthRateLimit.Window = %s, want 2m", cfg.AuthRateLimit.Window)
	}
}

func TestLoadServer_DefaultsAuthRateLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "server.yaml")
	data := []byte("probes:\n  - id: probe-1\n    secret: s3cr3t\n")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg, err := LoadServer(path)
	if err != nil {
		t.Fatalf("LoadServer: %v", err)
	}
	if cfg.AuthRateLimit.Requests != DefaultAuthRateLimitRequests {
		t.Fatalf("AuthRateLimit.Requests = %d, want %d", cfg.AuthRateLimit.Requests, DefaultAuthRateLimitRequests)
	}
	if cfg.AuthRateLimit.Window != DefaultAuthRateLimitWindow {
		t.Fatalf("AuthRateLimit.Window = %s, want %s", cfg.AuthRateLimit.Window, DefaultAuthRateLimitWindow)
	}
}

func TestLoadServer_DefaultsTrustedProxies(t *testing.T) {
	path := filepath.Join(t.TempDir(), "server.yaml")
	data := []byte("probes:\n  - id: probe-1\n    secret: s3cr3t\n")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg, err := LoadServer(path)
	if err != nil {
		t.Fatalf("LoadServer: %v", err)
	}
	if len(cfg.TrustedProxies) != len(DefaultTrustedProxies) {
		t.Fatalf("TrustedProxies len = %d, want %d", len(cfg.TrustedProxies), len(DefaultTrustedProxies))
	}
	if len(cfg.TrustedProxyCIDRs) != len(DefaultTrustedProxies) {
		t.Fatalf("TrustedProxyCIDRs len = %d, want %d", len(cfg.TrustedProxyCIDRs), len(DefaultTrustedProxies))
	}
	if !cfg.TrustedProxyCIDRs[0].Contains(netip.MustParseAddr("127.0.0.1")) {
		t.Fatal("expected loopback proxy CIDR to be trusted by default")
	}
	if len(cfg.TrustedProxyCIDRs) != 2 {
		t.Fatalf("TrustedProxyCIDRs len = %d, want 2 loopback defaults", len(cfg.TrustedProxyCIDRs))
	}
	if cfg.TrustedProxyCIDRs[1].Contains(netip.MustParseAddr("fc00::1")) {
		t.Fatal("did not expect ULA addresses to be trusted by default")
	}
}

func TestLoadServer_ParsesTrustedProxies(t *testing.T) {
	path := filepath.Join(t.TempDir(), "server.yaml")
	data := []byte("trusted_proxies:\n  - 203.0.113.0/24\nprobes:\n  - id: probe-1\n    secret: s3cr3t\n")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg, err := LoadServer(path)
	if err != nil {
		t.Fatalf("LoadServer: %v", err)
	}
	if len(cfg.TrustedProxyCIDRs) != 1 {
		t.Fatalf("TrustedProxyCIDRs len = %d, want 1", len(cfg.TrustedProxyCIDRs))
	}
	if !cfg.TrustedProxyCIDRs[0].Contains(netip.MustParseAddr("203.0.113.15")) {
		t.Fatal("expected custom trusted proxy CIDR to be parsed")
	}
}

func TestLoadServer_AllowsEmptyTrustedProxies(t *testing.T) {
	path := filepath.Join(t.TempDir(), "server.yaml")
	data := []byte("trusted_proxies: []\nprobes:\n  - id: probe-1\n    secret: s3cr3t\n")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg, err := LoadServer(path)
	if err != nil {
		t.Fatalf("LoadServer: %v", err)
	}
	if len(cfg.TrustedProxies) != 0 {
		t.Fatalf("TrustedProxies len = %d, want 0", len(cfg.TrustedProxies))
	}
	if len(cfg.TrustedProxyCIDRs) != 0 {
		t.Fatalf("TrustedProxyCIDRs len = %d, want 0", len(cfg.TrustedProxyCIDRs))
	}
}

func TestLoadServer_RejectsInvalidTrustedProxyCIDR(t *testing.T) {
	path := filepath.Join(t.TempDir(), "server.yaml")
	data := []byte("trusted_proxies:\n  - not-a-cidr\nprobes:\n  - id: probe-1\n    secret: s3cr3t\n")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if _, err := LoadServer(path); err == nil {
		t.Fatal("LoadServer() error = nil, want invalid trusted proxy CIDR")
	}
}

func TestLoadServer_DefaultsProbeOfflineAfter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "server.yaml")
	data := []byte("probes:\n  - id: probe-1\n    secret: s3cr3t\n")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg, err := LoadServer(path)
	if err != nil {
		t.Fatalf("LoadServer: %v", err)
	}
	if cfg.ProbeOfflineAfter != DefaultProbeOfflineAfter {
		t.Fatalf("ProbeOfflineAfter = %s, want %s", cfg.ProbeOfflineAfter, DefaultProbeOfflineAfter)
	}
}

func TestLoadServer_ParsesProbeOfflineAfter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "server.yaml")
	data := []byte("probe_offline_after: 8s\nprobes:\n  - id: probe-1\n    secret: s3cr3t\n")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg, err := LoadServer(path)
	if err != nil {
		t.Fatalf("LoadServer: %v", err)
	}
	if cfg.ProbeOfflineAfter != 8*time.Second {
		t.Fatalf("ProbeOfflineAfter = %s, want 8s", cfg.ProbeOfflineAfter)
	}
}
