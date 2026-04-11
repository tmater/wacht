package monitoring

import (
	"fmt"

	"github.com/tmater/wacht/internal/checks"
	"github.com/tmater/wacht/internal/store"
)

// recoveryStore is the metadata and compact current-state persistence surface
// needed to bootstrap the monitoring runtime on server startup.
type recoveryStore interface {
	ListAllChecks() ([]checks.Check, error)
	ActiveProbeStates() ([]store.PersistedProbeState, error)
	PersistedCheckStates() ([]store.PersistedCheckState, error)
	OpenIncidentCheckIDs() ([]string, error)
}

// LoadRuntime builds monitoring runtime state from current metadata and the
// compact persisted per-probe / per-(check, probe) snapshots.
func LoadRuntime(src recoveryStore) (*Runtime, error) {
	checks, err := src.ListAllChecks()
	if err != nil {
		return nil, fmt.Errorf("list checks: %w", err)
	}

	probes, err := src.ActiveProbeStates()
	if err != nil {
		return nil, fmt.Errorf("list active probes: %w", err)
	}

	checkStates, err := src.PersistedCheckStates()
	if err != nil {
		return nil, fmt.Errorf("list persisted check states: %w", err)
	}

	openIncidentCheckIDs, err := src.OpenIncidentCheckIDs()
	if err != nil {
		return nil, fmt.Errorf("list open incidents: %w", err)
	}

	checkIDs := make([]string, 0, len(checks))
	for _, check := range checks {
		checkIDs = append(checkIDs, check.ID)
	}

	probeIDs := make([]string, 0, len(probes))
	for _, probe := range probes {
		probeIDs = append(probeIDs, probe.ProbeID)
	}

	runtime := NewRuntime(checkIDs, probeIDs)
	openIncidents := make(map[string]struct{}, len(openIncidentCheckIDs))
	for _, checkID := range openIncidentCheckIDs {
		openIncidents[checkID] = struct{}{}
	}

	runtime.mu.Lock()
	defer runtime.mu.Unlock()

	for _, state := range checkStates {
		quorum, ok := runtime.quorums[state.CheckID]
		if !ok {
			continue
		}
		check, ok := quorum.checks[state.ProbeID]
		if !ok {
			continue
		}
		check.state = persistedCheckExecState(state)
	}

	for _, probe := range probes {
		runtimeProbe, ok := runtime.probes[probe.ProbeID]
		if !ok || probe.LastSeenAt == nil {
			continue
		}
		lastSeenAt := probe.LastSeenAt.UTC()
		runtimeProbe.state.State = ProbeStateOnline
		runtimeProbe.state.LastHeartbeatAt = &lastSeenAt
		runtimeProbe.state.LastError = ""
	}

	// Probes without any persisted liveness evidence should not contribute old
	// check votes after a restart.
	for probeID, probe := range runtime.probes {
		if probe.state.LastHeartbeatAt != nil {
			continue
		}
		if err := runtime.applyProbeDegradationLocked(probeID, ProbeStateOffline, ""); err != nil {
			return nil, err
		}
	}

	for checkID, quorum := range runtime.quorums {
		quorum.state = newCheckQuorumState(checkID)
		if _, ok := openIncidents[checkID]; ok {
			quorum.state.State = QuorumStateDown
			quorum.state.LastStableState = QuorumStateDown
			quorum.state.IncidentOpen = true
		}
		quorum.Recompute()
	}

	return runtime, nil
}

func persistedCheckExecState(state store.PersistedCheckState) CheckExecState {
	return CheckExecState{
		CheckID:      state.CheckID,
		ProbeID:      state.ProbeID,
		LastResultAt: state.LastResultAt,
		LastOutcome:  CheckState(state.LastOutcome),
		StreakLen:    state.StreakLen,
		ExpiresAt:    state.ExpiresAt,
		State:        CheckState(state.State),
		LastError:    state.LastError,
	}
}
