package proto

import "time"

// CheckResult is what a probe sends to the server after running a check.
type CheckResult struct {
	CheckID   string        `json:"check_id"` // which check this result belongs to
	ProbeID   string        `json:"probe_id"` // which probe ran it
	Type      string        `json:"type"`
	Target    string        `json:"target"` // e.g. "https://example.com" or "example.com:443"
	Up        bool          `json:"up"`
	Latency   time.Duration `json:"latency_ms"` // in milliseconds
	Error     string        `json:"error,omitempty"`
	Timestamp time.Time     `json:"timestamp"`
}
