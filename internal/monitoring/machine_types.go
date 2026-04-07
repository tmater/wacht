package monitoring

// ProbeTrigger identifies a probe-level state transition event.
type ProbeTrigger string

const (
	ProbeTriggerReceiveHeartbeat ProbeTrigger = "receive_heartbeat"
	ProbeTriggerHeartbeatExpired ProbeTrigger = "heartbeat_expired"
	ProbeTriggerMarkError        ProbeTrigger = "mark_error"
)

// CheckTrigger identifies a per-(check, probe) state transition event.
type CheckTrigger string

const (
	CheckTriggerObserveUp    CheckTrigger = "observe_up"
	CheckTriggerObserveDown  CheckTrigger = "observe_down"
	CheckTriggerLoseEvidence CheckTrigger = "lose_evidence"
	CheckTriggerMarkError    CheckTrigger = "mark_error"
)

// ProbeTransition is the result of firing one probe trigger.
type ProbeTransition struct {
	From    ProbeState
	To      ProbeState
	Trigger ProbeTrigger
	Reentry bool
}

// CheckTransition is the result of firing one per-probe check trigger.
type CheckTransition struct {
	From    CheckState
	To      CheckState
	Trigger CheckTrigger
	Reentry bool
}

// QuorumTransition is the result of recomputing one check's aggregate state.
type QuorumTransition struct {
	From            QuorumState
	To              QuorumState
	LastStableState QuorumState
}

// CheckUpdate is the combined result of changing one per-probe check machine
// and recomputing the aggregate quorum for its parent check.
type CheckUpdate struct {
	CheckTransition  CheckTransition
	QuorumTransition QuorumTransition
	Quorum           CheckQuorumState
}
