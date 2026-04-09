package store

import (
	"errors"
	"testing"
	"time"
)

// TestPersistMonitoringWriteAndLoadTail verifies ordered journal replay reads
// after appending runtime recovery records through the live write path.
func TestPersistMonitoringWriteAndLoadTail(t *testing.T) {
	s := newTestStore(t)

	firstOccurredAt := time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC)
	secondOccurredAt := firstOccurredAt.Add(2 * time.Minute)

	firstWrite, err := s.PersistMonitoringWrite(MonitoringWrite{
		JournalRecords: []MonitoringJournalRecord{{
			Kind:       "receive_heartbeat",
			ProbeID:    "probe-a",
			OccurredAt: firstOccurredAt,
		}},
	})
	if err != nil {
		t.Fatalf("PersistMonitoringWrite first: %v", err)
	}
	first := firstWrite.JournalRecords[0]

	secondWrite, err := s.PersistMonitoringWrite(MonitoringWrite{
		JournalRecords: []MonitoringJournalRecord{{
			Kind:       "observe_down",
			CheckID:    "check-1",
			ProbeID:    "probe-b",
			Message:    "timeout",
			OccurredAt: secondOccurredAt,
		}},
	})
	if err != nil {
		t.Fatalf("PersistMonitoringWrite second: %v", err)
	}
	second := secondWrite.JournalRecords[0]

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

// TestPersistMonitoringWriteCommitsRecoveryAndIncidentAtomically verifies the
// existing atomic write boundary for journal, probe metadata, and incident
// writes.
func TestPersistMonitoringWriteCommitsRecoveryAndIncidentAtomically(t *testing.T) {
	s := newTestStore(t)

	user, err := s.CreateUser("monitoring-write@example.com", "pass", false)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if err := s.CreateCheck(testCheckWithWebhook("check-1", "http", "https://example.com", "https://hooks.example.com/wacht", 30), user.ID); err != nil {
		t.Fatalf("CreateCheck: %v", err)
	}

	result, err := s.PersistMonitoringWrite(MonitoringWrite{
		JournalRecords: []MonitoringJournalRecord{
			{
				Kind:       "observe_down",
				CheckID:    "check-1",
				ProbeID:    "probe-a",
				Message:    "timeout",
				OccurredAt: time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC),
			},
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

	var journalCount int
	if err := s.db.QueryRow(`SELECT COUNT(1) FROM monitoring_journal`).Scan(&journalCount); err != nil {
		t.Fatalf("count monitoring_journal: %v", err)
	}
	if journalCount != 1 {
		t.Fatalf("expected 1 journal row, got %d", journalCount)
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

// TestPersistMonitoringWriteUpdatesProbeHeartbeatAtomically verifies that the
// heartbeat journal record and persisted probe metadata update commit together.
func TestPersistMonitoringWriteUpdatesProbeHeartbeatAtomically(t *testing.T) {
	s := newTestStore(t)

	if err := s.SeedProbes([]ProbeSeed{{ProbeID: "probe-a", Secret: "secret-a"}}); err != nil {
		t.Fatalf("SeedProbes: %v", err)
	}

	heartbeatAt := time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC)
	write, err := s.PersistMonitoringWrite(MonitoringWrite{
		JournalRecords: []MonitoringJournalRecord{
			{
				Kind:       "receive_heartbeat",
				ProbeID:    "probe-a",
				OccurredAt: heartbeatAt,
			},
		},
		ProbeHeartbeatID: "probe-a",
		ProbeHeartbeatAt: heartbeatAt,
	})
	if err != nil {
		t.Fatalf("PersistMonitoringWrite: %v", err)
	}
	if len(write.JournalRecords) != 1 {
		t.Fatalf("journal records = %d, want 1", len(write.JournalRecords))
	}
	if !write.ProbeHeartbeatAt.Equal(heartbeatAt) {
		t.Fatalf("ProbeHeartbeatAt = %s, want %s", write.ProbeHeartbeatAt, heartbeatAt)
	}

	probe, err := s.AuthenticateProbe("probe-a", "secret-a")
	if err != nil {
		t.Fatalf("AuthenticateProbe: %v", err)
	}
	if probe == nil {
		t.Fatal("expected probe, got nil")
	}
	if probe.LastSeenAt == nil || !probe.LastSeenAt.Equal(heartbeatAt) {
		t.Fatalf("LastSeenAt = %v, want %v", probe.LastSeenAt, heartbeatAt)
	}
}

// TestPersistMonitoringWriteRollsBackOnInvalidIncidentAction verifies that an
// invalid incident side effect aborts the full monitoring write.
func TestPersistMonitoringWriteRollsBackOnInvalidIncidentAction(t *testing.T) {
	s := newTestStore(t)

	user, err := s.CreateUser("monitoring-rollback@example.com", "pass", false)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if err := s.CreateCheck(testCheck("check-1", "http", "https://example.com"), user.ID); err != nil {
		t.Fatalf("CreateCheck: %v", err)
	}

	_, err = s.PersistMonitoringWrite(MonitoringWrite{
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

// TestPersistMonitoringWriteRejectsIncidentOnlyNoopInputs verifies input
// validation for incomplete incident-only or heartbeat-only writes.
func TestPersistMonitoringWriteRejectsIncidentOnlyNoopInputs(t *testing.T) {
	s := newTestStore(t)

	_, err := s.PersistMonitoringWrite(MonitoringWrite{
		ResolveIncident: true,
	})
	if !errors.Is(err, ErrInvalidMonitoringIncidentWrite) {
		t.Fatalf("ResolveIncident-only error = %v, want ErrInvalidMonitoringIncidentWrite", err)
	}

	_, err = s.PersistMonitoringWrite(MonitoringWrite{
		IncidentNotification: &NotificationRequest{
			WebhookURL: "https://hooks.example.com/wacht",
			Payload:    []byte(`{"status":"down"}`),
		},
	})
	if !errors.Is(err, ErrInvalidMonitoringIncidentWrite) {
		t.Fatalf("IncidentNotification-only error = %v, want ErrInvalidMonitoringIncidentWrite", err)
	}

	_, err = s.PersistMonitoringWrite(MonitoringWrite{
		ProbeHeartbeatAt: time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC),
	})
	if !errors.Is(err, ErrInvalidMonitoringProbeWrite) {
		t.Fatalf("ProbeHeartbeatAt-only error = %v, want ErrInvalidMonitoringProbeWrite", err)
	}
}
