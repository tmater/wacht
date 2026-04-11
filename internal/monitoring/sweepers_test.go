package monitoring

import (
	"errors"
	"testing"
	"time"

	"github.com/tmater/wacht/internal/checks"
	"github.com/tmater/wacht/internal/store"
)

type fakeSweeperStore struct {
	persistMonitoringWriteFn func(write store.MonitoringWrite) (store.MonitoringWrite, error)
	getCheckFn               func(id string) (*checks.Check, error)
	persistedWrites          []store.MonitoringWrite
}

func (f *fakeSweeperStore) PersistMonitoringWrite(write store.MonitoringWrite) (store.MonitoringWrite, error) {
	f.persistedWrites = append(f.persistedWrites, write)
	if f.persistMonitoringWriteFn != nil {
		return f.persistMonitoringWriteFn(write)
	}
	return write, nil
}

func (f *fakeSweeperStore) GetCheck(id string) (*checks.Check, error) {
	if f.getCheckFn != nil {
		return f.getCheckFn(id)
	}
	return nil, nil
}

func TestSweepProbesExpiresStaleHeartbeatAndClearsVotes(t *testing.T) {
	st := &fakeSweeperStore{}
	runtime := NewRuntime([]string{"check-a"}, []string{"probe-a", "probe-b", "probe-c"})
	at := time.Date(2026, time.April, 8, 12, 0, 0, 0, time.UTC)
	expiresAt := at.Add(30 * time.Second)
	secondAt := at.Add(time.Second)
	secondExpiry := secondAt.Add(30 * time.Second)

	for _, probeID := range []string{"probe-a", "probe-b"} {
		if _, err := runtime.ObserveCheckUp("check-a", probeID, at, &expiresAt); err != nil {
			t.Fatalf("ObserveCheckUp %s first: %v", probeID, err)
		}
		if _, err := runtime.ObserveCheckUp("check-a", probeID, secondAt, &secondExpiry); err != nil {
			t.Fatalf("ObserveCheckUp %s second: %v", probeID, err)
		}
	}

	if _, err := runtime.ReceiveHeartbeat("probe-a", at); err != nil {
		t.Fatalf("ReceiveHeartbeat probe-a: %v", err)
	}
	if _, err := runtime.ReceiveHeartbeat("probe-b", at.Add(20*time.Second)); err != nil {
		t.Fatalf("ReceiveHeartbeat probe-b: %v", err)
	}

	expired, err := SweepProbes(runtime, st, at.Add(100*time.Second), 90*time.Second)
	if err != nil {
		t.Fatalf("SweepProbes() error = %v", err)
	}
	if expired != 1 {
		t.Fatalf("expired probes = %d, want 1", expired)
	}
	if len(st.persistedWrites) != 0 {
		t.Fatalf("persisted writes = %d, want 0", len(st.persistedWrites))
	}

	probe, err := runtime.ProbeSnapshot("probe-a")
	if err != nil {
		t.Fatalf("ProbeSnapshot() error = %v", err)
	}
	if probe.State != ProbeStateOffline {
		t.Fatalf("probe state = %q, want %q", probe.State, ProbeStateOffline)
	}

	check, err := runtime.CheckSnapshot("check-a", "probe-a")
	if err != nil {
		t.Fatalf("CheckSnapshot() error = %v", err)
	}
	if check.State != CheckStateMissing {
		t.Fatalf("check state = %q, want %q", check.State, CheckStateMissing)
	}
	if check.LastOutcome != "" {
		t.Fatalf("last outcome = %q, want empty", check.LastOutcome)
	}

	quorum, err := runtime.QuorumSnapshot("check-a")
	if err != nil {
		t.Fatalf("QuorumSnapshot() error = %v", err)
	}
	if quorum.State != QuorumStateError {
		t.Fatalf("quorum state = %q, want %q", quorum.State, QuorumStateError)
	}
	if quorum.LastStableState != QuorumStateUp {
		t.Fatalf("last stable state = %q, want %q", quorum.LastStableState, QuorumStateUp)
	}
}

