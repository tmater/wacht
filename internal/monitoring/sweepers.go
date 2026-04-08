package monitoring

import (
	"fmt"
	"sort"
	"time"

	"github.com/tmater/wacht/internal/checks"
	"github.com/tmater/wacht/internal/store"
)

// sweeperStore is the persistence surface needed by the runtime-owned stale
// probe and stale evidence sweepers.
type sweeperStore interface {
	PersistMonitoringWrite(write store.MonitoringWrite) (store.MonitoringWrite, bool, error)
	GetCheck(id string) (*checks.Check, error)
}

type probeSweepRollback struct {
	probe   ProbeRuntimeState
	checks  map[string]CheckExecState
	quorums map[string]CheckQuorumState
}

// SweepProbes expires probe heartbeats that are older than offlineAfter and
// persists the matching recovery records.
func SweepProbes(runtime *Runtime, st sweeperStore, now time.Time, offlineAfter time.Duration) (int, error) {
	if runtime == nil {
		return 0, fmt.Errorf("monitoring: runtime is required")
	}
	if st == nil {
		return 0, fmt.Errorf("monitoring: store is required")
	}
	if offlineAfter <= 0 {
		return 0, fmt.Errorf("monitoring: offlineAfter must be positive")
	}

	sweptAt := now.UTC()

	runtime.mu.RLock()
	probeIDs := make([]string, 0, len(runtime.probes))
	for probeID := range runtime.probes {
		probeIDs = append(probeIDs, probeID)
	}
	runtime.mu.RUnlock()
	sort.Strings(probeIDs)

	expired := 0
	for _, probeID := range probeIDs {
		runtime.mu.Lock()

		probe, ok := runtime.probes[probeID]
		if !ok {
			runtime.mu.Unlock()
			return expired, ErrUnknownProbe
		}
		if probe.state.State == ProbeStateOffline ||
			probe.state.LastHeartbeatAt == nil ||
			probe.state.LastHeartbeatAt.Add(offlineAfter).After(sweptAt) {
			runtime.mu.Unlock()
			continue
		}

		rollback := runtime.captureProbeSweepRollbackLocked(probeID)

		if _, err := probe.ExpireHeartbeat(); err != nil {
			runtime.mu.Unlock()
			return expired, err
		}
		if err := runtime.applyProbeDegradationLocked(probeID, ProbeStateOffline, ""); err != nil {
			runtime.restoreProbeSweepRollbackLocked(probeID, rollback)
			runtime.mu.Unlock()
			return expired, err
		}

		write := store.MonitoringWrite{
			JournalRecords: []store.MonitoringJournalRecord{
				{
					Kind:       string(ProbeTriggerHeartbeatExpired),
					ProbeID:    probeID,
					OccurredAt: sweptAt,
				},
			},
		}
		if _, _, err := st.PersistMonitoringWrite(write); err != nil {
			runtime.restoreProbeSweepRollbackLocked(probeID, rollback)
			runtime.mu.Unlock()
			return expired, err
		}

		runtime.mu.Unlock()
		expired++
	}
	return expired, nil
}

// SweepChecks expires stale per-(check, probe) evidence and persists the
// matching recovery records.
func SweepChecks(runtime *Runtime, st sweeperStore, now time.Time) (int, error) {
	if runtime == nil {
		return 0, fmt.Errorf("monitoring: runtime is required")
	}
	if st == nil {
		return 0, fmt.Errorf("monitoring: store is required")
	}

	sweptAt := now.UTC()

	runtime.mu.RLock()
	assignments := make([]struct {
		CheckID string
		ProbeID string
	}, 0)
	for checkID, quorum := range runtime.quorums {
		for probeID, check := range quorum.checks {
			if check.state.State == CheckStateError ||
				check.state.ExpiresAt.IsZero() ||
				check.state.ExpiresAt.After(sweptAt) {
				continue
			}
			assignments = append(assignments, struct {
				CheckID string
				ProbeID string
			}{
				CheckID: checkID,
				ProbeID: probeID,
			})
		}
	}
	runtime.mu.RUnlock()
	sort.Slice(assignments, func(i, j int) bool {
		if assignments[i].CheckID == assignments[j].CheckID {
			return assignments[i].ProbeID < assignments[j].ProbeID
		}
		return assignments[i].CheckID < assignments[j].CheckID
	})

	expired := 0
	for _, assignment := range assignments {
		runtime.mu.Lock()

		quorum, ok := runtime.quorums[assignment.CheckID]
		if !ok {
			runtime.mu.Unlock()
			return expired, ErrUnknownCheck
		}
		check, ok := quorum.checks[assignment.ProbeID]
		if !ok {
			runtime.mu.Unlock()
			return expired, ErrUnknownCheckAssignment
		}
		if check.state.State == CheckStateError ||
			check.state.ExpiresAt.IsZero() ||
			check.state.ExpiresAt.After(sweptAt) {
			runtime.mu.Unlock()
			continue
		}

		previousCheck := check.state
		previousQuorum := quorum.state

		update, err := quorum.LoseEvidence(assignment.ProbeID)
		if err != nil {
			runtime.mu.Unlock()
			return expired, err
		}

		write := store.MonitoringWrite{
			JournalRecords: []store.MonitoringJournalRecord{
				{
					Kind:       string(CheckTriggerLoseEvidence),
					CheckID:    assignment.CheckID,
					ProbeID:    assignment.ProbeID,
					OccurredAt: sweptAt,
				},
			},
		}
		if previousQuorum.LastStableState != update.Quorum.LastStableState {
			checkDef, err := st.GetCheck(assignment.CheckID)
			if err != nil {
				check.state = previousCheck
				quorum.state = previousQuorum
				runtime.mu.Unlock()
				return expired, err
			}
			if checkDef != nil {
				write, err = monitoringWriteForCheckEvent(*checkDef, quorum, previousQuorum, update.Quorum, write)
				if err != nil {
					check.state = previousCheck
					quorum.state = previousQuorum
					runtime.mu.Unlock()
					return expired, err
				}
			}
		}
		if _, _, err := st.PersistMonitoringWrite(write); err != nil {
			check.state = previousCheck
			quorum.state = previousQuorum
			runtime.mu.Unlock()
			return expired, err
		}

		runtime.mu.Unlock()
		expired++
	}
	return expired, nil
}

func (r *Runtime) captureProbeSweepRollbackLocked(probeID string) probeSweepRollback {
	rollback := probeSweepRollback{
		checks:  make(map[string]CheckExecState),
		quorums: make(map[string]CheckQuorumState),
	}

	if probe, ok := r.probes[probeID]; ok {
		rollback.probe = probe.state.clone()
	}

	for checkID, quorum := range r.quorums {
		check, ok := quorum.checks[probeID]
		if !ok {
			continue
		}

		rollback.checks[checkID] = check.state
		rollback.quorums[checkID] = quorum.state
	}

	return rollback
}

func (r *Runtime) restoreProbeSweepRollbackLocked(probeID string, rollback probeSweepRollback) {
	if probe, ok := r.probes[probeID]; ok {
		probe.state = rollback.probe.clone()
	}

	for checkID, previousCheck := range rollback.checks {
		quorum, ok := r.quorums[checkID]
		if !ok {
			continue
		}
		check, ok := quorum.checks[probeID]
		if !ok {
			continue
		}

		check.state = previousCheck
		if previousQuorum, ok := rollback.quorums[checkID]; ok {
			quorum.state = previousQuorum
		}
	}
}
