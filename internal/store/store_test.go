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
		TRUNCATE check_probe_state, incident_notifications, signup_requests, incidents, sessions, checks, users, probes RESTART IDENTITY CASCADE
	`)
	if err != nil {
		t.Fatalf("truncate tables: %v", err)
	}

	return s
}

func openIncidentForTest(s *Store, checkID string) (bool, error) {
	return s.openIncidentWithNotification(checkID, nil)
}

func openIncidentWithNotificationForTest(s *Store, checkID string, request *NotificationRequest) (bool, error) {
	return s.openIncidentWithNotification(checkID, request)
}

func resolveIncidentForTest(s *Store, checkID string) (bool, error) {
	return s.resolveIncidentWithNotification(checkID, nil)
}

func resolveIncidentWithNotificationForTest(s *Store, checkID string, request *NotificationRequest) (bool, error) {
	return s.resolveIncidentWithNotification(checkID, request)
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

	alreadyOpen, err := openIncidentForTest(s, "check-1")
	if err != nil {
		t.Fatalf("first OpenIncident: %v", err)
	}
	if alreadyOpen {
		t.Fatal("expected alreadyOpen=false on first call, got true")
	}

	alreadyOpen, err = openIncidentForTest(s, "check-1")
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
			alreadyOpen, err := openIncidentForTest(s, "check-1")
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

	if _, err := openIncidentForTest(s, "check-1"); err != nil {
		t.Fatalf("OpenIncident: %v", err)
	}
	resolved, err := resolveIncidentForTest(s, "check-1")
	if err != nil {
		t.Fatalf("ResolveIncident: %v", err)
	}
	if !resolved {
		t.Fatal("expected ResolveIncident to report a resolved incident")
	}

	alreadyOpen, err := openIncidentForTest(s, "check-1")
	if err != nil {
		t.Fatalf("second OpenIncident: %v", err)
	}
	if alreadyOpen {
		t.Fatal("expected alreadyOpen=false after resolve, got true")
	}
}

func TestResolveIncident_NoOpenIncident(t *testing.T) {
	s := newTestStore(t)

	resolved, err := resolveIncidentForTest(s, "check-1")
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

	alreadyOpen, err := openIncidentWithNotificationForTest(s, "check-1", &NotificationRequest{
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

	if _, err := openIncidentWithNotificationForTest(s, "check-1", &NotificationRequest{
		WebhookURL: "https://hooks.example.com/wacht",
		Payload:    []byte(`{"status":"down"}`),
	}); err != nil {
		t.Fatalf("OpenIncidentWithNotification: %v", err)
	}
	resolved, err := resolveIncidentWithNotificationForTest(s, "check-1", &NotificationRequest{
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

	if _, err := openIncidentWithNotificationForTest(s, "check-1", &NotificationRequest{
		WebhookURL: "https://hooks.example.com/wacht",
		Payload:    []byte(`{"status":"down"}`),
	}); err != nil {
		t.Fatalf("OpenIncidentWithNotification: %v", err)
	}

	var notificationID int64
	if err := s.db.QueryRow(`SELECT id FROM incident_notifications WHERE event = $1`, notificationEventDown).Scan(&notificationID); err != nil {
		t.Fatalf("query notification id: %v", err)
	}

	if _, err := resolveIncidentForTest(s, "check-1"); err != nil {
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

	if _, err := openIncidentWithNotificationForTest(s, "check-1", &NotificationRequest{
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

	if _, err := resolveIncidentWithNotificationForTest(s, "check-1", &NotificationRequest{
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

func TestStatusCheckViews_ReturnAllChecksWithIncidentTimestamps(t *testing.T) {
	s := newTestStore(t)

	user, err := s.CreateUser("status-check-views@example.com", "pass", false)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if err := s.CreateCheck(testCheck("down-check", "http", "https://down.example.com"), user.ID); err != nil {
		t.Fatalf("CreateCheck down-check: %v", err)
	}
	if err := s.CreateCheck(testCheck("pending-check", "http", "https://pending.example.com"), user.ID); err != nil {
		t.Fatalf("CreateCheck pending-check: %v", err)
	}

	if _, err := openIncidentForTest(s, "down-check"); err != nil {
		t.Fatalf("OpenIncident: %v", err)
	}

	views, err := s.StatusCheckViews(user.ID)
	if err != nil {
		t.Fatalf("StatusCheckViews: %v", err)
	}
	if len(views) != 2 {
		t.Fatalf("expected 2 views, got %d", len(views))
	}
	if views[0].CheckID != "down-check" || views[0].Target != "https://down.example.com" {
		t.Fatalf("views[0] = %#v, want down-check metadata", views[0])
	}
	if views[0].IncidentSince == nil {
		t.Fatal("expected down-check incident timestamp")
	}
	if views[1].CheckID != "pending-check" || views[1].Target != "https://pending.example.com" {
		t.Fatalf("views[1] = %#v, want pending-check metadata", views[1])
	}
	if views[1].IncidentSince != nil {
		t.Fatal("expected pending-check to omit incident timestamp")
	}
}

func TestStatusCheckViews_ScopedToUser(t *testing.T) {
	s := newTestStore(t)

	alice, err := s.CreateUser("alice-status-views@example.com", "pass", false)
	if err != nil {
		t.Fatalf("CreateUser alice: %v", err)
	}
	bob, err := s.CreateUser("bob-status-views@example.com", "pass", false)
	if err != nil {
		t.Fatalf("CreateUser bob: %v", err)
	}

	if err := s.CreateCheck(testCheck("alice-check", "http", "https://alice.example.com"), alice.ID); err != nil {
		t.Fatalf("CreateCheck alice: %v", err)
	}
	if err := s.CreateCheck(testCheck("bob-check", "http", "https://bob.example.com"), bob.ID); err != nil {
		t.Fatalf("CreateCheck bob: %v", err)
	}
	if _, err := openIncidentForTest(s, "bob-check"); err != nil {
		t.Fatalf("OpenIncident bob: %v", err)
	}

	aliceViews, err := s.StatusCheckViews(alice.ID)
	if err != nil {
		t.Fatalf("StatusCheckViews alice: %v", err)
	}
	if len(aliceViews) != 1 {
		t.Fatalf("expected 1 alice view, got %d", len(aliceViews))
	}
	if aliceViews[0].CheckID != "alice-check" {
		t.Fatalf("expected alice-check, got %s", aliceViews[0].CheckID)
	}
	if aliceViews[0].IncidentSince != nil {
		t.Fatal("expected alice view to omit incident timestamp")
	}

	bobViews, err := s.StatusCheckViews(bob.ID)
	if err != nil {
		t.Fatalf("StatusCheckViews bob: %v", err)
	}
	if len(bobViews) != 1 {
		t.Fatalf("expected 1 bob view, got %d", len(bobViews))
	}
	if bobViews[0].CheckID != "bob-check" {
		t.Fatalf("expected bob-check, got %s", bobViews[0].CheckID)
	}
	if bobViews[0].IncidentSince == nil {
		t.Fatal("expected bob view to include incident timestamp")
	}
}

func TestPublicStatusCheckViews_DistinguishesUnknownSlugAndNoChecks(t *testing.T) {
	s := newTestStore(t)

	user, err := s.CreateUser("public-status-check-views@example.com", "pass", false)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	views, found, err := s.PublicStatusCheckViews(user.PublicStatusSlug)
	if err != nil {
		t.Fatalf("PublicStatusCheckViews existing slug: %v", err)
	}
	if !found {
		t.Fatal("expected existing slug to resolve")
	}
	if len(views) != 0 {
		t.Fatalf("expected no views for a user with no checks, got %d", len(views))
	}

	if err := s.CreateCheck(testCheck("down-check", "http", "https://down.example.com"), user.ID); err != nil {
		t.Fatalf("CreateCheck: %v", err)
	}
	if _, err := openIncidentForTest(s, "down-check"); err != nil {
		t.Fatalf("OpenIncident: %v", err)
	}

	views, found, err = s.PublicStatusCheckViews(user.PublicStatusSlug)
	if err != nil {
		t.Fatalf("PublicStatusCheckViews populated slug: %v", err)
	}
	if !found {
		t.Fatal("expected populated slug to resolve")
	}
	if len(views) != 1 {
		t.Fatalf("expected 1 view, got %d", len(views))
	}
	if views[0].CheckID != "down-check" {
		t.Fatalf("views[0].CheckID = %q, want down-check", views[0].CheckID)
	}
	if views[0].IncidentSince == nil {
		t.Fatal("expected down-check incident timestamp")
	}

	views, found, err = s.PublicStatusCheckViews("missing-slug")
	if err != nil {
		t.Fatalf("PublicStatusCheckViews missing slug: %v", err)
	}
	if found {
		t.Fatal("expected missing slug to report found=false")
	}
	if len(views) != 0 {
		t.Fatalf("expected no views for missing slug, got %d", len(views))
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
	if _, err := openIncidentForTest(s, "check-1"); err != nil {
		t.Fatalf("OpenIncident check-1: %v", err)
	}
	if _, err := resolveIncidentForTest(s, "check-1"); err != nil {
		t.Fatalf("ResolveIncident check-1: %v", err)
	}

	if _, err := openIncidentForTest(s, "check-2"); err != nil {
		t.Fatalf("OpenIncident check-2: %v", err)
	}
	if _, err := resolveIncidentForTest(s, "check-2"); err != nil {
		t.Fatalf("ResolveIncident check-2: %v", err)
	}

	if _, err := openIncidentForTest(s, "check-1"); err != nil {
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

	if _, err := openIncidentWithNotificationForTest(s, "check-1", &NotificationRequest{
		WebhookURL: "https://hooks.example.com/wacht",
		Payload:    []byte(`{"status":"down"}`),
	}); err != nil {
		t.Fatalf("OpenIncidentWithNotification: %v", err)
	}
	if _, err := resolveIncidentWithNotificationForTest(s, "check-1", &NotificationRequest{
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
		if _, err := openIncidentForTest(s, "check-1"); err != nil {
			t.Fatalf("OpenIncident: %v", err)
		}
		if _, err := resolveIncidentForTest(s, "check-1"); err != nil {
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

	if _, err := openIncidentForTest(s, "alice-check"); err != nil {
		t.Fatalf("OpenIncident alice: %v", err)
	}
	if _, err := openIncidentForTest(s, "bob-check"); err != nil {
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
