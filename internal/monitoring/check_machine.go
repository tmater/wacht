package monitoring

import (
	"context"
	"time"

	"github.com/qmuntal/stateless"
)

const consecutiveEvidenceThreshold = 2

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
	return m.observe(CheckTriggerObserveUp, CheckStateUp, at, expiresAt, "")
}

// ObserveDown applies a fresh failing result for this (check, probe) pair.
func (m *CheckMachine) ObserveDown(at time.Time, expiresAt *time.Time, message string) (CheckTransition, error) {
	return m.observe(CheckTriggerObserveDown, CheckStateDown, at, expiresAt, message)
}

// LoseEvidence marks the check state missing because its evidence is no longer
// fresh enough to count.
func (m *CheckMachine) LoseEvidence() (CheckTransition, error) {
	transition, err := m.fire(CheckTriggerLoseEvidence)
	if err != nil {
		return CheckTransition{}, err
	}

	m.state.ExpiresAt = time.Time{}
	m.state.LastOutcome = ""
	m.state.StreakLen = 0
	m.state.LastError = ""
	return transition, nil
}

// MarkError marks the check state as unusable fresh evidence.
func (m *CheckMachine) MarkError(message string) (CheckTransition, error) {
	transition, err := m.fire(CheckTriggerMarkError)
	if err != nil {
		return CheckTransition{}, err
	}

	m.bumpOutcome(CheckStateError)
	m.state.LastError = message
	return transition, nil
}

// observe validates one fresh up/down observation, records it, and maps the
// raw streak metadata to the contribution state that should count toward quorum.
func (m *CheckMachine) observe(
	trigger CheckTrigger,
	outcome CheckState,
	at time.Time,
	expiresAt *time.Time,
	message string,
) (CheckTransition, error) {
	from := m.state.State
	if _, err := m.fire(trigger); err != nil {
		return CheckTransition{}, err
	}

	m.recordObservation(outcome, at, expiresAt, message)
	m.state.State = m.contributionStateFor(outcome)

	return CheckTransition{
		From:    from,
		To:      m.state.State,
		Trigger: trigger,
		Reentry: from == m.state.State,
	}, nil
}

// fire sends one trigger through the check state machine and returns the
// resulting transition summary.
func (m *CheckMachine) fire(trigger CheckTrigger) (CheckTransition, error) {
	from := m.state.State
	if err := m.sm.Fire(trigger); err != nil {
		return CheckTransition{}, err
	}

	return CheckTransition{
		From:    from,
		To:      m.state.State,
		Trigger: trigger,
		Reentry: from == m.state.State,
	}, nil
}

// recordObservation updates the stored execution metadata after a fresh probe
// result has been accepted.
func (m *CheckMachine) recordObservation(outcome CheckState, at time.Time, expiresAt *time.Time, message string) {
	observedAt := at.UTC()
	m.state.LastResultAt = observedAt
	m.state.LastError = message

	if expiresAt == nil {
		m.state.ExpiresAt = time.Time{}
	} else {
		expiry := expiresAt.UTC()
		m.state.ExpiresAt = expiry
	}

	m.bumpOutcome(outcome)
}

// bumpOutcome advances the consecutive-evidence counter for the latest raw
// per-probe execution outcome.
func (m *CheckMachine) bumpOutcome(outcome CheckState) {
	if m.state.LastOutcome == outcome {
		m.state.StreakLen++
	} else {
		m.state.StreakLen = 1
	}
	m.state.LastOutcome = outcome
}

// contributionStateFor returns the per-probe check state that should count
// toward quorum after applying the consecutive-evidence rule.
func (m *CheckMachine) contributionStateFor(outcome CheckState) CheckState {
	if m.state.StreakLen < consecutiveEvidenceThreshold {
		return CheckStateMissing
	}
	return outcome
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
