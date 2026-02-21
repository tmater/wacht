package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type ServerConfig struct {
	Secret   string   `yaml:"secret"`
	Checks   []Check  `yaml:"checks"`
	SeedUser SeedUser `yaml:"seed_user"`
}

type SeedUser struct {
	Email    string `yaml:"email"`
	Password string `yaml:"password"`
}

type ProbeConfig struct {
	Secret            string        `yaml:"secret"`
	Server            string        `yaml:"server"`
	ProbeID           string        `yaml:"probe_id"`
	HeartbeatInterval time.Duration `yaml:"heartbeat_interval"`
}

type Check struct {
	ID      string `yaml:"id"`
	Type    string `yaml:"type"`
	Target  string `yaml:"target"`
	Webhook string `yaml:"webhook"`
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

	if cfg.Secret == "" {
		return nil, fmt.Errorf("config: secret is required")
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
		cfg.HeartbeatInterval = 30 * time.Second
	}

	return &cfg, nil
}
