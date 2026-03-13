package checks

import (
	"context"
	"fmt"
	"strings"

	"github.com/tmater/wacht/internal/network"
)

const (
	DefaultInterval = 30
	MaxInterval     = 86400
)

// Type identifies what kind of check should be executed.
type Type string

const (
	CheckHTTP Type = "http"
	CheckTCP  Type = "tcp"
	CheckDNS  Type = "dns"
)

// Check is the canonical definition of a monitored check after normalization.
type Check struct {
	ID       string `json:"id" yaml:"id"`
	Type     Type   `json:"type" yaml:"type"`
	Target   string `json:"target" yaml:"target"`
	Webhook  string `json:"webhook" yaml:"webhook"`
	Interval int    `json:"interval" yaml:"interval"`
}

func NewCheck(id, checkType, target, webhook string, interval int) Check {
	return Check{
		ID:       id,
		Type:     Type(checkType),
		Target:   target,
		Webhook:  webhook,
		Interval: interval,
	}
}

// Normalize trims user input, canonicalizes the type, and applies defaults.
func (c Check) Normalize() Check {
	c.ID = strings.TrimSpace(c.ID)
	c.Type = Type(network.NormalizeCheckType(string(c.Type)))
	c.Target = strings.TrimSpace(c.Target)
	c.Webhook = strings.TrimSpace(c.Webhook)
	if c.Interval == 0 {
		c.Interval = DefaultInterval
	}
	return c
}

// NormalizeAndValidate returns the canonical form of the check or an error when
// the definition is invalid under the given outbound target policy.
func (c Check) NormalizeAndValidate(ctx context.Context, policy network.Policy, requireID bool) (Check, error) {
	c = c.Normalize()

	if requireID && c.ID == "" {
		return Check{}, fmt.Errorf("id is required")
	}
	if c.Target == "" {
		return Check{}, fmt.Errorf("target is required")
	}
	if c.Interval < 1 || c.Interval > MaxInterval {
		return Check{}, fmt.Errorf("interval must be between 0 and 86400 seconds")
	}
	if err := network.ValidateWebhookURL(c.Webhook, policy); err != nil {
		return Check{}, err
	}
	if err := network.ValidateCheckTarget(ctx, string(c.Type), c.Target, policy); err != nil {
		return Check{}, err
	}
	return c, nil
}
