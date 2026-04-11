package monitoring

import (
	"errors"
	"fmt"

	"github.com/tmater/wacht/internal/checks"
	"github.com/tmater/wacht/internal/store"
)

var (
	// ErrUnknownRecoveryJournalKind reports an unsupported journal kind.
	ErrUnknownRecoveryJournalKind = errors.New("monitoring: unknown recovery journal kind")
)

// recoveryStore is the metadata and recovery persistence surface needed to
// bootstrap the monitoring runtime on server startup.
type recoveryStore interface {
	ListAllChecks() ([]checks.Check, error)
	ActiveProbeIDs() ([]string, error)
	MonitoringJournalAfter(afterID int64) ([]store.MonitoringJournalRecord, error)
}

// LoadRuntime builds monitoring runtime state from current metadata, then
// replays the monitoring journal to recover the latest in-memory view.
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

	records, err := src.MonitoringJournalAfter(0)
	if err != nil {
		return nil, fmt.Errorf("load monitoring journal: %w", err)
	}
	for _, record := range records {
		if err := runtime.applyJournalRecord(record); err != nil {
			return nil, err
		}
	}

	return runtime, nil
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
