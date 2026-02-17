package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Checks []Check `yaml:"checks"`
}

type Check struct {
	ID      string `yaml:"id"`
	Type    string `yaml:"type"`
	Target  string `yaml:"target"`
	Webhook string `yaml:"webhook"`
}

// Load reads and parses a wacht.yaml config file.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	if len(cfg.Checks) == 0 {
		return nil, fmt.Errorf("config: no checks defined")
	}

	return &cfg, nil
}
