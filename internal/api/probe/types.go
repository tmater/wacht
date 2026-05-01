package probe

import "github.com/tmater/wacht/internal/proto"

// HeartbeatRequest is the optional JSON body for probe heartbeats.
type HeartbeatRequest struct {
	ProbeID string `json:"probe_id"`
}

// RegisterRequest is the JSON body sent when a probe registers on startup.
type RegisterRequest struct {
	ProbeID string `json:"probe_id"`
	Version string `json:"version"`
}

// ResultBatchRequest is the JSON body sent when a probe flushes one or more
// executed check results back to the server.
type ResultBatchRequest struct {
	Results []proto.CheckResult `json:"results"`
}
