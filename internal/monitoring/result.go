package monitoring

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/tmater/wacht/internal/alert"
	"github.com/tmater/wacht/internal/checks"
	"github.com/tmater/wacht/internal/proto"
	"github.com/tmater/wacht/internal/store"
)

type resultBatchStore interface {
	PersistMonitoringBatch(writes []store.MonitoringWrite) ([]store.MonitoringWrite, error)
}

// ObservedResult is one normalized check/result pair accepted from a probe.
type ObservedResult struct {
	Check  checks.Check
	Result proto.CheckResult
}

// ApplyResultBatch updates runtime-owned monitoring state for one accepted
// result batch and durably records all resulting current-state rows and
// incident side effects in one DB transaction.
func ApplyResultBatch(runtime *Runtime, st resultBatchStore, observed []ObservedResult) error {
	if runtime == nil {
		return fmt.Errorf("monitoring: runtime is required")
	}
	if st == nil {
		return fmt.Errorf("monitoring: store is required")
	}
	if len(observed) == 0 {
		return nil
	}

	return runtime.applyObservedResultBatch(st, observed)
}

func (r *Runtime) applyObservedResultBatch(st resultBatchStore, observed []ObservedResult) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	writes := make([]store.MonitoringWrite, 0, len(observed))
	rollbacks := make([]observedResultRollback, 0, len(observed))

	for _, item := range observed {
		write, rollback, err := r.applyObservedResultLocked(item.Check, item.Result)
		if err != nil {
			r.rollbackObservedResultsLocked(rollbacks)
			return err
		}
		writes = append(writes, write)
		rollbacks = append(rollbacks, rollback)
	}

	if _, err := st.PersistMonitoringBatch(writes); err != nil {
		r.rollbackObservedResultsLocked(rollbacks)
		return err
	}

	return nil
}

type observedResultRollback struct {
	CheckID        string
	ProbeID        string
	CreatedQuorum  bool
	PreviousCheck  CheckExecState
	PreviousQuorum CheckQuorumState
}

func (r *Runtime) applyObservedResultLocked(check checks.Check, result proto.CheckResult) (store.MonitoringWrite, observedResultRollback, error) {
	expiresAt := evidenceExpiresAt(check, result.Timestamp)
	checkID := check.ID

	if _, ok := r.probes[result.ProbeID]; !ok {
		return store.MonitoringWrite{}, observedResultRollback{}, ErrUnknownProbe
	}

	_, quorumExisted := r.quorums[checkID]
	quorum := r.ensureQuorumLocked(checkID)
	child, ok := quorum.checks[result.ProbeID]
	if !ok {
		return store.MonitoringWrite{}, observedResultRollback{}, ErrUnknownCheckAssignment
	}

	rollback := observedResultRollback{
		CheckID:        checkID,
		ProbeID:        result.ProbeID,
		CreatedQuorum:  !quorumExisted,
		PreviousCheck:  child.state,
		PreviousQuorum: quorum.state,
	}

	var (
		update CheckUpdate
		err    error
	)
	if result.Up {
		update, err = quorum.ObserveUp(result.ProbeID, result.Timestamp, &expiresAt)
	} else {
		update, err = quorum.ObserveDown(result.ProbeID, result.Timestamp, &expiresAt, strings.TrimSpace(result.Error))
	}
	if err != nil {
		return store.MonitoringWrite{}, observedResultRollback{}, err
	}

	write := store.MonitoringWrite{
		CheckStateWrites: []store.CheckStateWrite{
			{
				CheckID:      checkID,
				ProbeID:      result.ProbeID,
				LastResultAt: child.state.LastResultAt,
				LastOutcome:  string(child.state.LastOutcome),
				StreakLen:    child.state.StreakLen,
				ExpiresAt:    child.state.ExpiresAt,
				State:        string(child.state.State),
				LastError:    child.state.LastError,
			},
		},
	}
	write, err = monitoringWriteForCheckEvent(check, quorum, rollback.PreviousQuorum, update.Quorum, write)
	if err != nil {
		return store.MonitoringWrite{}, observedResultRollback{}, err
	}

	return write, rollback, nil
}

