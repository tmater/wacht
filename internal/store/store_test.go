package store

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/tmater/wacht/internal/proto"
)

var testDSN string

func TestMain(m *testing.M) {
	ctx := context.Background()

	ctr, err := postgres.Run(ctx,
		"postgres:16-alpine",
		postgres.WithDatabase("wacht_test"),
		postgres.WithUsername("wacht"),
		postgres.WithPassword("wacht"),
		postgres.BasicWaitStrategies(),
	)
	if err != nil {
		panic("start postgres container: " + err.Error())
	}

	testDSN, err = ctr.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		panic("get connection string: " + err.Error())
	}

	code := m.Run()

	_ = ctr.Terminate(ctx)
	os.Exit(code)
}

func newTestStore(t *testing.T) *Store {
	t.Helper()

	s, err := New(testDSN)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	// Wipe all tables so tests don't interfere with each other.
	_, err = s.db.Exec(`
		TRUNCATE check_results, incidents, sessions, checks, users RESTART IDENTITY CASCADE
	`)
	if err != nil {
		t.Fatalf("truncate tables: %v", err)
	}

	return s
}

func saveResult(t *testing.T, s *Store, checkID, probeID string, up bool) {
	t.Helper()
	err := s.SaveResult(proto.CheckResult{
		CheckID:   checkID,
		ProbeID:   probeID,
		Type:      proto.CheckHTTP,
		Target:    "https://example.com",
		Up:        up,
		Timestamp: time.Now(),
	})
	if err != nil {
		t.Fatalf("SaveResult: %v", err)
	}
}

func TestOpenIncident_Deduplication(t *testing.T) {
	s := newTestStore(t)

	alreadyOpen, err := s.OpenIncident("check-1")
	if err != nil {
		t.Fatalf("first OpenIncident: %v", err)
	}
	if alreadyOpen {
		t.Fatal("expected alreadyOpen=false on first call, got true")
	}

	alreadyOpen, err = s.OpenIncident("check-1")
	if err != nil {
		t.Fatalf("second OpenIncident: %v", err)
	}
	if !alreadyOpen {
		t.Fatal("expected alreadyOpen=true on second call, got false")
	}
}

func TestResolveIncident_AllowsReopening(t *testing.T) {
	s := newTestStore(t)

	if _, err := s.OpenIncident("check-1"); err != nil {
		t.Fatalf("OpenIncident: %v", err)
	}
	if err := s.ResolveIncident("check-1"); err != nil {
		t.Fatalf("ResolveIncident: %v", err)
	}

	alreadyOpen, err := s.OpenIncident("check-1")
	if err != nil {
		t.Fatalf("second OpenIncident: %v", err)
	}
	if alreadyOpen {
		t.Fatal("expected alreadyOpen=false after resolve, got true")
	}
}

func TestRecentResultsPerProbe_LatestPerProbe(t *testing.T) {
	s := newTestStore(t)

	// probe-a: two results — first up, then down
	saveResult(t, s, "check-1", "probe-a", true)
	saveResult(t, s, "check-1", "probe-a", false)

	// probe-b: one result — up
	saveResult(t, s, "check-1", "probe-b", true)

	results, err := s.RecentResultsPerProbe("check-1")
	if err != nil {
		t.Fatalf("RecentResultsPerProbe: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results (one per probe), got %d", len(results))
	}

	byProbe := make(map[string]bool)
	for _, r := range results {
		byProbe[r.ProbeID] = r.Up
	}

	if byProbe["probe-a"] != false {
		t.Errorf("probe-a: expected latest result to be down")
	}
	if byProbe["probe-b"] != true {
		t.Errorf("probe-b: expected latest result to be up")
	}
}

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
	user, err := s.CreateUser("test@example.com", "password")
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

func TestCreateUser_HashesPassword(t *testing.T) {
	s := newTestStore(t)

	user, err := s.CreateUser("alice@example.com", "secret")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if user.ID == 0 {
		t.Error("expected non-zero ID")
	}
	if user.Email != "alice@example.com" {
		t.Errorf("unexpected email %q", user.Email)
	}
	// Password must be stored hashed, not as plaintext.
	if user.PasswordHash == "secret" {
		t.Error("password stored as plaintext")
	}
	if user.PasswordHash == "" {
		t.Error("password hash is empty")
	}
}

