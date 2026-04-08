package monitoring

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/tmater/wacht/internal/checks"
	"github.com/tmater/wacht/internal/store"
)

// fakeRecoveryStore is a minimal recoveryStore test double for boot and replay
// scenarios.
type fakeRecoveryStore struct {
	checks   []checks.Check
	probeIDs []string
	snapshot *store.MonitoringSnapshot
	journal  []store.MonitoringJournalRecord
}

func (f *fakeRecoveryStore) ListAllChecks() ([]checks.Check, error) {
	return append([]checks.Check(nil), f.checks...), nil
}

func (f *fakeRecoveryStore) ActiveProbeIDs() ([]string, error) {
	return append([]string(nil), f.probeIDs...), nil
}

func (f *fakeRecoveryStore) LatestMonitoringSnapshot() (*store.MonitoringSnapshot, error) {
	if f.snapshot == nil {
		return nil, nil
	}

	snapshot := *f.snapshot
	snapshot.Payload = append(json.RawMessage(nil), snapshot.Payload...)
	return &snapshot, nil
}

func (f *fakeRecoveryStore) MonitoringJournalAfter(afterID int64) ([]store.MonitoringJournalRecord, error) {
	records := make([]store.MonitoringJournalRecord, 0, len(f.journal))
	for _, record := range f.journal {
		if record.ID <= afterID {
			continue
		}
		if record.ExpiresAt != nil {
			expiresAt := *record.ExpiresAt
			record.ExpiresAt = &expiresAt
		}
		records = append(records, record)
	}
	return records, nil
}

func TestLoadRuntimeUsesMetadataDefaultsWithoutRecoveryData(t *testing.T) {
	recovered, err := LoadRuntime(&fakeRecoveryStore{
		checks: []checks.Check{
			checks.NewCheck("check-a", "http", "https://a.example.com", "", 30),
			checks.NewCheck("check-b", "http", "https://b.example.com", "", 30),
		},
		probeIDs: []string{"probe-a", "probe-b"},
	})
	if err != nil {
		t.Fatalf("LoadRuntime: %v", err)
	}

	probe, err := recovered.ProbeSnapshot("probe-a")
	if err != nil {
		t.Fatalf("ProbeSnapshot: %v", err)
	}
	if probe.State != ProbeStateOffline {
		t.Fatalf("probe state = %q, want %q", probe.State, ProbeStateOffline)
	}

	check, err := recovered.CheckSnapshot("check-a", "probe-a")
	if err != nil {
		t.Fatalf("CheckSnapshot: %v", err)
	}
	if check.State != CheckStateMissing {
		t.Fatalf("check state = %q, want %q", check.State, CheckStateMissing)
	}

	quorum, err := recovered.QuorumSnapshot("check-b")
	if err != nil {
		t.Fatalf("QuorumSnapshot: %v", err)
	}
	if quorum.State != QuorumStatePending {
		t.Fatalf("quorum state = %q, want %q", quorum.State, QuorumStatePending)
	}
}

