package config

import (
	"fmt"
	"net/netip"
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
	DefaultPassword               = "replace-with-a-strong-password"
)

var DefaultTrustedProxies = []string{
	"127.0.0.1/8",
	"::1/128",
}

type ServerConfig struct {
	Probes              []ProbeAuth    `yaml:"probes"`
	Checks              []checks.Check `yaml:"checks"`
	SeedUser            SeedUser       `yaml:"seed_user"`
	RetentionDays       int            `yaml:"retention_days"`        // 0 → default 30
	AllowPrivateTargets bool           `yaml:"allow_private_targets"` // false by default
	AuthRateLimit       RateLimit      `yaml:"auth_rate_limit"`
	TrustedProxies      []string       `yaml:"trusted_proxies"`
	ProbeOfflineAfter   time.Duration  `yaml:"probe_offline_after"`
	TrustedProxyCIDRs   []netip.Prefix `yaml:"-"`
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
		if probe.Secret == DefaultPassword {
			return nil, fmt.Errorf("config: probes[%d].secret must be changed from the shipped sample value", i)
		}
		if _, ok := seen[probe.ID]; ok {
			return nil, fmt.Errorf("config: duplicate probe id %q", probe.ID)
		}
		seen[probe.ID] = struct{}{}
	}
	if cfg.SeedUser.Password == DefaultPassword {
		return nil, fmt.Errorf("config: seed_user.password must be changed from the shipped sample value")
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
	if cfg.TrustedProxies == nil {
		cfg.TrustedProxies = append([]string(nil), DefaultTrustedProxies...)
	}
	cfg.TrustedProxyCIDRs = make([]netip.Prefix, 0, len(cfg.TrustedProxies))
	for i, cidr := range cfg.TrustedProxies {
		prefix, err := netip.ParsePrefix(cidr)
		if err != nil {
			return nil, fmt.Errorf("config: trusted_proxies[%d] invalid CIDR %q", i, cidr)
		}
		cfg.TrustedProxyCIDRs = append(cfg.TrustedProxyCIDRs, prefix.Masked())
	}
	return &cfg, nil
}

func validateProbeConfig(cfg *ProbeConfig) error {
	if cfg.Secret == "" {
		return fmt.Errorf("config: secret is required")
	}
	if cfg.Server == "" {
		return fmt.Errorf("config: server is required")
	}
	if cfg.ProbeID == "" {
		return fmt.Errorf("config: probe_id is required")
	}
	if cfg.Secret == DefaultPassword {
		return fmt.Errorf("config: secret must be changed from the shipped sample value")
	}
	if cfg.HeartbeatInterval == 0 {
		cfg.HeartbeatInterval = DefaultProbeHeartbeatInterval
	}

	return nil
}

func loadProbe(path string, overrides ProbeConfig) (*ProbeConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	var cfg ProbeConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	if overrides.Secret != "" {
		cfg.Secret = overrides.Secret
	}
	if overrides.Server != "" {
		cfg.Server = overrides.Server
	}
	if overrides.ProbeID != "" {
		cfg.ProbeID = overrides.ProbeID
	}
	if overrides.HeartbeatInterval != 0 {
		cfg.HeartbeatInterval = overrides.HeartbeatInterval
	}
	if overrides.AllowPrivateTargets {
		cfg.AllowPrivateTargets = true
	}
	if err := validateProbeConfig(&cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

// LoadProbe reads and parses a probe.yaml config file.
func LoadProbe(path string) (*ProbeConfig, error) {
	return loadProbe(path, ProbeConfig{})
}

// LoadProbeWithOverrides reads and parses a probe.yaml config file, then
// applies explicit runtime overrides before validation.
func LoadProbeWithOverrides(path string, overrides ProbeConfig) (*ProbeConfig, error) {
	return loadProbe(path, overrides)
}
