package store

import (
	"testing"

	"github.com/tmater/wacht/internal/checks"
)

func TestSeedChecks_SkipsExisting(t *testing.T) {
	s := newTestStore(t)

	checks := []checks.Check{testCheck("c1", "http", "https://a.com")}
	if err := s.SeedChecks(checks, 0); err != nil {
		t.Fatalf("SeedChecks: %v", err)
	}

	// Seed again with a different target — existing row must be unchanged.
	checks[0].Target = "https://b.com"
	if err := s.SeedChecks(checks, 0); err != nil {
		t.Fatalf("SeedChecks second call: %v", err)
	}

	c, err := s.GetCheck("c1")
	if err != nil {
		t.Fatalf("GetCheck: %v", err)
	}
	if c.Target != "https://a.com" {
		t.Errorf("expected original target to be preserved, got %q", c.Target)
	}
}

func TestCheckCRUD(t *testing.T) {
	s := newTestStore(t)

	// Create a user to own the checks.
	user, err := s.CreateUser("test@example.com", "password", false)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	// Create
	c := testCheck("c1", "http", "https://example.com")
	if err := s.CreateCheck(c, user.ID); err != nil {
		t.Fatalf("CreateCheck: %v", err)
	}

	// Get
	got, err := s.GetCheck("c1")
	if err != nil {
		t.Fatalf("GetCheck: %v", err)
	}
	if got == nil || got.Target != "https://example.com" {
		t.Fatalf("GetCheck: expected check, got %v", got)
	}

	// List
	all, err := s.ListChecks(user.ID)
	if err != nil {
		t.Fatalf("ListChecks: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("ListChecks: expected 1, got %d", len(all))
	}

	// Update
	c.Target = "https://updated.com"
	c.Webhook = "https://hooks.example.com"
	if err := s.UpdateCheck(c, user.ID); err != nil {
		t.Fatalf("UpdateCheck: %v", err)
	}
	got, _ = s.GetCheck("c1")
	if got.Target != "https://updated.com" || got.Webhook != "https://hooks.example.com" {
		t.Errorf("UpdateCheck: unexpected values %+v", got)
	}

	// Delete
	if err := s.DeleteCheck("c1", user.ID); err != nil {
		t.Fatalf("DeleteCheck: %v", err)
	}
	got, err = s.GetCheck("c1")
	if err != nil {
		t.Fatalf("GetCheck after delete: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil after delete, got %+v", got)
	}
}

func TestCheckCrossUserIsolation(t *testing.T) {
	s := newTestStore(t)

	alice, err := s.CreateUser("alice@example.com", "pass", false)
	if err != nil {
		t.Fatalf("CreateUser alice: %v", err)
	}
	bob, err := s.CreateUser("bob@example.com", "pass", false)
	if err != nil {
		t.Fatalf("CreateUser bob: %v", err)
	}

	aliceCheck := testCheck("alice-check", "http", "https://alice.com")
	if err := s.CreateCheck(aliceCheck, alice.ID); err != nil {
		t.Fatalf("CreateCheck alice: %v", err)
	}

	// Bob's list must not include Alice's check.
	bobChecks, err := s.ListChecks(bob.ID)
	if err != nil {
		t.Fatalf("ListChecks bob: %v", err)
	}
	if len(bobChecks) != 0 {
		t.Errorf("bob sees %d checks, expected 0", len(bobChecks))
	}

	// Bob must not be able to delete Alice's check.
	if err := s.DeleteCheck("alice-check", bob.ID); err != nil {
		t.Fatalf("DeleteCheck returned error: %v", err)
	}
	// Check must still exist.
	got, err := s.GetCheck("alice-check")
	if err != nil {
		t.Fatalf("GetCheck: %v", err)
	}
	if got == nil {
		t.Error("alice's check was deleted by bob")
	}
}

func TestCheckCRUD_PersistsCanonicalDefinition(t *testing.T) {
	s := newTestStore(t)

	user, err := s.CreateUser("normalize@example.com", "password", false)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	c := testCheckWithWebhook("c1", "http", "https://example.com", "https://hooks.example.com", 30)
	if err := s.CreateCheck(c, user.ID); err != nil {
		t.Fatalf("CreateCheck: %v", err)
	}

	got, err := s.GetCheck("c1")
	if err != nil {
		t.Fatalf("GetCheck: %v", err)
	}
	if got == nil {
		t.Fatal("GetCheck: expected check, got nil")
	}
	if *got != c {
		t.Fatalf("GetCheck = %+v, want %+v", *got, c)
	}
}

func TestDeleteCheck_PreservesHistoryWithoutLeakingStateOnIDReuse(t *testing.T) {
	s := newTestStore(t)

	alice, err := s.CreateUser("alice-delete-reuse@example.com", "pass", false)
	if err != nil {
		t.Fatalf("CreateUser alice: %v", err)
	}
	bob, err := s.CreateUser("bob-delete-reuse@example.com", "pass", false)
	if err != nil {
		t.Fatalf("CreateUser bob: %v", err)
	}

	check := testCheckWithWebhook("reused-check", "http", "https://alice.example.com", "https://hooks.example.com/wacht", 30)
	if err := s.CreateCheck(check, alice.ID); err != nil {
		t.Fatalf("CreateCheck alice: %v", err)
	}

	saveResult(t, s, "reused-check", "probe-a", false)
	if _, err := s.OpenIncidentWithNotification("reused-check", &NotificationRequest{
		WebhookURL: check.Webhook,
		Payload:    []byte(`{"status":"down"}`),
	}); err != nil {
		t.Fatalf("OpenIncidentWithNotification: %v", err)
	}

	if err := s.DeleteCheck("reused-check", alice.ID); err != nil {
		t.Fatalf("DeleteCheck alice: %v", err)
	}

	var (
		totalChecks   int
		deletedChecks int
	)
	if err := s.db.QueryRow(`SELECT COUNT(1) FROM checks WHERE id = $1`, "reused-check").Scan(&totalChecks); err != nil {
		t.Fatalf("count checks: %v", err)
	}
	if totalChecks != 1 {
		t.Fatalf("expected 1 historical check row after delete, got %d", totalChecks)
	}
	if err := s.db.QueryRow(`SELECT COUNT(1) FROM checks WHERE id = $1 AND deleted_at IS NOT NULL`, "reused-check").Scan(&deletedChecks); err != nil {
		t.Fatalf("count deleted checks: %v", err)
	}
	if deletedChecks != 1 {
		t.Fatalf("expected deleted generation to be retained, got %d", deletedChecks)
	}

	if err := s.CreateCheck(testCheck("reused-check", "http", "https://bob.example.com"), bob.ID); err != nil {
		t.Fatalf("CreateCheck bob: %v", err)
	}

	if err := s.db.QueryRow(`SELECT COUNT(1) FROM checks WHERE id = $1`, "reused-check").Scan(&totalChecks); err != nil {
		t.Fatalf("count checks after recreate: %v", err)
	}
	if totalChecks != 2 {
		t.Fatalf("expected 2 check generations after recreate, got %d", totalChecks)
	}

	results, err := s.RecentResultsPerProbe("reused-check")
	if err != nil {
		t.Fatalf("RecentResultsPerProbe after recreate: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected no historical results after recreate, got %d", len(results))
	}

	aliceIncidents, err := s.ListIncidents(alice.ID, 10)
	if err != nil {
		t.Fatalf("ListIncidents alice: %v", err)
	}
	if len(aliceIncidents) != 1 {
		t.Fatalf("expected 1 preserved incident for alice, got %d", len(aliceIncidents))
	}
	if aliceIncidents[0].CheckID != "reused-check" {
		t.Fatalf("expected preserved incident to keep reused-check id, got %s", aliceIncidents[0].CheckID)
	}
	if aliceIncidents[0].ResolvedAt == nil {
		t.Fatal("expected delete to close the historical open incident")
	}
	if aliceIncidents[0].DownNotification == nil || aliceIncidents[0].DownNotification.State != notificationStateSuperseded {
		t.Fatalf("expected preserved down notification to be superseded on delete, got %#v", aliceIncidents[0].DownNotification)
	}

	bobIncidents, err := s.ListIncidents(bob.ID, 10)
	if err != nil {
		t.Fatalf("ListIncidents bob: %v", err)
	}
	if len(bobIncidents) != 0 {
		t.Fatalf("expected recreated check to have no inherited incidents, got %d", len(bobIncidents))
	}

	saveResult(t, s, "reused-check", "probe-b", true)

	statuses, err := s.CheckStatuses(bob.ID)
	if err != nil {
		t.Fatalf("CheckStatuses bob: %v", err)
	}
	if len(statuses) != 1 {
		t.Fatalf("expected 1 recreated status, got %d", len(statuses))
	}
	if statuses[0].CheckID != "reused-check" {
		t.Fatalf("expected reused-check status, got %s", statuses[0].CheckID)
	}
	if !statuses[0].Up {
		t.Fatal("expected recreated check to stay healthy without stale incident state")
	}
	if statuses[0].IncidentSince != nil {
		t.Fatal("expected no stale incident timestamp after recreate")
	}

	aliceChecks, err := s.ListChecks(alice.ID)
	if err != nil {
		t.Fatalf("ListChecks alice: %v", err)
	}
	if len(aliceChecks) != 0 {
		t.Fatalf("expected deleted check to disappear from active list, got %d rows", len(aliceChecks))
	}
}
