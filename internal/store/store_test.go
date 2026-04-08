package store

import (
	"context"
	"database/sql"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/tmater/wacht/internal/checks"
	"github.com/tmater/wacht/internal/proto"
)

var testDSN string

func testCheck(id, checkType, target string) checks.Check {
	return checks.NewCheck(id, checkType, target, "", 0)
}

func testCheckWithWebhook(id, checkType, target, webhook string, intervalSeconds int) checks.Check {
	return checks.NewCheck(id, checkType, target, webhook, intervalSeconds)
}

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
		TRUNCATE incident_notifications, signup_requests, monitoring_snapshots, monitoring_journal, check_results, incidents, sessions, checks, users, probes RESTART IDENTITY CASCADE
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
		Type:      string(checks.CheckHTTP),
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

	user, err := s.CreateUser("open-dedup@example.com", "pass", false)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if err := s.CreateCheck(testCheck("check-1", "http", "https://example.com"), user.ID); err != nil {
		t.Fatalf("CreateCheck: %v", err)
	}

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
	if err := s.CreateCheck(testCheck("check-1", "http", "https://example.com"), user.ID); err != nil {
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
	if err := s.db.QueryRow(`
		SELECT COUNT(1)
		FROM incidents i
		JOIN checks c ON c.uid = i.check_uid
		WHERE c.id = $1
		  AND c.deleted_at IS NULL
		  AND i.resolved_at IS NULL
	`, "check-1").Scan(&openCount); err != nil {
		t.Fatalf("count open incidents: %v", err)
	}
	if openCount != 1 {
		t.Fatalf("expected exactly 1 open incident row, got %d", openCount)
	}
}

