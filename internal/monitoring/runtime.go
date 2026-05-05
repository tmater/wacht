package monitoring

import (
	"errors"
	"sort"
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

// NewRuntime creates runtime state for the active checks and known probes.
// Probes join a check's quorum only after submitting a valid result for that
// check, so newly known probes do not change existing check outcomes before
// they have execution evidence.
func NewRuntime(checkIDs, probeIDs []string) *Runtime {
	checkIDs = uniqueIDs(checkIDs)
	probeIDs = uniqueIDs(probeIDs)

	r := &Runtime{
		probes:  make(map[string]*ProbeMachine, len(probeIDs)),
		quorums: make(map[string]*QuorumMachine, len(checkIDs)),
	}

	for _, checkID := range checkIDs {
		r.quorums[checkID] = NewQuorumMachine(checkID, nil)
	}

	for _, probeID := range probeIDs {
		r.addProbeLocked(probeID)
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

// EnsureCheck creates an explicit pending quorum entry for one check when the
// runtime does not already know about it.
func (r *Runtime) EnsureCheck(checkID string) CheckQuorumState {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.ensureQuorumLocked(checkID).Snapshot()
}

// AddProbe creates an explicit offline probe entry when the runtime does not
// already know about it. It does not assign the probe to check quorums; that
// happens when the probe submits its first accepted result for each check.
func (r *Runtime) AddProbe(probeID string) ProbeRuntimeState {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.addProbeLocked(probeID)
}

func (r *Runtime) addProbeLocked(probeID string) ProbeRuntimeState {
	if probeID == "" {
		return ProbeRuntimeState{}
	}
	if probe, ok := r.probes[probeID]; ok {
		return probe.Snapshot()
	}

	probe := NewProbeMachine(probeID)
	r.probes[probeID] = probe
	return probe.Snapshot()
}

// RemoveCheck drops one check's runtime state after the owning metadata has
// been deleted.
func (r *Runtime) RemoveCheck(checkID string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	delete(r.quorums, checkID)
}

// QuorumSnapshots returns aggregate runtime state for the requested checks in
// input order. Missing checks are surfaced as explicit pending state so callers
// do not have to reconstruct "no runtime evidence yet" elsewhere.
func (r *Runtime) QuorumSnapshots(checkIDs []string) []CheckQuorumState {
	checkIDs = uniqueIDs(checkIDs)

	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make([]CheckQuorumState, 0, len(checkIDs))
	for _, checkID := range checkIDs {
		quorum, ok := r.quorums[checkID]
		if !ok {
			out = append(out, newCheckQuorumState(checkID))
			continue
		}
		out = append(out, quorum.Snapshot())
	}
	return out
}

// ProbeSnapshots returns all probe runtime states ordered by probe ID.
func (r *Runtime) ProbeSnapshots() []ProbeRuntimeState {
	r.mu.RLock()
	defer r.mu.RUnlock()

	probeIDs := make([]string, 0, len(r.probes))
	for probeID := range r.probes {
		probeIDs = append(probeIDs, probeID)
	}
	sort.Strings(probeIDs)

	out := make([]ProbeRuntimeState, 0, len(probeIDs))
	for _, probeID := range probeIDs {
		out = append(out, r.probes[probeID].Snapshot())
	}
	return out
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

	transition, err := probe.ExpireHeartbeat()
	if err != nil {
		return ProbeTransition{}, err
	}
	if err := r.applyProbeDegradationLocked(probeID, ProbeStateOffline, ""); err != nil {
		return ProbeTransition{}, err
	}
	return transition, nil
}

// MarkProbeError routes a probe error to the owning probe machine.
func (r *Runtime) MarkProbeError(probeID, message string) (ProbeTransition, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	probe, ok := r.probes[probeID]
	if !ok {
		return ProbeTransition{}, ErrUnknownProbe
	}

	transition, err := probe.MarkError(message)
	if err != nil {
		return ProbeTransition{}, err
	}
	if err := r.applyProbeDegradationLocked(probeID, ProbeStateError, message); err != nil {
		return ProbeTransition{}, err
	}
	return transition, nil
}

// ObserveCheckUp routes a successful result to the owning quorum machine.
func (r *Runtime) ObserveCheckUp(checkID, probeID string, at time.Time, expiresAt *time.Time) (CheckUpdate, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.probes[probeID]; !ok {
		return CheckUpdate{}, ErrUnknownProbe
	}
	quorum, ok := r.quorums[checkID]
	if !ok {
		return CheckUpdate{}, ErrUnknownCheck
	}
	quorum.AddProbe(probeID)
	return quorum.ObserveUp(probeID, at, expiresAt)
}

// ObserveCheckDown routes a failing result to the owning quorum machine.
func (r *Runtime) ObserveCheckDown(checkID, probeID string, at time.Time, expiresAt *time.Time, message string) (CheckUpdate, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.probes[probeID]; !ok {
		return CheckUpdate{}, ErrUnknownProbe
	}
	quorum, ok := r.quorums[checkID]
	if !ok {
		return CheckUpdate{}, ErrUnknownCheck
	}
	quorum.AddProbe(probeID)
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
