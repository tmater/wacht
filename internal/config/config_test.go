package config

import (
	"os"
	"path/filepath"
	"testing"
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