func TestSweepProbesDoesNotPersistExpiredHeartbeats(t *testing.T) {
	st := &fakeSweeperStore{
		persistMonitoringWriteFn: func(write store.MonitoringWrite) (store.MonitoringWrite, error) {
			return store.MonitoringWrite{}, errors.New("unexpected persist")
		},
	}
	runtime := NewRuntime([]string{"check-a"}, []string{"probe-a", "probe-b"})
	at := time.Date(2026, time.April, 8, 12, 0, 0, 0, time.UTC)
	expiresAt := at.Add(30 * time.Second)
	secondAt := at.Add(time.Second)
	secondExpiry := secondAt.Add(30 * time.Second)

	for _, probeID := range []string{"probe-a", "probe-b"} {
		if _, err := runtime.ObserveCheckUp("check-a", probeID, at, &expiresAt); err != nil {
			t.Fatalf("ObserveCheckUp %s first: %v", probeID, err)
		}
		if _, err := runtime.ObserveCheckUp("check-a", probeID, secondAt, &secondExpiry); err != nil {
			t.Fatalf("ObserveCheckUp %s second: %v", probeID, err)
		}
		if _, err := runtime.ReceiveHeartbeat(probeID, at); err != nil {
			t.Fatalf("ReceiveHeartbeat %s: %v", probeID, err)
		}
	}

	expired, err := SweepProbes(runtime, st, at.Add(100*time.Second), 90*time.Second)
	if err != nil {
		t.Fatalf("SweepProbes() error = %v", err)
	}
	if expired != 2 {
		t.Fatalf("expired probes = %d, want 2", expired)
	}
	if len(st.persistedWrites) != 0 {
		t.Fatalf("persisted writes = %d, want 0", len(st.persistedWrites))
	}

	probe, err := runtime.ProbeSnapshot("probe-a")
	if err != nil {
		t.Fatalf("ProbeSnapshot() error = %v", err)
	}
	if probe.State != ProbeStateOffline {
		t.Fatalf("probe state = %q, want %q", probe.State, ProbeStateOffline)
	}

	check, err := runtime.CheckSnapshot("check-a", "probe-a")
	if err != nil {
		t.Fatalf("CheckSnapshot() error = %v", err)
	}
	if check.State != CheckStateMissing {
		t.Fatalf("check state = %q, want %q", check.State, CheckStateMissing)
	}

	quorum, err := runtime.QuorumSnapshot("check-a")
	if err != nil {
		t.Fatalf("QuorumSnapshot() error = %v", err)
	}
	if quorum.State != QuorumStateError {
		t.Fatalf("quorum state = %q, want %q", quorum.State, QuorumStateError)
	}
}

func TestSweepChecksExpiresStaleEvidenceAndResetsStreak(t *testing.T) {
	st := &fakeSweeperStore{}
	runtime := NewRuntime([]string{"check-a"}, []string{"probe-a"})
	at := time.Date(2026, time.April, 8, 12, 0, 0, 0, time.UTC)
	expiresAt := at.Add(10 * time.Second)

	if _, err := runtime.ObserveCheckUp("check-a", "probe-a", at, &expiresAt); err != nil {
		t.Fatalf("ObserveCheckUp(): %v", err)
	}

	expired, err := SweepChecks(runtime, st, at.Add(11*time.Second))
	if err != nil {
		t.Fatalf("SweepChecks() error = %v", err)
	}
	if expired != 1 {
		t.Fatalf("expired assignments = %d, want 1", expired)
	}
	if len(st.persistedWrites) != 1 {
		t.Fatalf("persisted writes = %d, want 1", len(st.persistedWrites))
	}
	if len(st.persistedWrites[0].CheckStateWrites) != 1 {
		t.Fatalf("check state writes = %d, want 1", len(st.persistedWrites[0].CheckStateWrites))
	}
	persisted := st.persistedWrites[0].CheckStateWrites[0]
	if persisted.State != "missing" {
		t.Fatalf("persisted state = %q, want missing", persisted.State)
	}
	if persisted.LastOutcome != "" {
		t.Fatalf("persisted last outcome = %q, want empty", persisted.LastOutcome)
	}

	check, err := runtime.CheckSnapshot("check-a", "probe-a")
	if err != nil {
		t.Fatalf("CheckSnapshot() error = %v", err)
	}
	if check.State != CheckStateMissing {
		t.Fatalf("check state = %q, want %q", check.State, CheckStateMissing)
	}
	if check.LastOutcome != "" {
		t.Fatalf("last outcome = %q, want empty", check.LastOutcome)
	}
	if check.StreakLen != 0 {
		t.Fatalf("streak = %d, want 0", check.StreakLen)
	}
	if !check.ExpiresAt.IsZero() {
		t.Fatalf("expiresAt = %s, want zero", check.ExpiresAt)
	}
}