func TestLoadRuntimeRestoresSnapshotAndReplaysJournalTail(t *testing.T) {
	base := NewRuntime([]string{"check-a"}, []string{"probe-a", "probe-b", "probe-c"})

	firstAt := time.Date(2026, time.April, 8, 7, 0, 0, 0, time.UTC)
	firstExpiry := firstAt.Add(30 * time.Second)
	if _, err := base.ReceiveHeartbeat("probe-a", firstAt); err != nil {
		t.Fatalf("ReceiveHeartbeat probe-a: %v", err)
	}
	if _, err := base.ObserveCheckDown("check-a", "probe-a", firstAt, &firstExpiry, "timeout"); err != nil {
		t.Fatalf("ObserveCheckDown probe-a: %v", err)
	}
	if _, err := base.ObserveCheckDown("check-a", "probe-b", firstAt, &firstExpiry, "timeout"); err != nil {
		t.Fatalf("ObserveCheckDown probe-b: %v", err)
	}
	secondAt := firstAt.Add(time.Second)
	secondExpiry := secondAt.Add(30 * time.Second)
	if _, err := base.ObserveCheckDown("check-a", "probe-a", secondAt, &secondExpiry, "timeout"); err != nil {
		t.Fatalf("ObserveCheckDown probe-a second: %v", err)
	}
	if _, err := base.ObserveCheckDown("check-a", "probe-b", secondAt, &secondExpiry, "timeout"); err != nil {
		t.Fatalf("ObserveCheckDown probe-b second: %v", err)
	}

	snapshot, err := base.RecoverySnapshot(7, firstAt.Add(time.Minute))
	if err != nil {
		t.Fatalf("RecoverySnapshot: %v", err)
	}

	replayAt := firstAt.Add(2 * time.Minute)
	replayExpiry := replayAt.Add(30 * time.Second)
	recovered, err := LoadRuntime(&fakeRecoveryStore{
		checks: []checks.Check{
			checks.NewCheck("check-a", "http", "https://a.example.com", "", 30),
		},
		probeIDs: []string{"probe-a", "probe-b", "probe-c"},
		snapshot: &snapshot,
		journal: []store.MonitoringJournalRecord{
			newJournalRecord(store.MonitoringJournalRecord{
				ID:         8,
				Kind:       string(ProbeTriggerReceiveHeartbeat),
				ProbeID:    "probe-b",
				OccurredAt: replayAt,
				RecordedAt: replayAt,
			}),
			newJournalRecord(store.MonitoringJournalRecord{
				ID:         9,
				Kind:       string(CheckTriggerObserveUp),
				CheckID:    "check-a",
				ProbeID:    "probe-a",
				ExpiresAt:  &replayExpiry,
				OccurredAt: replayAt,
				RecordedAt: replayAt,
			}),
			newJournalRecord(store.MonitoringJournalRecord{
				ID:         10,
				Kind:       string(CheckTriggerObserveUp),
				CheckID:    "check-a",
				ProbeID:    "probe-b",
				ExpiresAt:  &replayExpiry,
				OccurredAt: replayAt,
				RecordedAt: replayAt,
			}),
			newJournalRecord(store.MonitoringJournalRecord{
				ID:         11,
				Kind:       string(CheckTriggerObserveUp),
				CheckID:    "check-a",
				ProbeID:    "probe-a",
				ExpiresAt:  &replayExpiry,
				OccurredAt: replayAt.Add(time.Second),
				RecordedAt: replayAt.Add(time.Second),
			}),
			newJournalRecord(store.MonitoringJournalRecord{
				ID:         12,
				Kind:       string(CheckTriggerObserveUp),
				CheckID:    "check-a",
				ProbeID:    "probe-b",
				ExpiresAt:  &replayExpiry,
				OccurredAt: replayAt.Add(time.Second),
				RecordedAt: replayAt.Add(time.Second),
			}),
		},
	})
	if err != nil {
		t.Fatalf("LoadRuntime: %v", err)
	}

	probeA, err := recovered.ProbeSnapshot("probe-a")
	if err != nil {
		t.Fatalf("ProbeSnapshot probe-a: %v", err)
	}
	if probeA.LastHeartbeatAt == nil || !probeA.LastHeartbeatAt.Equal(firstAt) {
		t.Fatalf("probe-a last heartbeat = %v, want %v", probeA.LastHeartbeatAt, firstAt)
	}

	probeB, err := recovered.ProbeSnapshot("probe-b")
	if err != nil {
		t.Fatalf("ProbeSnapshot probe-b: %v", err)
	}
	if probeB.State != ProbeStateOnline {
		t.Fatalf("probe-b state = %q, want %q", probeB.State, ProbeStateOnline)
	}
	if probeB.LastHeartbeatAt == nil || !probeB.LastHeartbeatAt.Equal(replayAt) {
		t.Fatalf("probe-b last heartbeat = %v, want %v", probeB.LastHeartbeatAt, replayAt)
	}

	check, err := recovered.CheckSnapshot("check-a", "probe-b")
	if err != nil {
		t.Fatalf("CheckSnapshot: %v", err)
	}
	if check.State != CheckStateUp {
		t.Fatalf("check state = %q, want %q", check.State, CheckStateUp)
	}
	if !check.LastResultAt.Equal(replayAt.Add(time.Second)) {
		t.Fatalf("check LastResultAt = %s, want %s", check.LastResultAt, replayAt.Add(time.Second))
	}

	quorum, err := recovered.QuorumSnapshot("check-a")
	if err != nil {
		t.Fatalf("QuorumSnapshot: %v", err)
	}
	if quorum.State != QuorumStateUp {
		t.Fatalf("quorum state = %q, want %q", quorum.State, QuorumStateUp)
	}
	if quorum.LastStableState != QuorumStateUp {
		t.Fatalf("last stable state = %q, want %q", quorum.LastStableState, QuorumStateUp)
	}
}

