package monitoring

import (
	"errors"
	"sync"
	"time"
)

var (
	// ErrUnknownCheck reports that the runtime has no such check.
	ErrUnknownCheck = errors.New("monitoring: unknown check")
	// ErrUnknownProbe reports that the runtime has no such probe.
	ErrUnknownProbe = errors.New("monitoring: unknown probe")
	// ErrUnknownCheckAssignment reports that the runtime has no such assigned
	// (check, probe) pair.
	ErrUnknownCheckAssignment = errors.New("monitoring: unknown check assignment")
)

// Runtime owns current monitoring truth in memory.
type Runtime struct {
	mu      sync.RWMutex
	probes  map[string]*ProbeMachine
	quorums map[string]*QuorumMachine
}

// NewRuntime creates runtime state for the active checks and active probes.
// In v1, every probe is assigned to every check, so each quorum machine owns
// one child check machine per active probe.
func NewRuntime(checkIDs, probeIDs []string) *Runtime {
	checkIDs = uniqueIDs(checkIDs)
	probeIDs = uniqueIDs(probeIDs)

	r := &Runtime{
		probes:  make(map[string]*ProbeMachine, len(probeIDs)),
		quorums: make(map[string]*QuorumMachine, len(checkIDs)),
	}

	for _, probeID := range probeIDs {
		r.probes[probeID] = NewProbeMachine(probeID)
	}

	for _, checkID := range checkIDs {
		r.quorums[checkID] = NewQuorumMachine(checkID, probeIDs)
	}

	return r
}

// ProbeSnapshot returns the current runtime state of one probe.
func (r *Runtime) ProbeSnapshot(probeID string) (ProbeRuntimeState, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	probe, ok := r.probes[probeID]
	if !ok {
		return ProbeRuntimeState{}, ErrUnknownProbe
	}
	return probe.Snapshot(), nil
}

// CheckSnapshot returns the current runtime state of one assigned (check,
// probe) pair.
func (r *Runtime) CheckSnapshot(checkID, probeID string) (CheckExecState, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	quorum, ok := r.quorums[checkID]
	if !ok {
		return CheckExecState{}, ErrUnknownCheck
	}

	check, ok := quorum.CheckSnapshot(probeID)
	if !ok {
		return CheckExecState{}, ErrUnknownCheckAssignment
	}
	return check, nil
}

// QuorumSnapshot returns the current aggregate runtime state of one check.
func (r *Runtime) QuorumSnapshot(checkID string) (CheckQuorumState, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	quorum, ok := r.quorums[checkID]
	if !ok {
		return CheckQuorumState{}, ErrUnknownCheck
	}
	return quorum.Snapshot(), nil
}

// ReceiveHeartbeat routes a fresh heartbeat to the owning probe machine.
func (r *Runtime) ReceiveHeartbeat(probeID string, at time.Time) (ProbeTransition, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	probe, ok := r.probes[probeID]
	if !ok {
		return ProbeTransition{}, ErrUnknownProbe
	}
	return probe.ReceiveHeartbeat(at)
}

// ExpireHeartbeat routes a heartbeat expiry to the owning probe machine.
func (r *Runtime) ExpireHeartbeat(probeID string) (ProbeTransition, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	probe, ok := r.probes[probeID]
	if !ok {
		return ProbeTransition{}, ErrUnknownProbe
	}
	return probe.ExpireHeartbeat()
}

// MarkProbeError routes a probe error to the owning probe machine.
func (r *Runtime) MarkProbeError(probeID, message string) (ProbeTransition, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	probe, ok := r.probes[probeID]
	if !ok {
		return ProbeTransition{}, ErrUnknownProbe
	}
	return probe.MarkError(message)
}

// ObserveCheckUp routes a successful result to the owning quorum machine.
func (r *Runtime) ObserveCheckUp(checkID, probeID string, at time.Time, expiresAt *time.Time) (CheckUpdate, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	quorum, ok := r.quorums[checkID]
	if !ok {
		return CheckUpdate{}, ErrUnknownCheck
	}
	return quorum.ObserveUp(probeID, at, expiresAt)
}

// ObserveCheckDown routes a failing result to the owning quorum machine.
func (r *Runtime) ObserveCheckDown(checkID, probeID string, at time.Time, expiresAt *time.Time, message string) (CheckUpdate, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	quorum, ok := r.quorums[checkID]
	if !ok {
		return CheckUpdate{}, ErrUnknownCheck
	}
	return quorum.ObserveDown(probeID, at, expiresAt, message)
}

// LoseCheckEvidence routes an evidence-expiry event to the owning quorum
// machine.
func (r *Runtime) LoseCheckEvidence(checkID, probeID string) (CheckUpdate, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	quorum, ok := r.quorums[checkID]
	if !ok {
		return CheckUpdate{}, ErrUnknownCheck
	}
	return quorum.LoseEvidence(probeID)
}

// MarkCheckError routes an unusable fresh-evidence event to the owning quorum
// machine.
func (r *Runtime) MarkCheckError(checkID, probeID, message string) (CheckUpdate, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	quorum, ok := r.quorums[checkID]
	if !ok {
		return CheckUpdate{}, ErrUnknownCheck
	}
	return quorum.MarkCheckError(probeID, message)
}

// RecomputeCheck recalculates one check's quorum state from its current child
// check machines.
func (r *Runtime) RecomputeCheck(checkID string) (CheckQuorumState, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	quorum, ok := r.quorums[checkID]
	if !ok {
		return CheckQuorumState{}, ErrUnknownCheck
	}

	quorum.Recompute()
	return quorum.Snapshot(), nil
}

// uniqueIDs removes empty and duplicate IDs while preserving first-seen order.
func uniqueIDs(ids []string) []string {
	seen := make(map[string]struct{}, len(ids))
	out := make([]string, 0, len(ids))

	for _, id := range ids {
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}

	return out
}
