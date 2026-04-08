package monitoring

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/tmater/wacht/internal/checks"
	"github.com/tmater/wacht/internal/store"
)

const recoverySnapshotVersion = 1

var (
	// ErrInvalidRecoverySnapshot reports a malformed snapshot payload.
	ErrInvalidRecoverySnapshot = errors.New("monitoring: invalid recovery snapshot")
	// ErrUnknownRecoveryJournalKind reports an unsupported journal kind.
	ErrUnknownRecoveryJournalKind = errors.New("monitoring: unknown recovery journal kind")
)

// recoveryStore is the metadata and recovery persistence surface needed to
// bootstrap the monitoring runtime on server startup.
type recoveryStore interface {
	ListAllChecks() ([]checks.Check, error)
	ActiveProbeIDs() ([]string, error)
	LatestMonitoringSnapshot() (*store.MonitoringSnapshot, error)
	MonitoringJournalAfter(afterID int64) ([]store.MonitoringJournalRecord, error)
}

// runtimeSnapshotPayload is the serialized runtime image stored inside one
// monitoring snapshot row.
type runtimeSnapshotPayload struct {
	Version int                 `json:"version"`
	Probes  []ProbeRuntimeState `json:"probes"`
	Checks  []CheckExecState    `json:"checks"`
	Quorums []CheckQuorumState  `json:"quorums"`
}

// LoadRuntime builds monitoring runtime state from current metadata, then
// overlays the latest snapshot and replays the journal tail after it.
func LoadRuntime(src recoveryStore) (*Runtime, error) {
	checks, err := src.ListAllChecks()
	if err != nil {
		return nil, fmt.Errorf("list checks: %w", err)
	}

	probeIDs, err := src.ActiveProbeIDs()
	if err != nil {
		return nil, fmt.Errorf("list active probes: %w", err)
	}

	checkIDs := make([]string, 0, len(checks))
	for _, check := range checks {
		checkIDs = append(checkIDs, check.ID)
	}

	runtime := NewRuntime(checkIDs, probeIDs)

	afterID := int64(0)
	snapshot, err := src.LatestMonitoringSnapshot()
	if err != nil {
		return nil, fmt.Errorf("load monitoring snapshot: %w", err)
	}
	if snapshot != nil {
		if err := runtime.restoreSnapshot(*snapshot); err != nil {
			return nil, err
		}
		afterID = snapshot.LastJournalID
	}

	records, err := src.MonitoringJournalAfter(afterID)
	if err != nil {
		return nil, fmt.Errorf("load monitoring journal tail: %w", err)
	}
	for _, record := range records {
		if err := runtime.applyJournalRecord(record); err != nil {
			return nil, err
		}
	}

	return runtime, nil
}

// RecoverySnapshot captures the current runtime image as one persisted recovery
// baseline.
func (r *Runtime) RecoverySnapshot(lastJournalID int64, capturedAt time.Time) (store.MonitoringSnapshot, error) {
	payload, err := json.Marshal(r.snapshotPayload())
	if err != nil {
		return store.MonitoringSnapshot{}, fmt.Errorf("marshal recovery snapshot: %w", err)
	}

	return store.MonitoringSnapshot{
		LastJournalID: lastJournalID,
		Payload:       payload,
		CapturedAt:    capturedAt,
	}, nil
}

