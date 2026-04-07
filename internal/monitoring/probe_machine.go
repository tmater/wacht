package monitoring

import (
	"context"
	"time"

	"github.com/qmuntal/stateless"
)

// ProbeMachine owns one probe's runtime state and transitions.
type ProbeMachine struct {
	state ProbeRuntimeState
	sm    *stateless.StateMachine
}

// NewProbeMachine creates a probe state machine.
func NewProbeMachine(probeID string) *ProbeMachine {
	m := &ProbeMachine{
		state: newProbeRuntimeState(probeID),
	}
	m.sm = newProbeStateMachine(m)
	return m
}

// Snapshot returns the current probe runtime state.
func (m *ProbeMachine) Snapshot() ProbeRuntimeState {
	return m.state
}

// ReceiveHeartbeat advances the probe state for a fresh heartbeat.
func (m *ProbeMachine) ReceiveHeartbeat(at time.Time) (ProbeTransition, error) {
	transition, err := m.fire(ProbeTriggerReceiveHeartbeat)
	if err != nil {
		return ProbeTransition{}, err
	}

	heartbeatAt := at.UTC()
	m.state.LastHeartbeatAt = &heartbeatAt
	m.state.LastError = ""
	return transition, nil
}

// ExpireHeartbeat marks the probe offline when its heartbeat is no longer
// fresh enough to trust.
func (m *ProbeMachine) ExpireHeartbeat() (ProbeTransition, error) {
	return m.fire(ProbeTriggerHeartbeatExpired)
}

// MarkError moves the probe into the error state and records the reason.
func (m *ProbeMachine) MarkError(message string) (ProbeTransition, error) {
	transition, err := m.fire(ProbeTriggerMarkError)
	if err != nil {
		return ProbeTransition{}, err
	}

	m.state.LastError = message
	return transition, nil
}

// fire sends one trigger through the probe state machine and returns the
// resulting transition summary.
func (m *ProbeMachine) fire(trigger ProbeTrigger) (ProbeTransition, error) {
	from := m.state.State
	if err := m.sm.Fire(trigger); err != nil {
		return ProbeTransition{}, err
	}

	to := m.state.State
	return ProbeTransition{
		From:    from,
		To:      to,
		Trigger: trigger,
		Reentry: from == to,
	}, nil
}

// newProbeStateMachine configures the stateless machine around the probe state
// owned by the given probe machine.
func newProbeStateMachine(owner *ProbeMachine) *stateless.StateMachine {
	sm := stateless.NewStateMachineWithExternalStorage(
		func(context.Context) (stateless.State, error) {
			return owner.state.State, nil
		},
		func(_ context.Context, state stateless.State) error {
			owner.state.State = state.(ProbeState)
			return nil
		},
		stateless.FiringImmediate,
	)

	sm.Configure(ProbeStateOnline).
		PermitReentry(ProbeTriggerReceiveHeartbeat).
		Permit(ProbeTriggerHeartbeatExpired, ProbeStateOffline).
		Permit(ProbeTriggerMarkError, ProbeStateError)

	sm.Configure(ProbeStateOffline).
		Permit(ProbeTriggerReceiveHeartbeat, ProbeStateOnline).
		PermitReentry(ProbeTriggerHeartbeatExpired).
		Permit(ProbeTriggerMarkError, ProbeStateError)

	sm.Configure(ProbeStateError).
		Permit(ProbeTriggerReceiveHeartbeat, ProbeStateOnline).
		PermitReentry(ProbeTriggerMarkError).
		Permit(ProbeTriggerHeartbeatExpired, ProbeStateOffline)

	return sm
}
