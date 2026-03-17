package probe

// HeartbeatRequest is the optional JSON body for probe heartbeats.
type HeartbeatRequest struct {
	ProbeID string `json:"probe_id"`
}

// RegisterRequest is the JSON body sent when a probe registers on startup.
type RegisterRequest struct {
	ProbeID string `json:"probe_id"`
	Version string `json:"version"`
}
