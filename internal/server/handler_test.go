package server

import (
	"testing"
	"time"

	"github.com/tmater/wacht/internal/config"
)

func TestHandlerProbeOfflineAfterDefaults(t *testing.T) {
	h := &Handler{}

	if got := h.probeOfflineAfter(); got != config.DefaultProbeOfflineAfter {
		t.Fatalf("probeOfflineAfter() = %s, want %s", got, config.DefaultProbeOfflineAfter)
	}
}

func TestHandlerProbeOfflineAfterUsesConfig(t *testing.T) {
	h := &Handler{config: &config.ServerConfig{ProbeOfflineAfter: 8 * time.Second}}

	if got := h.probeOfflineAfter(); got != 8*time.Second {
		t.Fatalf("probeOfflineAfter() = %s, want 8s", got)
	}
}

func TestProbeOnlineUsesDefaultThreshold(t *testing.T) {
	fresh := time.Now().Add(-89 * time.Second)
	if !probeOnline(&fresh, 0) {
		t.Fatal("expected probe to stay online inside the default threshold")
	}

	stale := time.Now().Add(-91 * time.Second)
	if probeOnline(&stale, 0) {
		t.Fatal("expected probe to go offline once it crosses the default threshold")
	}
}

func TestProbeOnlineUsesConfiguredThreshold(t *testing.T) {
	fresh := time.Now().Add(-7 * time.Second)
	if !probeOnline(&fresh, 8*time.Second) {
		t.Fatal("expected probe to stay online inside the configured threshold")
	}

	stale := time.Now().Add(-9 * time.Second)
	if probeOnline(&stale, 8*time.Second) {
		t.Fatal("expected probe to go offline once it crosses the configured threshold")
	}
}
