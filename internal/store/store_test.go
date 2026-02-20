package store

import (
	"testing"
	"time"

	"github.com/tmater/wacht/internal/proto"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := New("file::memory:?cache=shared&mode=memory")
	if err != nil {
		t.Fatalf("failed to open in-memory store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
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
	if err := s.SeedChecks(checks); err != nil {
		t.Fatalf("SeedChecks: %v", err)
	}

	// Seed again with a different target — existing row must be unchanged.
	checks[0].Target = "https://b.com"
	if err := s.SeedChecks(checks); err != nil {
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

	// Create
	c := Check{ID: "c1", Type: "http", Target: "https://example.com", Webhook: ""}
	if err := s.CreateCheck(c); err != nil {
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
	all, err := s.ListChecks()
	if err != nil {
		t.Fatalf("ListChecks: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("ListChecks: expected 1, got %d", len(all))
	}

	// Update
	c.Target = "https://updated.com"
	c.Webhook = "https://hooks.example.com"
	if err := s.UpdateCheck(c); err != nil {
		t.Fatalf("UpdateCheck: %v", err)
	}
	got, _ = s.GetCheck("c1")
	if got.Target != "https://updated.com" || got.Webhook != "https://hooks.example.com" {
		t.Errorf("UpdateCheck: unexpected values %+v", got)
	}

	// Delete
	if err := s.DeleteCheck("c1"); err != nil {
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