func (r *Runtime) rollbackObservedResultsLocked(rollbacks []observedResultRollback) {
	for i := len(rollbacks) - 1; i >= 0; i-- {
		rollback := rollbacks[i]
		if rollback.CreatedQuorum {
			delete(r.quorums, rollback.CheckID)
			continue
		}

		quorum, ok := r.quorums[rollback.CheckID]
		if !ok {
			continue
		}
		child, ok := quorum.checks[rollback.ProbeID]
		if !ok {
			continue
		}
		child.state = rollback.PreviousCheck
		quorum.state = rollback.PreviousQuorum
	}
}

// evidenceExpiresAt returns the freshness deadline for one accepted probe
// result using the check interval as the base cadence.
func evidenceExpiresAt(check checks.Check, observedAt time.Time) time.Time {
	intervalSeconds := check.Interval
	if intervalSeconds <= 0 {
		intervalSeconds = checks.DefaultInterval
	}
	return observedAt.UTC().Add(2 * time.Duration(intervalSeconds) * time.Second)
}

// ensureQuorumLocked returns the quorum machine for one check, creating it on
// demand so checks added after boot can start reporting before the next
// restart.
func (r *Runtime) ensureQuorumLocked(checkID string) *QuorumMachine {
	quorum, ok := r.quorums[checkID]
	if ok {
		return quorum
	}

	probeIDs := make([]string, 0, len(r.probes))
	for probeID := range r.probes {
		probeIDs = append(probeIDs, probeID)
	}
	sort.Strings(probeIDs)

	quorum = NewQuorumMachine(checkID, probeIDs)
	r.quorums[checkID] = quorum
	return quorum
}

func monitoringWriteForCheckEvent(
	check checks.Check,
	quorum *QuorumMachine,
	previousQuorum CheckQuorumState,
	currentQuorum CheckQuorumState,
	write store.MonitoringWrite,
) (store.MonitoringWrite, error) {
	switch {
	case previousQuorum.LastStableState == QuorumStateUp && currentQuorum.LastStableState == QuorumStateDown:
		request, err := notificationRequest(check, "down", quorum)
		if err != nil {
			return store.MonitoringWrite{}, err
		}
		write.IncidentCheckID = check.ID
		write.IncidentNotification = request
	case previousQuorum.IncidentOpen &&
		previousQuorum.LastStableState == QuorumStateDown &&
		currentQuorum.LastStableState == QuorumStateUp:
		request, err := notificationRequest(check, "up", quorum)
		if err != nil {
			return store.MonitoringWrite{}, err
		}
		write.IncidentCheckID = check.ID
		write.ResolveIncident = true
		write.IncidentNotification = request
	}

	return write, nil
}

// notificationRequest builds the durable webhook work item for one stable
// quorum transition.
func notificationRequest(check checks.Check, status string, quorum *QuorumMachine) (*store.NotificationRequest, error) {
	if check.Webhook == "" {
		return nil, nil
	}

	probesDown, probesTotal := quorumCounts(quorum)
	body, err := json.Marshal(alert.AlertPayload{
		CheckID:     check.ID,
		CheckName:   check.Name,
		Target:      check.Target,
		Status:      status,
		ProbesDown:  probesDown,
		ProbesTotal: probesTotal,
	})
	if err != nil {
		return nil, err
	}

	return &store.NotificationRequest{
		WebhookURL: check.Webhook,
		Payload:    body,
	}, nil
}

// quorumCounts summarizes the current child-check distribution for incident
// notifications.
func quorumCounts(quorum *QuorumMachine) (down int, total int) {
	total = len(quorum.checks)
	for _, check := range quorum.checks {
		if check.state.State == CheckStateDown {
			down++
		}
	}
	return down, total
}
