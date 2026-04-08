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

// resultStore is the persistence surface needed for result ingestion during the
// refactor period while legacy result rows still support old read paths.
type resultStore interface {
	SaveResult(r proto.CheckResult) error
	PersistMonitoringWrite(write store.MonitoringWrite) (store.MonitoringWrite, bool, error)
}

// ApplyResult persists the compatibility raw result row, updates runtime-owned
// monitoring state, and durably records the resulting journal and incident
// side effects.
func ApplyResult(runtime *Runtime, st resultStore, check checks.Check, result proto.CheckResult) error {
	if runtime == nil {
		return fmt.Errorf("monitoring: runtime is required")
	}
	if st == nil {
		return fmt.Errorf("monitoring: store is required")
	}

	if err := st.SaveResult(result); err != nil {
		return err
	}

	return runtime.applyObservedResult(st, check, result)
}

// applyObservedResult updates runtime-owned check state for one observed probe
// result and durably records the resulting journal and incident side effects.
func (r *Runtime) applyObservedResult(st resultStore, check checks.Check, result proto.CheckResult) error {
	expiresAt := evidenceExpiresAt(check, result.Timestamp)

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.probes[result.ProbeID]; !ok {
		return ErrUnknownProbe
	}

	quorum := r.ensureQuorumLocked(check.ID)
	child, ok := quorum.checks[result.ProbeID]
	if !ok {
		return ErrUnknownCheckAssignment
	}

	previousCheck := child.state
	previousQuorum := quorum.state

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
		return err
	}

	write, err := monitoringWriteForResult(check, result, expiresAt, quorum, previousQuorum, update.Quorum)
	if err != nil {
		child.state = previousCheck
		quorum.state = previousQuorum
		return err
	}

	if _, _, err := st.PersistMonitoringWrite(write); err != nil {
		child.state = previousCheck
		quorum.state = previousQuorum
		return err
	}

	return nil
}

// monitoringWriteForResult derives the durable journal entry and any incident
// side effect that corresponds to one accepted probe result.
func monitoringWriteForResult(
	check checks.Check,
	result proto.CheckResult,
	expiresAt time.Time,
	quorum *QuorumMachine,
	previousQuorum CheckQuorumState,
	currentQuorum CheckQuorumState,
) (store.MonitoringWrite, error) {
	write := store.MonitoringWrite{
		JournalRecords: []store.MonitoringJournalRecord{
			{
				Kind:       resultTriggerKind(result),
				CheckID:    check.ID,
				ProbeID:    result.ProbeID,
				Message:    strings.TrimSpace(result.Error),
				ExpiresAt:  &expiresAt,
				OccurredAt: result.Timestamp,
			},
		},
	}

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

// evidenceExpiresAt returns the freshness deadline for one accepted probe
// result using the check interval as the base cadence.
func evidenceExpiresAt(check checks.Check, observedAt time.Time) time.Time {
	intervalSeconds := check.Interval
	if intervalSeconds <= 0 {
		intervalSeconds = checks.DefaultInterval
	}
	return observedAt.UTC().Add(2 * time.Duration(intervalSeconds) * time.Second)
}

// resultTriggerKind maps one probe result to the matching persisted check
// trigger name.
func resultTriggerKind(result proto.CheckResult) string {
	if result.Up {
		return string(CheckTriggerObserveUp)
	}
	return string(CheckTriggerObserveDown)
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