func TestLoadRuntimeIgnoresRecoveryDataForRemovedMetadata(t *testing.T) {
	recovered, err := LoadRuntime(&fakeRecoveryStore{
		checks:   []checks.Check{checks.NewCheck("check-a", "http", "https://a.example.com", "", 30)},
		probeIDs: []string{"probe-a"},
		journal: []store.MonitoringJournalRecord{
			newJournalRecord(store.MonitoringJournalRecord{
				ID:         1,
				Kind:       string(ProbeTriggerReceiveHeartbeat),
				ProbeID:    "missing-probe",
				OccurredAt: time.Date(2026, time.April, 8, 7, 0, 0, 0, time.UTC),
				RecordedAt: time.Date(2026, time.April, 8, 7, 0, 0, 0, time.UTC),
			}),
			newJournalRecord(store.MonitoringJournalRecord{
				ID:         2,
				Kind:       string(CheckTriggerObserveDown),
				CheckID:    "missing-check",
				ProbeID:    "probe-a",
				Message:    "timeout",
				OccurredAt: time.Date(2026, time.April, 8, 7, 1, 0, 0, time.UTC),
				RecordedAt: time.Date(2026, time.April, 8, 7, 1, 0, 0, time.UTC),
			}),
		},
	})
	if err != nil {
		t.Fatalf("LoadRuntime: %v", err)
	}

	probe, err := recovered.ProbeSnapshot("probe-a")
	if err != nil {
		t.Fatalf("ProbeSnapshot: %v", err)
	}
	if probe.State != ProbeStateOffline {
		t.Fatalf("probe state = %q, want %q", probe.State, ProbeStateOffline)
	}
}

func TestLoadRuntimeRejectsUnknownJournalKinds(t *testing.T) {
	_, err := LoadRuntime(&fakeRecoveryStore{
		checks:   []checks.Check{checks.NewCheck("check-a", "http", "https://a.example.com", "", 30)},
		probeIDs: []string{"probe-a"},
		journal: []store.MonitoringJournalRecord{
			{
				ID:         1,
				Kind:       "bogus",
				OccurredAt: time.Date(2026, time.April, 8, 7, 0, 0, 0, time.UTC),
				RecordedAt: time.Date(2026, time.April, 8, 7, 0, 0, 0, time.UTC),
			},
		},
	})
	if !errors.Is(err, ErrUnknownRecoveryJournalKind) {
		t.Fatalf("LoadRuntime err = %v, want %v", err, ErrUnknownRecoveryJournalKind)
	}
}

func newJournalRecord(record store.MonitoringJournalRecord) store.MonitoringJournalRecord {
	if record.ExpiresAt != nil {
		expiresAt := *record.ExpiresAt
		record.ExpiresAt = &expiresAt
	}
	return record
}
