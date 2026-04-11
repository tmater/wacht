package monitoring

import (
	"fmt"
	"time"

	"github.com/tmater/wacht/internal/store"
)

// heartbeatStore is the persistence surface needed for runtime-owned probe
// heartbeat ingestion.
type heartbeatStore interface {
	PersistMonitoringWrite(write store.MonitoringWrite) (store.MonitoringWrite, error)
}

// ApplyHeartbeat updates runtime-owned probe liveness, persists the matching
// recovery journal record, and refreshes the persisted probe heartbeat
// timestamp used by metadata reads.
func ApplyHeartbeat(runtime *Runtime, st heartbeatStore, probeID string, at time.Time) error {
	if runtime == nil {
		return fmt.Errorf("monitoring: runtime is required")
	}
	if st == nil {
		return fmt.Errorf("monitoring: store is required")
	}

	return runtime.applyHeartbeat(st, probeID, at)
}

// applyHeartbeat advances one probe's runtime state and persists the matching
// recovery and probe metadata writes as one unit.
func (r *Runtime) applyHeartbeat(st heartbeatStore, probeID string, at time.Time) error {
	heartbeatAt := at.UTC()

	r.mu.Lock()
	defer r.mu.Unlock()

	probe, ok := r.probes[probeID]
	if !ok {
		return ErrUnknownProbe
	}

	previous := probe.state.clone()
	if _, err := probe.ReceiveHeartbeat(heartbeatAt); err != nil {
		return err
	}

	write := store.MonitoringWrite{
		JournalRecords: []store.MonitoringJournalRecord{
			{
				Kind:       string(ProbeTriggerReceiveHeartbeat),
				ProbeID:    probeID,
				OccurredAt: heartbeatAt,
			},
		},
		ProbeHeartbeatID: probeID,
		ProbeHeartbeatAt: heartbeatAt,
	}

	if _, err := st.PersistMonitoringWrite(write); err != nil {
		probe.state = previous
		return err
	}

	return nil
}

// applyProbeDegradationLocked propagates probe-wide degradation into each
// assigned (check, probe) state so stale votes stop counting immediately.
func (r *Runtime) applyProbeDegradationLocked(probeID string, state ProbeState, message string) error {
	for _, quorum := range r.quorums {
		if _, ok := quorum.checks[probeID]; !ok {
			continue
		}

		switch state {
		case ProbeStateOffline:
			if _, err := quorum.LoseEvidence(probeID); err != nil {
				return err
			}
		case ProbeStateError:
			if _, err := quorum.MarkCheckError(probeID, message); err != nil {
				return err
			}
		}
	}

	return nil
}
