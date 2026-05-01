package proto

// ProbeCheck is the server-to-probe check payload. It intentionally excludes
// server-only metadata like alert destinations.
type ProbeCheck struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Type     string `json:"type"`
	Target   string `json:"target"`
	Interval int    `json:"interval"`
}