func TestCreateUser_DuplicateEmail(t *testing.T) {
	s := newTestStore(t)

	if _, err := s.CreateUser("bob@example.com", "pass1"); err != nil {
		t.Fatalf("first CreateUser: %v", err)
	}
	_, err := s.CreateUser("bob@example.com", "pass2")
	if err == nil {
		t.Fatal("expected error on duplicate email, got nil")
	}
}

func TestAuthenticateUser_CorrectPassword(t *testing.T) {
	s := newTestStore(t)

	if _, err := s.CreateUser("carol@example.com", "correcthorsebattery"); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	user, err := s.AuthenticateUser("carol@example.com", "correcthorsebattery")
	if err != nil {
		t.Fatalf("AuthenticateUser: %v", err)
	}
	if user == nil {
		t.Fatal("expected user, got nil")
	}
	if user.Email != "carol@example.com" {
		t.Errorf("unexpected email %q", user.Email)
	}
}

func TestAuthenticateUser_WrongPassword(t *testing.T) {
	s := newTestStore(t)

	if _, err := s.CreateUser("dave@example.com", "rightpassword"); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	user, err := s.AuthenticateUser("dave@example.com", "wrongpassword")
	if err != nil {
		t.Fatalf("AuthenticateUser returned unexpected error: %v", err)
	}
	if user != nil {
		t.Fatal("expected nil for wrong password, got user")
	}
}

func TestAuthenticateUser_UnknownEmail(t *testing.T) {
	s := newTestStore(t)

	user, err := s.AuthenticateUser("nobody@example.com", "anything")
	if err != nil {
		t.Fatalf("AuthenticateUser returned unexpected error: %v", err)
	}
	if user != nil {
		t.Fatal("expected nil for unknown email, got user")
	}
}

func TestUserExists(t *testing.T) {
	s := newTestStore(t)

	exists, err := s.UserExists()
	if err != nil {
		t.Fatalf("UserExists (empty): %v", err)
	}
	if exists {
		t.Fatal("expected false on empty store")
	}

	if _, err := s.CreateUser("eve@example.com", "pass"); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	exists, err = s.UserExists()
	if err != nil {
		t.Fatalf("UserExists (after create): %v", err)
	}
	if !exists {
		t.Fatal("expected true after creating a user")
	}
}

func TestSession_CreateAndLookup(t *testing.T) {
	s := newTestStore(t)

	user, err := s.CreateUser("frank@example.com", "pass")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	token, err := s.CreateSession(user.ID)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if token == "" {
		t.Fatal("expected non-empty token")
	}

	got, err := s.GetSessionUser(token)
	if err != nil {
		t.Fatalf("GetSessionUser: %v", err)
	}
	if got == nil {
		t.Fatal("expected user, got nil")
	}
	if got.ID != user.ID {
		t.Errorf("expected user ID %d, got %d", user.ID, got.ID)
	}
}

func TestSession_InvalidToken(t *testing.T) {
	s := newTestStore(t)

	got, err := s.GetSessionUser("doesnotexist")
	if err != nil {
		t.Fatalf("GetSessionUser: %v", err)
	}
	if got != nil {
		t.Fatal("expected nil for unknown token, got user")
	}
}

func TestSession_Delete(t *testing.T) {
	s := newTestStore(t)

	user, err := s.CreateUser("grace@example.com", "pass")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	token, err := s.CreateSession(user.ID)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	if err := s.DeleteSession(token); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}

	got, err := s.GetSessionUser(token)
	if err != nil {
		t.Fatalf("GetSessionUser after delete: %v", err)
	}
	if got != nil {
		t.Fatal("expected nil after delete, got user")
	}
}

func TestCheckCrossUserIsolation(t *testing.T) {
	s := newTestStore(t)

	alice, err := s.CreateUser("alice@example.com", "pass")
	if err != nil {
		t.Fatalf("CreateUser alice: %v", err)
	}
	bob, err := s.CreateUser("bob@example.com", "pass")
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

func TestRecentResultsByProbe_OrderAndLimit(t *testing.T) {
	s := newTestStore(t)

	// Insert 3 results: up, up, down (oldest to newest)
	saveResult(t, s, "check-1", "probe-a", true)
	saveResult(t, s, "check-1", "probe-a", true)
	saveResult(t, s, "check-1", "probe-a", false)

	// Ask for last 2 — should be down, up (newest first)
	results, err := s.RecentResultsByProbe("check-1", "probe-a", 2)
	if err != nil {
		t.Fatalf("RecentResultsByProbe: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].Up != false {
		t.Errorf("results[0]: expected down (newest), got up")
	}
	if results[1].Up != true {
		t.Errorf("results[1]: expected up, got down")
	}
}
