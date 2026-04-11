package store

import (
	"errors"
	"testing"
	"time"
)

func TestPersistMonitoringWriteUpsertsCheckStateAndListsRecoverySnapshots(t *testing.T) {
	s := newTestStore(t)

	if err := s.SeedProbes([]ProbeSeed{
		{ProbeID: "probe-a", Secret: "secret-a"},
		{ProbeID: "probe-b", Secret: "secret-b"},
	}); err != nil {
		t.Fatalf("SeedProbes: %v", err)
	}

	user, err := s.CreateUser("monitoring-state@example.com", "pass", false)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if err := s.CreateCheck(testCheck("check-1", "http", "https://example.com"), user.ID); err != nil {
		t.Fatalf("CreateCheck: %v", err)
	}

	firstAt := time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC)
	secondAt := firstAt.Add(2 * time.Minute)
	if _, err := s.PersistMonitoringWrite(MonitoringWrite{
		CheckStateWrites: []CheckStateWrite{
			{
				CheckID:      "check-1",
				ProbeID:      "probe-b",
				LastResultAt: secondAt,
				LastOutcome:  "up",
				StreakLen:    2,
				ExpiresAt:    secondAt.Add(30 * time.Second),
				State:        "up",
			},
			{
				CheckID:      "check-1",
				ProbeID:      "probe-a",
				LastResultAt: firstAt,
				LastOutcome:  "down",
				StreakLen:    1,
				ExpiresAt:    firstAt.Add(30 * time.Second),
				State:        "missing",
				LastError:    "timeout",
			},
		},
	}); err != nil {
		t.Fatalf("PersistMonitoringWrite first: %v", err)
	}

	if _, err := s.PersistMonitoringWrite(MonitoringWrite{
		CheckStateWrites: []CheckStateWrite{{
			CheckID:      "check-1",
			ProbeID:      "probe-a",
			LastResultAt: secondAt,
			LastOutcome:  "down",
			StreakLen:    2,
			ExpiresAt:    secondAt.Add(30 * time.Second),
			State:        "down",
			LastError:    "timeout",
		}},
	}); err != nil {
		t.Fatalf("PersistMonitoringWrite second: %v", err)
	}

	probes, err := s.ActiveProbeStates()
	if err != nil {
		t.Fatalf("ActiveProbeStates: %v", err)
	}
	if len(probes) != 2 {
		t.Fatalf("active probes = %d, want 2", len(probes))
	}
	if probes[0].ProbeID != "probe-a" || probes[0].LastSeenAt != nil {
		t.Fatalf("probe-a = %#v, want nil last_seen", probes[0])
	}

	states, err := s.PersistedCheckStates()
	if err != nil {
		t.Fatalf("PersistedCheckStates: %v", err)
	}
	if len(states) != 2 {
		t.Fatalf("persisted check states = %d, want 2", len(states))
	}
	if states[0].ProbeID != "probe-a" || states[0].State != "down" || states[0].StreakLen != 2 {
		t.Fatalf("states[0] = %#v, want probe-a/down/streak 2", states[0])
	}
	if !states[0].LastResultAt.Equal(secondAt) {
		t.Fatalf("states[0].LastResultAt = %s, want %s", states[0].LastResultAt, secondAt)
	}
	if states[1].ProbeID != "probe-b" || states[1].State != "up" {
		t.Fatalf("states[1] = %#v, want probe-b/up", states[1])
	}
}

