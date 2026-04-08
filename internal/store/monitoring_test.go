package store

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestAppendMonitoringJournalAndLoadTail(t *testing.T) {
	s := newTestStore(t)

	firstOccurredAt := time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC)
	secondOccurredAt := firstOccurredAt.Add(2 * time.Minute)

	first, err := s.AppendMonitoringJournal(MonitoringJournalRecord{
		Kind:       "receive_heartbeat",
		ProbeID:    "probe-a",
		OccurredAt: firstOccurredAt,
	})
	if err != nil {
		t.Fatalf("AppendMonitoringJournal first: %v", err)
	}

	second, err := s.AppendMonitoringJournal(MonitoringJournalRecord{
		Kind:       "observe_down",
		CheckID:    "check-1",
		ProbeID:    "probe-b",
		Message:    "timeout",
		OccurredAt: secondOccurredAt,
	})
	if err != nil {
		t.Fatalf("AppendMonitoringJournal second: %v", err)
	}

	tail, err := s.MonitoringJournalAfter(first.ID)
	if err != nil {
		t.Fatalf("MonitoringJournalAfter: %v", err)
	}
	if len(tail) != 1 {
		t.Fatalf("expected 1 tail record, got %d", len(tail))
	}
	if tail[0].ID != second.ID {
		t.Fatalf("tail[0].ID = %d, want %d", tail[0].ID, second.ID)
	}
	if tail[0].Kind != second.Kind {
		t.Fatalf("tail[0].Kind = %q, want %q", tail[0].Kind, second.Kind)
	}
	if tail[0].CheckID != second.CheckID {
		t.Fatalf("tail[0].CheckID = %q, want %q", tail[0].CheckID, second.CheckID)
	}
	if tail[0].ProbeID != second.ProbeID {
		t.Fatalf("tail[0].ProbeID = %q, want %q", tail[0].ProbeID, second.ProbeID)
	}
	if tail[0].Message != second.Message {
		t.Fatalf("tail[0].Message = %q, want %q", tail[0].Message, second.Message)
	}
	if !tail[0].OccurredAt.Equal(secondOccurredAt) {
		t.Fatalf("tail[0].OccurredAt = %s, want %s", tail[0].OccurredAt, secondOccurredAt)
	}
	if tail[0].RecordedAt.IsZero() {
		t.Fatal("expected tail record to have RecordedAt")
	}
}

