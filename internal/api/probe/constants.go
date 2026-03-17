package probe

const (
	// HeaderProbeID identifies the authenticated probe making the request.
	HeaderProbeID = "X-Wacht-Probe-ID"
	// HeaderProbeSecret carries the shared secret for probe authentication.
	HeaderProbeSecret = "X-Wacht-Probe-Secret"

	// PathRegister records probe startup against the server.
	PathRegister = "/api/probes/register"
	// PathChecks returns the current probe-visible check set.
	PathChecks = "/api/probes/checks"
	// PathHeartbeat refreshes the probe's last-seen timestamp.
	PathHeartbeat = "/api/probes/heartbeat"
	// PathResults accepts executed check results from probes.
	PathResults = "/api/results"
)
