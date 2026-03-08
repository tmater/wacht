package store

import (
	"context"
	"os"
	"sync"
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
		TRUNCATE check_results, incidents, sessions, checks, users, probes RESTART IDENTITY CASCADE
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

func TestOpenIncident_ConcurrentDeduplication(t *testing.T) {
	s := newTestStore(t)

	user, err := s.CreateUser("concurrency@example.com", "pass", false)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if err := s.CreateCheck(Check{ID: "check-1", Type: "http", Target: "https://example.com"}, user.ID); err != nil {
		t.Fatalf("CreateCheck: %v", err)
	}

	const workers = 12

	var wg sync.WaitGroup
	results := make(chan bool, workers)
	errs := make(chan error, workers)

	wg.Add(workers)
	for range workers {
		go func() {
			defer wg.Done()
			alreadyOpen, err := s.OpenIncident("check-1")
			if err != nil {
				errs <- err
				return
			}
			results <- alreadyOpen
		}()
	}

	wg.Wait()
	close(results)
	close(errs)

	for err := range errs {
		t.Fatalf("OpenIncident: %v", err)
	}

	opened := 0
	duplicates := 0
	for alreadyOpen := range results {
		if alreadyOpen {
			duplicates++
			continue
		}
		opened++
	}

	if opened != 1 {
		t.Fatalf("expected exactly one incident opener, got %d", opened)
	}
	if duplicates != workers-1 {
		t.Fatalf("expected %d duplicate opens, got %d", workers-1, duplicates)
	}

	var openCount int
	if err := s.db.QueryRow(`SELECT COUNT(1) FROM incidents WHERE check_id = $1 AND resolved_at IS NULL`, "check-1").Scan(&openCount); err != nil {
		t.Fatalf("count open incidents: %v", err)
	}
	if openCount != 1 {
		t.Fatalf("expected exactly 1 open incident row, got %d", openCount)
	}
}

func TestResolveIncident_AllowsReopening(t *testing.T) {
	s := newTestStore(t)

	if _, err := s.OpenIncident("check-1"); err != nil {
		t.Fatalf("OpenIncident: %v", err)
	}
	resolved, err := s.ResolveIncident("check-1")
	if err != nil {
		t.Fatalf("ResolveIncident: %v", err)
	}
	if !resolved {
		t.Fatal("expected ResolveIncident to report a resolved incident")
	}

	alreadyOpen, err := s.OpenIncident("check-1")
	if err != nil {
		t.Fatalf("second OpenIncident: %v", err)
	}
	if alreadyOpen {
		t.Fatal("expected alreadyOpen=false after resolve, got true")
	}
}

func TestResolveIncident_NoOpenIncident(t *testing.T) {
	s := newTestStore(t)

	resolved, err := s.ResolveIncident("check-1")
	if err != nil {
		t.Fatalf("ResolveIncident: %v", err)
	}
	if resolved {
		t.Fatal("expected ResolveIncident to report no-op when nothing was open")
	}
}

func TestCheckStatuses_UsesIncidentStateInsteadOfLatestSingleResult(t *testing.T) {
	s := newTestStore(t)

	user, err := s.CreateUser("statuses@example.com", "pass", false)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if err := s.CreateCheck(Check{ID: "check-1", Type: "http", Target: "https://example.com"}, user.ID); err != nil {
		t.Fatalf("CreateCheck: %v", err)
	}

	saveResult(t, s, "check-1", "probe-a", true)
	saveResult(t, s, "check-1", "probe-b", false)

	statuses, err := s.CheckStatuses()
	if err != nil {
		t.Fatalf("CheckStatuses: %v", err)
	}
	if len(statuses) != 1 {
		t.Fatalf("expected 1 status, got %d", len(statuses))
	}
	if !statuses[0].Up {
		t.Fatal("expected check to remain up without an open incident")
	}
	if statuses[0].IncidentSince != nil {
		t.Fatal("expected no incident timestamp for a healthy check")
	}
}

func TestCheckStatuses_UsesOpenIncidentAsDownTruth(t *testing.T) {
	s := newTestStore(t)

	user, err := s.CreateUser("incident-status@example.com", "pass", false)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if err := s.CreateCheck(Check{ID: "check-1", Type: "http", Target: "https://example.com"}, user.ID); err != nil {
		t.Fatalf("CreateCheck: %v", err)
	}

	saveResult(t, s, "check-1", "probe-a", true)
	if _, err := s.OpenIncident("check-1"); err != nil {
		t.Fatalf("OpenIncident: %v", err)
	}
	saveResult(t, s, "check-1", "probe-b", true)

	statuses, err := s.CheckStatuses()
	if err != nil {
		t.Fatalf("CheckStatuses: %v", err)
	}
	if len(statuses) != 1 {
		t.Fatalf("expected 1 status, got %d", len(statuses))
	}
	if statuses[0].Up {
		t.Fatal("expected open incident to keep status down")
	}
	if statuses[0].IncidentSince == nil {
		t.Fatal("expected incident timestamp for an open incident")
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

func TestEvictOldResults_DeletesOldKeepsNew(t *testing.T) {
	s := newTestStore(t)

	// Insert one old result and one recent result.
	old := proto.CheckResult{
		CheckID:   "check-1",
		ProbeID:   "probe-a",
		Type:      proto.CheckHTTP,
		Target:    "https://example.com",
		Up:        true,
		Timestamp: time.Now().Add(-40 * 24 * time.Hour), // 40 days ago
	}
	recent := proto.CheckResult{
		CheckID:   "check-1",
		ProbeID:   "probe-a",
		Type:      proto.CheckHTTP,
		Target:    "https://example.com",
		Up:        true,
		Timestamp: time.Now().Add(-1 * time.Hour), // 1 hour ago
	}
	if err := s.SaveResult(old); err != nil {
		t.Fatalf("SaveResult old: %v", err)
	}
	if err := s.SaveResult(recent); err != nil {
		t.Fatalf("SaveResult recent: %v", err)
	}

	cutoff := time.Now().Add(-30 * 24 * time.Hour) // 30-day cutoff
	n, err := s.EvictOldResults(cutoff)
	if err != nil {
		t.Fatalf("EvictOldResults: %v", err)
	}
	if n != 1 {
		t.Errorf("expected 1 row deleted, got %d", n)
	}

	// Only the recent result should remain.
	results, err := s.RecentResultsByProbe("check-1", "probe-a", 10)
	if err != nil {
		t.Fatalf("RecentResultsByProbe: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("expected 1 row remaining, got %d", len(results))
	}
}

func TestListIncidents_OrderAndResolved(t *testing.T) {
	s := newTestStore(t)

	user, err := s.CreateUser("incidents@example.com", "pass", false)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if err := s.CreateCheck(Check{ID: "check-1", Type: "http", Target: "https://example.com"}, user.ID); err != nil {
		t.Fatalf("CreateCheck check-1: %v", err)
	}
	if err := s.CreateCheck(Check{ID: "check-2", Type: "http", Target: "https://example.com"}, user.ID); err != nil {
		t.Fatalf("CreateCheck check-2: %v", err)
	}

	// Open and resolve two incidents, then open a third (still open).
	if _, err := s.OpenIncident("check-1"); err != nil {
		t.Fatalf("OpenIncident check-1: %v", err)
	}
	if _, err := s.ResolveIncident("check-1"); err != nil {
		t.Fatalf("ResolveIncident check-1: %v", err)
	}

	if _, err := s.OpenIncident("check-2"); err != nil {
		t.Fatalf("OpenIncident check-2: %v", err)
	}
	if _, err := s.ResolveIncident("check-2"); err != nil {
		t.Fatalf("ResolveIncident check-2: %v", err)
	}

	if _, err := s.OpenIncident("check-1"); err != nil {
		t.Fatalf("OpenIncident check-1 (second): %v", err)
	}

	incidents, err := s.ListIncidents(user.ID, 10)
	if err != nil {
		t.Fatalf("ListIncidents: %v", err)
	}
	if len(incidents) != 3 {
		t.Fatalf("expected 3 incidents, got %d", len(incidents))
	}

	// Newest first — the still-open check-1 incident was inserted last.
	if incidents[0].CheckID != "check-1" {
		t.Errorf("incidents[0]: expected check-1, got %s", incidents[0].CheckID)
	}
	if incidents[0].ResolvedAt != nil {
		t.Errorf("incidents[0]: expected open (ResolvedAt nil), got resolved")
	}

	// The two resolved incidents should have ResolvedAt set.
	for _, inc := range incidents[1:] {
		if inc.ResolvedAt == nil {
			t.Errorf("incident id=%d check_id=%s: expected resolved, got open", inc.ID, inc.CheckID)
		}
	}
}

func TestListIncidents_RespectsLimit(t *testing.T) {
	s := newTestStore(t)

	user, err := s.CreateUser("limits@example.com", "pass", false)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if err := s.CreateCheck(Check{ID: "check-1", Type: "http", Target: "https://example.com"}, user.ID); err != nil {
		t.Fatalf("CreateCheck: %v", err)
	}

	for i := 0; i < 5; i++ {
		if _, err := s.OpenIncident("check-1"); err != nil {
			t.Fatalf("OpenIncident: %v", err)
		}
		if _, err := s.ResolveIncident("check-1"); err != nil {
			t.Fatalf("ResolveIncident: %v", err)
		}
	}

	incidents, err := s.ListIncidents(user.ID, 3)
	if err != nil {
		t.Fatalf("ListIncidents: %v", err)
	}
	if len(incidents) != 3 {
		t.Errorf("expected 3 incidents (limit), got %d", len(incidents))
	}
}

func TestListIncidents_ScopedToUser(t *testing.T) {
	s := newTestStore(t)

	alice, err := s.CreateUser("alice-incidents@example.com", "pass", false)
	if err != nil {
		t.Fatalf("CreateUser alice: %v", err)
	}
	bob, err := s.CreateUser("bob-incidents@example.com", "pass", false)
	if err != nil {
		t.Fatalf("CreateUser bob: %v", err)
	}

	if err := s.CreateCheck(Check{ID: "alice-check", Type: "http", Target: "https://alice.example.com"}, alice.ID); err != nil {
		t.Fatalf("CreateCheck alice: %v", err)
	}
	if err := s.CreateCheck(Check{ID: "bob-check", Type: "http", Target: "https://bob.example.com"}, bob.ID); err != nil {
		t.Fatalf("CreateCheck bob: %v", err)
	}

	if _, err := s.OpenIncident("alice-check"); err != nil {
		t.Fatalf("OpenIncident alice: %v", err)
	}
	if _, err := s.OpenIncident("bob-check"); err != nil {
		t.Fatalf("OpenIncident bob: %v", err)
	}

	aliceIncidents, err := s.ListIncidents(alice.ID, 10)
	if err != nil {
		t.Fatalf("ListIncidents alice: %v", err)
	}
	if len(aliceIncidents) != 1 {
		t.Fatalf("expected 1 alice incident, got %d", len(aliceIncidents))
	}
	if aliceIncidents[0].CheckID != "alice-check" {
		t.Fatalf("expected alice-check, got %s", aliceIncidents[0].CheckID)
	}

	bobIncidents, err := s.ListIncidents(bob.ID, 10)
	if err != nil {
		t.Fatalf("ListIncidents bob: %v", err)
	}
	if len(bobIncidents) != 1 {
		t.Fatalf("expected 1 bob incident, got %d", len(bobIncidents))
	}
	if bobIncidents[0].CheckID != "bob-check" {
		t.Fatalf("expected bob-check, got %s", bobIncidents[0].CheckID)
	}
}

func TestEvictOldResults_NothingToDelete(t *testing.T) {
	s := newTestStore(t)

	saveResult(t, s, "check-1", "probe-a", true) // recent result

	cutoff := time.Now().Add(-30 * 24 * time.Hour)
	n, err := s.EvictOldResults(cutoff)
	if err != nil {
		t.Fatalf("EvictOldResults: %v", err)
	}
	if n != 0 {
		t.Errorf("expected 0 rows deleted, got %d", n)
	}
}