func TestLatestMonitoringSnapshotReturnsNewest(t *testing.T) {
	s := newTestStore(t)

	journal, err := s.AppendMonitoringJournal(MonitoringJournalRecord{
		Kind:       "observe_up",
		CheckID:    "check-1",
		ProbeID:    "probe-a",
		OccurredAt: time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("AppendMonitoringJournal: %v", err)
	}

	if _, err := s.AppendMonitoringSnapshot(MonitoringSnapshot{
		LastJournalID: 0,
		Payload:       json.RawMessage(`{"runtime":"older"}`),
		CapturedAt:    time.Date(2026, time.January, 2, 3, 5, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("AppendMonitoringSnapshot older: %v", err)
	}

	newest, err := s.AppendMonitoringSnapshot(MonitoringSnapshot{
		LastJournalID: journal.ID,
		Payload:       json.RawMessage(`{"runtime":"newer"}`),
		CapturedAt:    time.Date(2026, time.January, 2, 3, 6, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("AppendMonitoringSnapshot newer: %v", err)
	}

	got, err := s.LatestMonitoringSnapshot()
	if err != nil {
		t.Fatalf("LatestMonitoringSnapshot: %v", err)
	}
	if got == nil {
		t.Fatal("expected snapshot, got nil")
	}
	if got.ID != newest.ID {
		t.Fatalf("got snapshot ID %d, want %d", got.ID, newest.ID)
	}
	if got.LastJournalID != journal.ID {
		t.Fatalf("got LastJournalID %d, want %d", got.LastJournalID, journal.ID)
	}
	assertJSONEqual(t, got.Payload, newest.Payload)
	if !got.CapturedAt.Equal(newest.CapturedAt) {
		t.Fatalf("got CapturedAt %s, want %s", got.CapturedAt, newest.CapturedAt)
	}
}

func TestPersistMonitoringWriteCommitsRecoveryAndIncidentAtomically(t *testing.T) {
	s := newTestStore(t)

	user, err := s.CreateUser("monitoring-write@example.com", "pass", false)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if err := s.CreateCheck(testCheckWithWebhook("check-1", "http", "https://example.com", "https://hooks.example.com/wacht", 30), user.ID); err != nil {
		t.Fatalf("CreateCheck: %v", err)
	}

	result, incidentApplied, err := s.PersistMonitoringWrite(MonitoringWrite{
		JournalRecords: []MonitoringJournalRecord{
			{
				Kind:       "observe_down",
				CheckID:    "check-1",
				ProbeID:    "probe-a",
				Message:    "timeout",
				OccurredAt: time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC),
			},
		},
		Snapshot: &MonitoringSnapshot{
			LastJournalID: 0,
			Payload:       json.RawMessage(`{"runtime":"captured"}`),
			CapturedAt:    time.Date(2026, time.January, 2, 3, 4, 6, 0, time.UTC),
		},
		IncidentCheckID: "check-1",
		IncidentNotification: &NotificationRequest{
			WebhookURL: "https://hooks.example.com/wacht",
			Payload:    []byte(`{"status":"down"}`),
		},
	})
	if err != nil {
		t.Fatalf("PersistMonitoringWrite: %v", err)
	}

	if len(result.JournalRecords) != 1 {
		t.Fatalf("expected 1 journal record, got %d", len(result.JournalRecords))
	}
	if result.Snapshot == nil {
		t.Fatal("expected snapshot result")
	}
	if result.Snapshot.LastJournalID != result.JournalRecords[0].ID {
		t.Fatalf("expected snapshot LastJournalID %d, got %d", result.JournalRecords[0].ID, result.Snapshot.LastJournalID)
	}
	if !incidentApplied {
		t.Fatal("expected incidentApplied=true")
	}

	var journalCount int
	if err := s.db.QueryRow(`SELECT COUNT(1) FROM monitoring_journal`).Scan(&journalCount); err != nil {
		t.Fatalf("count monitoring_journal: %v", err)
	}
	if journalCount != 1 {
		t.Fatalf("expected 1 journal row, got %d", journalCount)
	}

	var snapshotCount int
	if err := s.db.QueryRow(`SELECT COUNT(1) FROM monitoring_snapshots`).Scan(&snapshotCount); err != nil {
		t.Fatalf("count monitoring_snapshots: %v", err)
	}
	if snapshotCount != 1 {
		t.Fatalf("expected 1 snapshot row, got %d", snapshotCount)
	}

	var openIncidents int
	if err := s.db.QueryRow(`
		SELECT COUNT(1)
		FROM incidents i
		JOIN checks c ON c.uid = i.check_uid
		WHERE c.id = $1
		  AND i.resolved_at IS NULL
	`, "check-1").Scan(&openIncidents); err != nil {
		t.Fatalf("count open incidents: %v", err)
	}
	if openIncidents != 1 {
		t.Fatalf("expected 1 open incident, got %d", openIncidents)
	}
}

func TestPersistMonitoringWriteRollsBackOnInvalidIncidentAction(t *testing.T) {
	s := newTestStore(t)

	user, err := s.CreateUser("monitoring-rollback@example.com", "pass", false)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if err := s.CreateCheck(testCheck("check-1", "http", "https://example.com"), user.ID); err != nil {
		t.Fatalf("CreateCheck: %v", err)
	}

	_, _, err = s.PersistMonitoringWrite(MonitoringWrite{
		JournalRecords: []MonitoringJournalRecord{
			{
				Kind:       "observe_down",
				CheckID:    "check-1",
				ProbeID:    "probe-a",
				Message:    "timeout",
				OccurredAt: time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC),
			},
		},
		IncidentNotification: &NotificationRequest{
			WebhookURL: "https://hooks.example.com/wacht",
			Payload:    []byte(`{"status":"down"}`),
		},
	})
	if !errors.Is(err, ErrInvalidMonitoringIncidentWrite) {
		t.Fatalf("PersistMonitoringWrite error = %v, want ErrInvalidMonitoringIncidentWrite", err)
	}

	var journalCount int
	if err := s.db.QueryRow(`SELECT COUNT(1) FROM monitoring_journal`).Scan(&journalCount); err != nil {
		t.Fatalf("count monitoring_journal: %v", err)
	}
	if journalCount != 0 {
		t.Fatalf("expected rollback to leave 0 journal rows, got %d", journalCount)
	}

	var incidentCount int
	if err := s.db.QueryRow(`SELECT COUNT(1) FROM incidents`).Scan(&incidentCount); err != nil {
		t.Fatalf("count incidents: %v", err)
	}
	if incidentCount != 0 {
		t.Fatalf("expected rollback to leave 0 incidents, got %d", incidentCount)
	}
}

func TestPersistMonitoringWriteRejectsIncidentOnlyNoopInputs(t *testing.T) {
	s := newTestStore(t)

	_, _, err := s.PersistMonitoringWrite(MonitoringWrite{
		ResolveIncident: true,
	})
	if !errors.Is(err, ErrInvalidMonitoringIncidentWrite) {
		t.Fatalf("ResolveIncident-only error = %v, want ErrInvalidMonitoringIncidentWrite", err)
	}

	_, _, err = s.PersistMonitoringWrite(MonitoringWrite{
		IncidentNotification: &NotificationRequest{
			WebhookURL: "https://hooks.example.com/wacht",
			Payload:    []byte(`{"status":"down"}`),
		},
	})
	if !errors.Is(err, ErrInvalidMonitoringIncidentWrite) {
		t.Fatalf("IncidentNotification-only error = %v, want ErrInvalidMonitoringIncidentWrite", err)
	}
}

func assertJSONEqual(t *testing.T, got, want json.RawMessage) {
	t.Helper()

	var gotValue any
	if err := json.Unmarshal(got, &gotValue); err != nil {
		t.Fatalf("unmarshal got JSON: %v", err)
	}

	var wantValue any
	if err := json.Unmarshal(want, &wantValue); err != nil {
		t.Fatalf("unmarshal want JSON: %v", err)
	}

	if !reflect.DeepEqual(gotValue, wantValue) {
		t.Fatalf("got JSON %s, want %s", got, want)
	}
}
