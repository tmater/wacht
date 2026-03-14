package config

import (
	"fmt"
	"os"
	"time"

	"github.com/tmater/wacht/internal/checks"
	"gopkg.in/yaml.v3"
)

const (
	DefaultRetentionDays          = 30
	DefaultAuthRateLimitRequests  = 10
	DefaultAuthRateLimitWindow    = time.Minute
	DefaultProbeOfflineAfter      = 90 * time.Second
	DefaultProbeHeartbeatInterval = 30 * time.Second
)

type ServerConfig struct {
	Probes              []ProbeAuth    `yaml:"probes"`
	Checks              []checks.Check `yaml:"checks"`
	SeedUser            SeedUser       `yaml:"seed_user"`
	RetentionDays       int            `yaml:"retention_days"`        // 0 → default 30
	AllowPrivateTargets bool           `yaml:"allow_private_targets"` // false by default
	AuthRateLimit       RateLimit      `yaml:"auth_rate_limit"`
	ProbeOfflineAfter   time.Duration  `yaml:"probe_offline_after"`
}

type SeedUser struct {
	Email    string `yaml:"email"`
	Password string `yaml:"password"`
}

type ProbeAuth struct {
	ID     string `yaml:"id"`
	Secret string `yaml:"secret"`
}

type RateLimit struct {
	Requests int           `yaml:"requests"`
	Window   time.Duration `yaml:"window"`
}

type ProbeConfig struct {
	Secret              string        `yaml:"secret"`
	Server              string        `yaml:"server"`
	ProbeID             string        `yaml:"probe_id"`
	HeartbeatInterval   time.Duration `yaml:"heartbeat_interval"`
	AllowPrivateTargets bool          `yaml:"allow_private_targets"` // false by default
}

// LoadServer reads and parses a server.yaml config file.
func LoadServer(path string) (*ServerConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	var cfg ServerConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	seen := make(map[string]struct{}, len(cfg.Probes))
	for i, probe := range cfg.Probes {
		if probe.ID == "" {
			return nil, fmt.Errorf("config: probes[%d].id is required", i)
		}
		if probe.Secret == "" {
			return nil, fmt.Errorf("config: probes[%d].secret is required", i)
		}
		if _, ok := seen[probe.ID]; ok {
			return nil, fmt.Errorf("config: duplicate probe id %q", probe.ID)
		}
		seen[probe.ID] = struct{}{}
	}
	if cfg.ProbeOfflineAfter <= 0 {
		cfg.ProbeOfflineAfter = DefaultProbeOfflineAfter
	}
	if cfg.AuthRateLimit.Requests <= 0 {
		cfg.AuthRateLimit.Requests = DefaultAuthRateLimitRequests
	}
	if cfg.AuthRateLimit.Window <= 0 {
		cfg.AuthRateLimit.Window = DefaultAuthRateLimitWindow
	}
	return &cfg, nil
}

// LoadProbe reads and parses a probe.yaml config file.
func LoadProbe(path string) (*ProbeConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	var cfg ProbeConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	if cfg.Secret == "" {
		return nil, fmt.Errorf("config: secret is required")
	}
	if cfg.Server == "" {
		return nil, fmt.Errorf("config: server is required")
	}
	if cfg.ProbeID == "" {
		return nil, fmt.Errorf("config: probe_id is required")
	}
	if cfg.HeartbeatInterval == 0 {
		cfg.HeartbeatInterval = DefaultProbeHeartbeatInterval
	}

	return &cfg, nil
}
