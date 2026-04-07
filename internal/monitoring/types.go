package monitoring

import "time"

// ProbeState describes whether a probe is currently usable for monitoring.
type ProbeState string

const (
	ProbeStateOnline  ProbeState = "online"
	ProbeStateOffline ProbeState = "offline"
	ProbeStateError   ProbeState = "error"
)

// CheckState describes the current state of one check on one probe.
type CheckState string

const (
	CheckStateUp      CheckState = "up"
	CheckStateDown    CheckState = "down"
	CheckStateMissing CheckState = "missing"
	CheckStateError   CheckState = "error"
)

// CountsAsVote reports whether the per-probe check state contributes to quorum.
func (s CheckState) CountsAsVote() bool {
	return s == CheckStateUp || s == CheckStateDown
}

// QuorumState is the aggregate state of a check.
type QuorumState string

const (
	QuorumStatePending QuorumState = "pending"
	QuorumStateUp      QuorumState = "up"
	QuorumStateDown    QuorumState = "down"
	QuorumStateError   QuorumState = "error"
)

// ProbeRuntimeState is the current runtime state of a probe.
type ProbeRuntimeState struct {
	ProbeID         string
	State           ProbeState
	LastHeartbeatAt *time.Time
	LastError       string
}

// CheckExecState stores the current runtime facts for one (check, probe) pair.
type CheckExecState struct {
	CheckID      string
	ProbeID      string
	LastResultAt time.Time
	LastOutcome  CheckState
	StreakLen    int
	ExpiresAt    time.Time
	State        CheckState
	LastError    string
}

// CheckQuorumState is the aggregate runtime state of one check.
type CheckQuorumState struct {
	CheckID         string
	State           QuorumState
	LastStableState QuorumState
	IncidentOpen    bool
}

// clone returns a detached copy of the probe runtime state.
func (s ProbeRuntimeState) clone() ProbeRuntimeState {
	if s.LastHeartbeatAt != nil {
		heartbeatAt := *s.LastHeartbeatAt
		s.LastHeartbeatAt = &heartbeatAt
	}
	return s
}

// newProbeRuntimeState builds the initial runtime state for a probe.
func newProbeRuntimeState(probeID string) ProbeRuntimeState {
	return ProbeRuntimeState{
		ProbeID: probeID,
		State:   ProbeStateOffline,
	}
}

// newCheckExecState builds the initial runtime state for one (check, probe)
// pair.
func newCheckExecState(checkID, probeID string) CheckExecState {
	return CheckExecState{
		CheckID: checkID,
		ProbeID: probeID,
		State:   CheckStateMissing,
	}
}

// newCheckQuorumState builds the initial aggregate runtime state for a check.
func newCheckQuorumState(checkID string) CheckQuorumState {
	return CheckQuorumState{
		CheckID: checkID,
		State:   QuorumStatePending,
	}
}