func (r *Runtime) snapshotPayload() runtimeSnapshotPayload {
	r.mu.RLock()
	defer r.mu.RUnlock()

	payload := runtimeSnapshotPayload{
		Version: recoverySnapshotVersion,
		Probes:  make([]ProbeRuntimeState, 0, len(r.probes)),
		Quorums: make([]CheckQuorumState, 0, len(r.quorums)),
	}

	probeIDs := make([]string, 0, len(r.probes))
	for probeID := range r.probes {
		probeIDs = append(probeIDs, probeID)
	}
	sort.Strings(probeIDs)

	checkIDs := make([]string, 0, len(r.quorums))
	for checkID := range r.quorums {
		checkIDs = append(checkIDs, checkID)
	}
	sort.Strings(checkIDs)

	checks := make([]CheckExecState, 0)
	for _, probeID := range probeIDs {
		payload.Probes = append(payload.Probes, r.probes[probeID].Snapshot())
	}

	for _, checkID := range checkIDs {
		quorum := r.quorums[checkID]
		payload.Quorums = append(payload.Quorums, quorum.Snapshot())

		childProbeIDs := make([]string, 0, len(quorum.checks))
		for probeID := range quorum.checks {
			childProbeIDs = append(childProbeIDs, probeID)
		}
		sort.Strings(childProbeIDs)

		for _, probeID := range childProbeIDs {
			checks = append(checks, quorum.checks[probeID].Snapshot())
		}
	}

	payload.Checks = checks
	return payload
}

func (r *Runtime) restoreSnapshot(snapshot store.MonitoringSnapshot) error {
	var payload runtimeSnapshotPayload
	if err := json.Unmarshal(snapshot.Payload, &payload); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidRecoverySnapshot, err)
	}
	if payload.Version != recoverySnapshotVersion {
		return fmt.Errorf("%w: version %d", ErrInvalidRecoverySnapshot, payload.Version)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	for _, probeState := range payload.Probes {
		probe, ok := r.probes[probeState.ProbeID]
		if !ok {
			continue
		}
		probe.state = probeState.clone()
	}

	for _, checkState := range payload.Checks {
		quorum, ok := r.quorums[checkState.CheckID]
		if !ok {
			continue
		}
		check, ok := quorum.checks[checkState.ProbeID]
		if !ok {
			continue
		}
		check.state = checkState
	}

	for _, quorumState := range payload.Quorums {
		quorum, ok := r.quorums[quorumState.CheckID]
		if !ok {
			continue
		}
		quorum.state = quorumState
	}

	return nil
}

func (r *Runtime) applyJournalRecord(record store.MonitoringJournalRecord) error {
	if record.CheckID == "" {
		switch ProbeTrigger(record.Kind) {
		case ProbeTriggerReceiveHeartbeat:
			_, err := r.ReceiveHeartbeat(record.ProbeID, record.OccurredAt)
			return ignoreUnknownReplayError(err)
		case ProbeTriggerHeartbeatExpired:
			_, err := r.ExpireHeartbeat(record.ProbeID)
			return ignoreUnknownReplayError(err)
		case ProbeTriggerMarkError:
			_, err := r.MarkProbeError(record.ProbeID, record.Message)
			return ignoreUnknownReplayError(err)
		default:
			return fmt.Errorf("%w: %s", ErrUnknownRecoveryJournalKind, record.Kind)
		}
	}

	switch CheckTrigger(record.Kind) {
	case CheckTriggerObserveUp:
		_, err := r.ObserveCheckUp(record.CheckID, record.ProbeID, record.OccurredAt, record.ExpiresAt)
		return ignoreUnknownReplayError(err)
	case CheckTriggerObserveDown:
		_, err := r.ObserveCheckDown(record.CheckID, record.ProbeID, record.OccurredAt, record.ExpiresAt, record.Message)
		return ignoreUnknownReplayError(err)
	case CheckTriggerLoseEvidence:
		_, err := r.LoseCheckEvidence(record.CheckID, record.ProbeID)
		return ignoreUnknownReplayError(err)
	case CheckTriggerMarkError:
		_, err := r.MarkCheckError(record.CheckID, record.ProbeID, record.Message)
		return ignoreUnknownReplayError(err)
	default:
		return fmt.Errorf("%w: %s", ErrUnknownRecoveryJournalKind, record.Kind)
	}
}

func ignoreUnknownReplayError(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, ErrUnknownProbe):
		return nil
	case errors.Is(err, ErrUnknownCheck):
		return nil
	case errors.Is(err, ErrUnknownCheckAssignment):
		return nil
	default:
		return err
	}
}
