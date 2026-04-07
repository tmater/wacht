package monitoring

import (
	"context"
	"time"

	"github.com/qmuntal/stateless"
)

// CheckMachine owns the per-(check, probe) runtime state and transitions.
type CheckMachine struct {
	state CheckExecState
	sm    *stateless.StateMachine
}

// NewCheckMachine creates a check state machine for one assigned (check, probe)
// pair.
func NewCheckMachine(checkID, probeID string) *CheckMachine {
	m := &CheckMachine{
		state: newCheckExecState(checkID, probeID),
	}
	m.sm = newCheckStateMachine(m)
	return m
}

// Snapshot returns the current per-(check, probe) runtime state.
func (m *CheckMachine) Snapshot() CheckExecState {
	return m.state
}

// ObserveUp applies a fresh successful result for this (check, probe) pair.
func (m *CheckMachine) ObserveUp(at time.Time, expiresAt *time.Time) (CheckTransition, error) {
	transition, err := m.fire(CheckTriggerObserveUp)
	if err != nil {
		return CheckTransition{}, err
	}

	m.recordObservation(true, at, expiresAt, "")
	return transition, nil
}

// ObserveDown applies a fresh failing result for this (check, probe) pair.
func (m *CheckMachine) ObserveDown(at time.Time, expiresAt *time.Time, message string) (CheckTransition, error) {
	transition, err := m.fire(CheckTriggerObserveDown)
	if err != nil {
		return CheckTransition{}, err
	}

	m.recordObservation(false, at, expiresAt, message)
	return transition, nil
}

// LoseEvidence marks the check state missing because its evidence is no longer
// fresh enough to count.
func (m *CheckMachine) LoseEvidence() (CheckTransition, error) {
	transition, err := m.fire(CheckTriggerLoseEvidence)
	if err != nil {
		return CheckTransition{}, err
	}

	m.state.ExpiresAt = nil
	m.state.LastError = ""
	return transition, nil
}

// MarkError marks the check state as unusable fresh evidence.
func (m *CheckMachine) MarkError(message string) (CheckTransition, error) {
	transition, err := m.fire(CheckTriggerMarkError)
	if err != nil {
		return CheckTransition{}, err
	}

	m.state.LastError = message
	return transition, nil
}

// fire sends one trigger through the check state machine and returns the
// resulting transition summary.
func (m *CheckMachine) fire(trigger CheckTrigger) (CheckTransition, error) {
	from := m.state.State
	if err := m.sm.Fire(trigger); err != nil {
		return CheckTransition{}, err
	}

	to := m.state.State
	return CheckTransition{
		From:    from,
		To:      to,
		Trigger: trigger,
		Reentry: from == to,
	}, nil
}

// recordObservation updates the stored execution metadata after a fresh probe
// result has been accepted.
func (m *CheckMachine) recordObservation(up bool, at time.Time, expiresAt *time.Time, message string) {
	observedAt := at.UTC()
	m.state.LastResultAt = &observedAt
	m.state.LastError = message

	if expiresAt == nil {
		m.state.ExpiresAt = nil
	} else {
		expiry := expiresAt.UTC()
		m.state.ExpiresAt = &expiry
	}

	if m.state.LastOutcomeUp != nil && *m.state.LastOutcomeUp == up {
		m.state.StreakLen++
	} else {
		m.state.StreakLen = 1
	}

	lastOutcomeUp := up
	m.state.LastOutcomeUp = &lastOutcomeUp
}

// newCheckStateMachine configures the stateless machine around the per-probe
// check state owned by the given check machine.
func newCheckStateMachine(owner *CheckMachine) *stateless.StateMachine {
	sm := stateless.NewStateMachineWithExternalStorage(
		func(context.Context) (stateless.State, error) {
			return owner.state.State, nil
		},
		func(_ context.Context, state stateless.State) error {
			owner.state.State = state.(CheckState)
			return nil
		},
		stateless.FiringImmediate,
	)

	sm.Configure(CheckStateMissing).
		Permit(CheckTriggerObserveUp, CheckStateUp).
		Permit(CheckTriggerObserveDown, CheckStateDown).
		Permit(CheckTriggerMarkError, CheckStateError).
		PermitReentry(CheckTriggerLoseEvidence)

	sm.Configure(CheckStateUp).
		PermitReentry(CheckTriggerObserveUp).
		Permit(CheckTriggerObserveDown, CheckStateDown).
		Permit(CheckTriggerLoseEvidence, CheckStateMissing).
		Permit(CheckTriggerMarkError, CheckStateError)

	sm.Configure(CheckStateDown).
		Permit(CheckTriggerObserveUp, CheckStateUp).
		PermitReentry(CheckTriggerObserveDown).
		Permit(CheckTriggerLoseEvidence, CheckStateMissing).
		Permit(CheckTriggerMarkError, CheckStateError)

	sm.Configure(CheckStateError).
		Permit(CheckTriggerObserveUp, CheckStateUp).
		Permit(CheckTriggerObserveDown, CheckStateDown).
		Permit(CheckTriggerLoseEvidence, CheckStateMissing).
		PermitReentry(CheckTriggerMarkError)

	return sm
}