func TestSweepChecksPersistsExpiredEvidenceWithoutIncidentTransition(t *testing.T) {
	st := &fakeSweeperStore{}
	runtime := NewRuntime([]string{"check-a"}, []string{"probe-a"})
	at := time.Date(2026, time.April, 8, 12, 0, 0, 0, time.UTC)
	expiresAt := at.Add(10 * time.Second)

	if _, err := runtime.ObserveCheckUp("check-a", "probe-a", at, &expiresAt); err != nil {
		t.Fatalf("ObserveCheckUp(): %v", err)
	}

	expired, err := SweepChecks(runtime, st, at.Add(11*time.Second))
	if err != nil {
		t.Fatalf("SweepChecks() error = %v", err)
	}
	if expired != 1 {
		t.Fatalf("expired assignments = %d, want 1", expired)
	}
	if len(st.persistedWrites) != 1 {
		t.Fatalf("persisted writes = %d, want 1", len(st.persistedWrites))
	}
	if st.persistedWrites[0].IncidentCheckID != "" {
		t.Fatalf("incident check id = %q, want empty", st.persistedWrites[0].IncidentCheckID)
	}
	if len(st.persistedWrites[0].CheckStateWrites) != 1 {
		t.Fatalf("check state writes = %d, want 1", len(st.persistedWrites[0].CheckStateWrites))
	}
	if st.persistedWrites[0].CheckStateWrites[0].LastOutcome != "" {
		t.Fatalf("persisted last outcome = %q, want empty", st.persistedWrites[0].CheckStateWrites[0].LastOutcome)
	}

	check, err := runtime.CheckSnapshot("check-a", "probe-a")
	if err != nil {
		t.Fatalf("CheckSnapshot() error = %v", err)
	}
	if check.State != CheckStateMissing {
		t.Fatalf("check state = %q, want %q", check.State, CheckStateMissing)
	}
	if check.LastOutcome != "" {
		t.Fatalf("last outcome = %q, want empty", check.LastOutcome)
	}
	if check.StreakLen != 0 {
		t.Fatalf("streak = %d, want 0", check.StreakLen)
	}
	if !check.ExpiresAt.IsZero() {
		t.Fatalf("expiresAt = %s, want zero", check.ExpiresAt)
	}
}

func TestSweepChecksDoesNotClearProbeErrorState(t *testing.T) {
	st := &fakeSweeperStore{}
	runtime := NewRuntime([]string{"check-a"}, []string{"probe-a"})
	at := time.Date(2026, time.April, 8, 12, 0, 0, 0, time.UTC)
	expiresAt := at.Add(10 * time.Second)

	if _, err := runtime.ObserveCheckUp("check-a", "probe-a", at, &expiresAt); err != nil {
		t.Fatalf("ObserveCheckUp first: %v", err)
	}
	if _, err := runtime.ObserveCheckUp("check-a", "probe-a", at.Add(time.Second), ptrTime(expiresAt.Add(time.Second))); err != nil {
		t.Fatalf("ObserveCheckUp second: %v", err)
	}
	if _, err := runtime.MarkProbeError("probe-a", "transport failed"); err != nil {
		t.Fatalf("MarkProbeError: %v", err)
	}

	expired, err := SweepChecks(runtime, st, at.Add(30*time.Second))
	if err != nil {
		t.Fatalf("SweepChecks() error = %v", err)
	}
	if expired != 0 {
		t.Fatalf("expired assignments = %d, want 0", expired)
	}
	if len(st.persistedWrites) != 0 {
		t.Fatalf("persisted writes = %d, want 0", len(st.persistedWrites))
	}

	check, err := runtime.CheckSnapshot("check-a", "probe-a")
	if err != nil {
		t.Fatalf("CheckSnapshot() error = %v", err)
	}
	if check.State != CheckStateError {
		t.Fatalf("check state = %q, want %q", check.State, CheckStateError)
	}
	if check.LastError != "transport failed" {
		t.Fatalf("last error = %q, want transport failed", check.LastError)
	}
}

func ptrTime(t time.Time) *time.Time {
	return &t
}