// TestPersistMonitoringWriteCommitsCurrentStateAndIncidentAtomically verifies
// the existing atomic write boundary for current-state rows, probe metadata,
// and incident writes.
func TestPersistMonitoringWriteCommitsCurrentStateAndIncidentAtomically(t *testing.T) {
	s := newTestStore(t)

	user, err := s.CreateUser("monitoring-write@example.com", "pass", false)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if err := s.CreateCheck(testCheckWithWebhook("check-1", "http", "https://example.com", "https://hooks.example.com/wacht", 30), user.ID); err != nil {
		t.Fatalf("CreateCheck: %v", err)
	}

	result, err := s.PersistMonitoringWrite(MonitoringWrite{
		CheckStateWrites: []CheckStateWrite{
			{
				CheckID:      "check-1",
				ProbeID:      "probe-a",
				LastResultAt: time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC),
				LastOutcome:  "down",
				StreakLen:    2,
				ExpiresAt:    time.Date(2026, time.January, 2, 3, 5, 5, 0, time.UTC),
				State:        "down",
				LastError:    "timeout",
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

	if len(result.CheckStateWrites) != 1 {
		t.Fatalf("expected 1 check state write, got %d", len(result.CheckStateWrites))
	}

	var stateCount int
	if err := s.db.QueryRow(`SELECT COUNT(1) FROM check_probe_state`).Scan(&stateCount); err != nil {
		t.Fatalf("count check_probe_state: %v", err)
	}
	if stateCount != 1 {
		t.Fatalf("expected 1 check state row, got %d", stateCount)
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

	checkIDs, err := s.OpenIncidentCheckIDs()
	if err != nil {
		t.Fatalf("OpenIncidentCheckIDs: %v", err)
	}
	if len(checkIDs) != 1 || checkIDs[0] != "check-1" {
		t.Fatalf("OpenIncidentCheckIDs = %#v, want [check-1]", checkIDs)
	}
}

// TestPersistMonitoringWriteUpdatesProbeHeartbeatAtomically verifies that the
// heartbeat write and persisted probe metadata update commit together.
func TestPersistMonitoringWriteUpdatesProbeHeartbeatAtomically(t *testing.T) {
	s := newTestStore(t)

	if err := s.SeedProbes([]ProbeSeed{{ProbeID: "probe-a", Secret: "secret-a"}}); err != nil {
		t.Fatalf("SeedProbes: %v", err)
	}

	heartbeatAt := time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC)
	write, err := s.PersistMonitoringWrite(MonitoringWrite{
		ProbeHeartbeatID: "probe-a",
		ProbeHeartbeatAt: heartbeatAt,
	})
	if err != nil {
		t.Fatalf("PersistMonitoringWrite: %v", err)
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
		CheckStateWrites: []CheckStateWrite{
			{
				CheckID:      "check-1",
				ProbeID:      "probe-a",
				LastResultAt: time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC),
				LastOutcome:  "down",
				StreakLen:    2,
				ExpiresAt:    time.Date(2026, time.January, 2, 3, 5, 5, 0, time.UTC),
				State:        "down",
				LastError:    "timeout",
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

	var stateCount int
	if err := s.db.QueryRow(`SELECT COUNT(1) FROM check_probe_state`).Scan(&stateCount); err != nil {
		t.Fatalf("count check_probe_state: %v", err)
	}
	if stateCount != 0 {
		t.Fatalf("expected rollback to leave 0 check state rows, got %d", stateCount)
	}

	var incidentCount int
	if err := s.db.QueryRow(`SELECT COUNT(1) FROM incidents`).Scan(&incidentCount); err != nil {
		t.Fatalf("count incidents: %v", err)
	}
	if incidentCount != 0 {
		t.Fatalf("expected rollback to leave 0 incidents, got %d", incidentCount)
	}
}

// TestPersistMonitoringWriteRejectsIncompleteInputs verifies input validation
// for incomplete incident-only, heartbeat-only, and check-state writes.
func TestPersistMonitoringWriteRejectsIncompleteInputs(t *testing.T) {
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

	_, err = s.PersistMonitoringWrite(MonitoringWrite{
		CheckStateWrites: []CheckStateWrite{{
			CheckID:      "check-1",
			LastResultAt: time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC),
			LastOutcome:  "up",
			StreakLen:    1,
			ExpiresAt:    time.Date(2026, time.January, 2, 3, 5, 5, 0, time.UTC),
			State:        "missing",
		}},
	})
	if !errors.Is(err, ErrInvalidMonitoringCheckStateWrite) {
		t.Fatalf("CheckStateWrites-only error = %v, want ErrInvalidMonitoringCheckStateWrite", err)
	}
}
