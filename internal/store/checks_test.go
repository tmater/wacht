package store

import (
	"testing"
)

func TestSeedChecks_SkipsExisting(t *testing.T) {
	s := newTestStore(t)

	checks := []Check{{ID: "c1", Type: "http", Target: "https://a.com", Webhook: ""}}
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
	c := Check{ID: "c1", Type: "http", Target: "https://example.com", Webhook: ""}
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

	aliceCheck := Check{ID: "alice-check", Type: "http", Target: "https://alice.com"}
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
