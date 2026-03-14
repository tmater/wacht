package config

import (
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