func TestResolveIncident_AllowsReopening(t *testing.T) {
	s := newTestStore(t)

	user, err := s.CreateUser("resolve-reopen@example.com", "pass", false)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if err := s.CreateCheck(testCheck("check-1", "http", "https://example.com"), user.ID); err != nil {
		t.Fatalf("CreateCheck: %v", err)
	}

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

func TestOpenIncidentWithNotification_CreatesDurableDownNotification(t *testing.T) {
	s := newTestStore(t)

	user, err := s.CreateUser("notify-open@example.com", "pass", false)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if err := s.CreateCheck(testCheckWithWebhook("check-1", "http", "https://example.com", "https://hooks.example.com/wacht", 30), user.ID); err != nil {
		t.Fatalf("CreateCheck: %v", err)
	}

	alreadyOpen, err := s.OpenIncidentWithNotification("check-1", &NotificationRequest{
		WebhookURL: "https://hooks.example.com/wacht",
		Payload:    []byte(`{"status":"down"}`),
	})
	if err != nil {
		t.Fatalf("OpenIncidentWithNotification: %v", err)
	}
	if alreadyOpen {
		t.Fatal("expected first open to create incident")
	}

	var (
		event, state, webhookURL string
		attempts                 int
		payload                  string
	)
	if err := s.db.QueryRow(`
		SELECT event, state, webhook_url, attempts, payload::text
		FROM incident_notifications
		LIMIT 1
	`).Scan(&event, &state, &webhookURL, &attempts, &payload); err != nil {
		t.Fatalf("QueryRow incident_notifications: %v", err)
	}

	if event != notificationEventDown {
		t.Fatalf("event = %q, want %q", event, notificationEventDown)
	}
	if state != notificationStatePending {
		t.Fatalf("state = %q, want %q", state, notificationStatePending)
	}
	if webhookURL != "https://hooks.example.com/wacht" {
		t.Fatalf("webhook_url = %q, want durable webhook URL", webhookURL)
	}
	if attempts != 0 {
		t.Fatalf("attempts = %d, want 0", attempts)
	}
	if payload != `{"status": "down"}` {
		t.Fatalf("payload = %s, want down payload snapshot", payload)
	}
}

func TestResolveIncidentWithNotification_SupersedesPendingDownNotification(t *testing.T) {
	s := newTestStore(t)

	user, err := s.CreateUser("notify-resolve@example.com", "pass", false)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if err := s.CreateCheck(testCheckWithWebhook("check-1", "http", "https://example.com", "https://hooks.example.com/wacht", 30), user.ID); err != nil {
		t.Fatalf("CreateCheck: %v", err)
	}

	if _, err := s.OpenIncidentWithNotification("check-1", &NotificationRequest{
		WebhookURL: "https://hooks.example.com/wacht",
		Payload:    []byte(`{"status":"down"}`),
	}); err != nil {
		t.Fatalf("OpenIncidentWithNotification: %v", err)
	}
	resolved, err := s.ResolveIncidentWithNotification("check-1", &NotificationRequest{
		WebhookURL: "https://hooks.example.com/wacht",
		Payload:    []byte(`{"status":"up"}`),
	})
	if err != nil {
		t.Fatalf("ResolveIncidentWithNotification: %v", err)
	}
	if !resolved {
		t.Fatal("expected incident to resolve")
	}

	rows, err := s.db.Query(`
		SELECT event, state
		FROM incident_notifications
		ORDER BY event ASC
	`)
	if err != nil {
		t.Fatalf("Query incident_notifications: %v", err)
	}
	defer rows.Close()

	states := map[string]string{}
	for rows.Next() {
		var event, state string
		if err := rows.Scan(&event, &state); err != nil {
			t.Fatalf("Scan: %v", err)
		}
		states[event] = state
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows.Err: %v", err)
	}

	if states[notificationEventDown] != notificationStateSuperseded {
		t.Fatalf("down state = %q, want %q", states[notificationEventDown], notificationStateSuperseded)
	}
	if states[notificationEventUp] != notificationStatePending {
		t.Fatalf("up state = %q, want %q", states[notificationEventUp], notificationStatePending)
	}
}

func TestMarkIncidentNotificationRetry_SupersedesDownAfterResolve(t *testing.T) {
	s := newTestStore(t)

	user, err := s.CreateUser("notify-retry@example.com", "pass", false)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if err := s.CreateCheck(testCheckWithWebhook("check-1", "http", "https://example.com", "https://hooks.example.com/wacht", 30), user.ID); err != nil {
		t.Fatalf("CreateCheck: %v", err)
	}

	if _, err := s.OpenIncidentWithNotification("check-1", &NotificationRequest{
		WebhookURL: "https://hooks.example.com/wacht",
		Payload:    []byte(`{"status":"down"}`),
	}); err != nil {
		t.Fatalf("OpenIncidentWithNotification: %v", err)
	}

	var notificationID int64
	if err := s.db.QueryRow(`SELECT id FROM incident_notifications WHERE event = $1`, notificationEventDown).Scan(&notificationID); err != nil {
		t.Fatalf("query notification id: %v", err)
	}

	if _, err := s.ResolveIncident("check-1"); err != nil {
		t.Fatalf("ResolveIncident: %v", err)
	}
	if err := s.MarkIncidentNotificationRetry(notificationID, time.Now().UTC(), time.Now().UTC().Add(time.Minute), "boom"); err != nil {
		t.Fatalf("MarkIncidentNotificationRetry: %v", err)
	}

	var state string
	if err := s.db.QueryRow(`SELECT state FROM incident_notifications WHERE id = $1`, notificationID).Scan(&state); err != nil {
		t.Fatalf("query state: %v", err)
	}
	if state != notificationStateSuperseded {
		t.Fatalf("state = %q, want %q", state, notificationStateSuperseded)
	}
}

func TestMarkIncidentNotificationDelivered_DoesNotOverrideSupersededDownNotification(t *testing.T) {
	s := newTestStore(t)

	user, err := s.CreateUser("notify-delivered@example.com", "pass", false)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if err := s.CreateCheck(testCheckWithWebhook("check-1", "http", "https://example.com", "https://hooks.example.com/wacht", 30), user.ID); err != nil {
		t.Fatalf("CreateCheck: %v", err)
	}

	if _, err := s.OpenIncidentWithNotification("check-1", &NotificationRequest{
		WebhookURL: "https://hooks.example.com/wacht",
		Payload:    []byte(`{"status":"down"}`),
	}); err != nil {
		t.Fatalf("OpenIncidentWithNotification: %v", err)
	}

	now := time.Now().UTC().Add(time.Second)
	jobs, err := s.ClaimDueIncidentNotifications(now, now.Add(-time.Minute), 1)
	if err != nil {
		t.Fatalf("ClaimDueIncidentNotifications: %v", err)
	}
	if len(jobs) != 1 {
		t.Fatalf("expected 1 claimed job, got %d", len(jobs))
	}

	if _, err := s.ResolveIncidentWithNotification("check-1", &NotificationRequest{
		WebhookURL: "https://hooks.example.com/wacht",
		Payload:    []byte(`{"status":"up"}`),
	}); err != nil {
		t.Fatalf("ResolveIncidentWithNotification: %v", err)
	}

	if err := s.MarkIncidentNotificationDelivered(jobs[0].ID, time.Now().UTC()); err != nil {
		t.Fatalf("MarkIncidentNotificationDelivered: %v", err)
	}

	var (
		state       string
		deliveredAt sql.NullTime
	)
	if err := s.db.QueryRow(`
		SELECT state, delivered_at
		FROM incident_notifications
		WHERE id = $1
	`, jobs[0].ID).Scan(&state, &deliveredAt); err != nil {
		t.Fatalf("query notification: %v", err)
	}
	if state != notificationStateSuperseded {
		t.Fatalf("state = %q, want %q", state, notificationStateSuperseded)
	}
	if deliveredAt.Valid {
		t.Fatalf("delivered_at = %s, want NULL for superseded notification", deliveredAt.Time)
	}
}

func TestCheckStatuses_UsesIncidentStateInsteadOfLatestSingleResult(t *testing.T) {
	s := newTestStore(t)

	user, err := s.CreateUser("statuses@example.com", "pass", false)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if err := s.CreateCheck(testCheck("check-1", "http", "https://example.com"), user.ID); err != nil {
		t.Fatalf("CreateCheck: %v", err)
	}

	saveResult(t, s, "check-1", "probe-a", true)
	saveResult(t, s, "check-1", "probe-b", false)

	statuses, err := s.CheckStatuses(user.ID)
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
	if err := s.CreateCheck(testCheck("check-1", "http", "https://example.com"), user.ID); err != nil {
		t.Fatalf("CreateCheck: %v", err)
	}

	saveResult(t, s, "check-1", "probe-a", true)
	if _, err := s.OpenIncident("check-1"); err != nil {
		t.Fatalf("OpenIncident: %v", err)
	}
	saveResult(t, s, "check-1", "probe-b", true)

	statuses, err := s.CheckStatuses(user.ID)
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

	user, err := s.CreateUser("recent-per-probe@example.com", "pass", false)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if err := s.CreateCheck(testCheck("check-1", "http", "https://example.com"), user.ID); err != nil {
		t.Fatalf("CreateCheck: %v", err)
	}

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

	user, err := s.CreateUser("recent-by-probe@example.com", "pass", false)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if err := s.CreateCheck(testCheck("check-1", "http", "https://example.com"), user.ID); err != nil {
		t.Fatalf("CreateCheck: %v", err)
	}

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

func TestCheckStatuses_ScopedToUser(t *testing.T) {
	s := newTestStore(t)

	alice, err := s.CreateUser("alice-statuses@example.com", "pass", false)
	if err != nil {
		t.Fatalf("CreateUser alice: %v", err)
	}
	bob, err := s.CreateUser("bob-statuses@example.com", "pass", false)
	if err != nil {
		t.Fatalf("CreateUser bob: %v", err)
	}

	if err := s.CreateCheck(testCheck("alice-check", "http", "https://alice.example.com"), alice.ID); err != nil {
		t.Fatalf("CreateCheck alice: %v", err)
	}
	if err := s.CreateCheck(testCheck("bob-check", "http", "https://bob.example.com"), bob.ID); err != nil {
		t.Fatalf("CreateCheck bob: %v", err)
	}

	saveResult(t, s, "alice-check", "probe-a", true)
	saveResult(t, s, "bob-check", "probe-b", true)
	if _, err := s.OpenIncident("bob-check"); err != nil {
		t.Fatalf("OpenIncident bob: %v", err)
	}

	aliceStatuses, err := s.CheckStatuses(alice.ID)
	if err != nil {
		t.Fatalf("CheckStatuses alice: %v", err)
	}
	if len(aliceStatuses) != 1 {
		t.Fatalf("expected 1 alice status, got %d", len(aliceStatuses))
	}
	if aliceStatuses[0].CheckID != "alice-check" {
		t.Fatalf("expected alice-check, got %s", aliceStatuses[0].CheckID)
	}

	bobStatuses, err := s.CheckStatuses(bob.ID)
	if err != nil {
		t.Fatalf("CheckStatuses bob: %v", err)
	}
	if len(bobStatuses) != 1 {
		t.Fatalf("expected 1 bob status, got %d", len(bobStatuses))
	}
	if bobStatuses[0].CheckID != "bob-check" {
		t.Fatalf("expected bob-check, got %s", bobStatuses[0].CheckID)
	}
	if bobStatuses[0].IncidentSince == nil {
		t.Fatal("expected bob status to include the open incident timestamp")
	}
}

func TestPublicCheckStatuses_UsesPendingUpAndDownStates(t *testing.T) {
	s := newTestStore(t)

	user, err := s.CreateUser("public-statuses@example.com", "pass", false)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if err := s.CreateCheck(testCheck("down-check", "http", "https://down.example.com"), user.ID); err != nil {
		t.Fatalf("CreateCheck down: %v", err)
	}
	if err := s.CreateCheck(testCheck("pending-check", "http", "https://pending.example.com"), user.ID); err != nil {
		t.Fatalf("CreateCheck pending: %v", err)
	}
	if err := s.CreateCheck(testCheck("up-check", "http", "https://up.example.com"), user.ID); err != nil {
		t.Fatalf("CreateCheck up: %v", err)
	}

	saveResult(t, s, "down-check", "probe-a", false)
	if _, err := s.OpenIncident("down-check"); err != nil {
		t.Fatalf("OpenIncident: %v", err)
	}
	saveResult(t, s, "up-check", "probe-b", true)

	statuses, found, err := s.PublicCheckStatuses(user.PublicStatusSlug)
	if err != nil {
		t.Fatalf("PublicCheckStatuses: %v", err)
	}
	if !found {
		t.Fatal("expected slug to resolve to a user")
	}
	if len(statuses) != 3 {
		t.Fatalf("expected 3 statuses, got %d", len(statuses))
	}

	byID := make(map[string]PublicCheckStatus, len(statuses))
	for _, status := range statuses {
		byID[status.CheckID] = status
	}

	if got := byID["down-check"].Status; got != "down" {
		t.Fatalf("down-check status = %q, want down", got)
	}
	if byID["down-check"].IncidentSince == nil {
		t.Fatal("expected down-check incident timestamp")
	}
	if got := byID["pending-check"].Status; got != "pending" {
		t.Fatalf("pending-check status = %q, want pending", got)
	}
	if byID["pending-check"].IncidentSince != nil {
		t.Fatal("expected pending-check to omit incident timestamp")
	}
	if got := byID["up-check"].Status; got != "up" {
		t.Fatalf("up-check status = %q, want up", got)
	}
}

func TestPublicCheckStatuses_DistinguishesUnknownSlugAndNoChecks(t *testing.T) {
	s := newTestStore(t)

	user, err := s.CreateUser("public-empty@example.com", "pass", false)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	statuses, found, err := s.PublicCheckStatuses(user.PublicStatusSlug)
	if err != nil {
		t.Fatalf("PublicCheckStatuses existing slug: %v", err)
	}
	if !found {
		t.Fatal("expected existing slug to resolve")
	}
	if len(statuses) != 0 {
		t.Fatalf("expected no statuses for a user with no checks, got %d", len(statuses))
	}

	statuses, found, err = s.PublicCheckStatuses("missing-slug")
	if err != nil {
		t.Fatalf("PublicCheckStatuses missing slug: %v", err)
	}
	if found {
		t.Fatal("expected missing slug to report found=false")
	}
	if len(statuses) != 0 {
		t.Fatalf("expected no statuses for missing slug, got %d", len(statuses))
	}
}

func TestEvictOldResults_DeletesOldKeepsNew(t *testing.T) {
	s := newTestStore(t)

	user, err := s.CreateUser("evict-old@example.com", "pass", false)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if err := s.CreateCheck(testCheck("check-1", "http", "https://example.com"), user.ID); err != nil {
		t.Fatalf("CreateCheck: %v", err)
	}

	// Insert one old result and one recent result.
	old := proto.CheckResult{
		CheckID:   "check-1",
		ProbeID:   "probe-a",
		Type:      string(checks.CheckHTTP),
		Target:    "https://example.com",
		Up:        true,
		Timestamp: time.Now().Add(-40 * 24 * time.Hour), // 40 days ago
	}
	recent := proto.CheckResult{
		CheckID:   "check-1",
		ProbeID:   "probe-a",
		Type:      string(checks.CheckHTTP),
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
	if err := s.CreateCheck(testCheck("check-1", "http", "https://example.com"), user.ID); err != nil {
		t.Fatalf("CreateCheck check-1: %v", err)
	}
	if err := s.CreateCheck(testCheck("check-2", "http", "https://example.com"), user.ID); err != nil {
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

func TestListIncidents_IncludesNotificationSummary(t *testing.T) {
	s := newTestStore(t)

	user, err := s.CreateUser("incident-summary@example.com", "pass", false)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if err := s.CreateCheck(testCheckWithWebhook("check-1", "http", "https://example.com", "https://hooks.example.com/wacht", 30), user.ID); err != nil {
		t.Fatalf("CreateCheck: %v", err)
	}

	if _, err := s.OpenIncidentWithNotification("check-1", &NotificationRequest{
		WebhookURL: "https://hooks.example.com/wacht",
		Payload:    []byte(`{"status":"down"}`),
	}); err != nil {
		t.Fatalf("OpenIncidentWithNotification: %v", err)
	}
	if _, err := s.ResolveIncidentWithNotification("check-1", &NotificationRequest{
		WebhookURL: "https://hooks.example.com/wacht",
		Payload:    []byte(`{"status":"up"}`),
	}); err != nil {
		t.Fatalf("ResolveIncidentWithNotification: %v", err)
	}

	incidents, err := s.ListIncidents(user.ID, 10)
	if err != nil {
		t.Fatalf("ListIncidents: %v", err)
	}
	if len(incidents) != 1 {
		t.Fatalf("expected 1 incident, got %d", len(incidents))
	}

	incident := incidents[0]
	if incident.DownNotification == nil {
		t.Fatal("expected down notification summary")
	}
	if incident.UpNotification == nil {
		t.Fatal("expected up notification summary")
	}
	if incident.DownNotification.State != notificationStateSuperseded {
		t.Fatalf("down state = %q, want %q", incident.DownNotification.State, notificationStateSuperseded)
	}
	if incident.UpNotification.State != notificationStatePending {
		t.Fatalf("up state = %q, want %q", incident.UpNotification.State, notificationStatePending)
	}
}

func TestListIncidents_RespectsLimit(t *testing.T) {
	s := newTestStore(t)

	user, err := s.CreateUser("limits@example.com", "pass", false)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if err := s.CreateCheck(testCheck("check-1", "http", "https://example.com"), user.ID); err != nil {
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

	if err := s.CreateCheck(testCheck("alice-check", "http", "https://alice.example.com"), alice.ID); err != nil {
		t.Fatalf("CreateCheck alice: %v", err)
	}
	if err := s.CreateCheck(testCheck("bob-check", "http", "https://bob.example.com"), bob.ID); err != nil {
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

	user, err := s.CreateUser("evict-none@example.com", "pass", false)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if err := s.CreateCheck(testCheck("check-1", "http", "https://example.com"), user.ID); err != nil {
		t.Fatalf("CreateCheck: %v", err)
	}

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
