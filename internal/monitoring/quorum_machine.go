package monitoring

import "time"

// QuorumMachine owns one check's aggregate state.
type QuorumMachine struct {
	state  CheckQuorumState
	checks map[string]*CheckMachine
}

// NewQuorumMachine creates a quorum machine for one check.
func NewQuorumMachine(checkID string, probeIDs []string) *QuorumMachine {
	probeIDs = uniqueIDs(probeIDs)

	m := &QuorumMachine{
		state:  newCheckQuorumState(checkID),
		checks: make(map[string]*CheckMachine, len(probeIDs)),
	}

	for _, probeID := range probeIDs {
		m.checks[probeID] = NewCheckMachine(checkID, probeID)
	}

	return m
}

// Snapshot returns the current aggregate check state.
func (m *QuorumMachine) Snapshot() CheckQuorumState {
	return m.state
}

// CheckSnapshot returns the current runtime state for one child (check, probe)
// machine owned by this quorum machine.
func (m *QuorumMachine) CheckSnapshot(probeID string) (CheckExecState, bool) {
	check, ok := m.checks[probeID]
	if !ok {
		return CheckExecState{}, false
	}
	return check.Snapshot(), true
}

// ObserveUp routes a successful result to the owning child check machine and
// recomputes aggregate quorum.
func (m *QuorumMachine) ObserveUp(probeID string, at time.Time, expiresAt *time.Time) (CheckUpdate, error) {
	check, ok := m.checks[probeID]
	if !ok {
		return CheckUpdate{}, ErrUnknownCheckAssignment
	}

	checkTransition, err := check.ObserveUp(at, expiresAt)
	if err != nil {
		return CheckUpdate{}, err
	}

	return CheckUpdate{
		CheckTransition:  checkTransition,
		QuorumTransition: m.Recompute(),
		Quorum:           m.Snapshot(),
	}, nil
}

// ObserveDown routes a failing result to the owning child check machine and
// recomputes aggregate quorum.
func (m *QuorumMachine) ObserveDown(probeID string, at time.Time, expiresAt *time.Time, message string) (CheckUpdate, error) {
	check, ok := m.checks[probeID]
	if !ok {
		return CheckUpdate{}, ErrUnknownCheckAssignment
	}

	checkTransition, err := check.ObserveDown(at, expiresAt, message)
	if err != nil {
		return CheckUpdate{}, err
	}

	return CheckUpdate{
		CheckTransition:  checkTransition,
		QuorumTransition: m.Recompute(),
		Quorum:           m.Snapshot(),
	}, nil
}

// LoseEvidence routes an evidence-expiry event to the owning child check
// machine and recomputes aggregate quorum.
func (m *QuorumMachine) LoseEvidence(probeID string) (CheckUpdate, error) {
	check, ok := m.checks[probeID]
	if !ok {
		return CheckUpdate{}, ErrUnknownCheckAssignment
	}

	checkTransition, err := check.LoseEvidence()
	if err != nil {
		return CheckUpdate{}, err
	}

	return CheckUpdate{
		CheckTransition:  checkTransition,
		QuorumTransition: m.Recompute(),
		Quorum:           m.Snapshot(),
	}, nil
}

// MarkCheckError routes an unusable fresh-evidence event to the owning child
// check machine and recomputes aggregate quorum.
func (m *QuorumMachine) MarkCheckError(probeID, message string) (CheckUpdate, error) {
	check, ok := m.checks[probeID]
	if !ok {
		return CheckUpdate{}, ErrUnknownCheckAssignment
	}

	checkTransition, err := check.MarkError(message)
	if err != nil {
		return CheckUpdate{}, err
	}

	return CheckUpdate{
		CheckTransition:  checkTransition,
		QuorumTransition: m.Recompute(),
		Quorum:           m.Snapshot(),
	}, nil
}

// Recompute derives aggregate check truth from all assigned child check
// machines owned by this quorum machine.
func (m *QuorumMachine) Recompute() QuorumTransition {
	current := m.state
	next := current

	var upVotes, downVotes int
	for _, check := range m.checks {
		switch check.Snapshot().State {
		case CheckStateUp:
			upVotes++
		case CheckStateDown:
			downVotes++
		}
	}

	required := quorumThreshold(len(m.checks))

	switch {
	case len(m.checks) > 0 && upVotes >= required:
		next.State = QuorumStateUp
		next.LastStableState = QuorumStateUp
	case len(m.checks) > 0 && downVotes >= required:
		next.State = QuorumStateDown
		next.LastStableState = QuorumStateDown
	case current.LastStableState == "":
		next.State = QuorumStatePending
	default:
		next.State = QuorumStateError
	}

	m.state = next
	return QuorumTransition{
		From:            current.State,
		To:              next.State,
		LastStableState: next.LastStableState,
	}
}
