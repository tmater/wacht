package store

import (
	"context"
	"database/sql"
	"embed"
	"log"
	"time"

	"github.com/golang-migrate/migrate/v4"
	pgxmigrate "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/tmater/wacht/internal/checks"
	"github.com/tmater/wacht/internal/proto"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Store handles persistence for metadata, incidents, and monitoring recovery.
type Store struct {
	db *sql.DB
}

// New opens the Postgres database and runs any pending migrations.
func New(dsn string) (*Store, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, err
	}

	if err := runMigrations(db, dsn); err != nil {
		return nil, err
	}

	log.Printf("store: database ready")
	return &Store{db: db}, nil
}

func runMigrations(db *sql.DB, dsn string) error {
	src, err := iofs.New(migrationsFS, "migrations")
	if err != nil {
		return err
	}

	driver, err := pgxmigrate.WithInstance(db, &pgxmigrate.Config{})
	if err != nil {
		return err
	}

	m, err := migrate.NewWithInstance("iofs", src, "pgx", driver)
	if err != nil {
		return err
	}

	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		return err
	}
	return nil
}

// SaveResult persists a check result to the database.
func (s *Store) SaveResult(r proto.CheckResult) error {
	var resultID int64
	err := s.db.QueryRow(`
		INSERT INTO check_results (check_uid, probe_id, type, target, up, latency_ms, error, timestamp)
		SELECT uid, $2, $3, $4, $5, $6, $7, $8
		FROM checks
		WHERE id = $1 AND deleted_at IS NULL
		RETURNING id
	`,
		r.CheckID,
		r.ProbeID,
		r.Type,
		r.Target,
		r.Up,
		r.Latency/time.Millisecond,
		r.Error,
		r.Timestamp,
	).Scan(&resultID)
	return err
}

// RecentResultsByProbe returns the last n results for a specific probe+check,
// ordered newest first. Used for consecutive failure detection.
func (s *Store) RecentResultsByProbe(checkID, probeID string, n int) ([]proto.CheckResult, error) {
	rows, err := s.db.Query(`
		SELECT probe_id, up
		FROM check_results
		WHERE check_uid = (
			SELECT uid
			FROM checks
			WHERE id = $1 AND deleted_at IS NULL
		)
		  AND probe_id = $2
		ORDER BY id DESC
		LIMIT $3
	`, checkID, probeID, n)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []proto.CheckResult
	for rows.Next() {
		var r proto.CheckResult
		if err := rows.Scan(&r.ProbeID, &r.Up); err != nil {
			return nil, err
		}
		results = append(results, r)
	}
	return results, rows.Err()
}

// RecentResultsPerProbe returns the most recent result for each probe that has
// reported for the given check_id. This is used for quorum evaluation.
func (s *Store) RecentResultsPerProbe(checkID string) ([]proto.CheckResult, error) {
	rows, err := s.db.Query(`
		SELECT probe_id, up
		FROM check_results
		WHERE id IN (
			SELECT MAX(id)
			FROM check_results
			WHERE check_uid = (
				SELECT uid
				FROM checks
				WHERE id = $1 AND deleted_at IS NULL
			)
			GROUP BY probe_id
		)
	`, checkID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []proto.CheckResult
	for rows.Next() {
		var r proto.CheckResult
		if err := rows.Scan(&r.ProbeID, &r.Up); err != nil {
			return nil, err
		}
		results = append(results, r)
	}
	return results, rows.Err()
}

// CheckStatus holds the current state of a check for the status page.
type CheckStatus struct {
	CheckID       string
	Target        string
	Up            bool
	IncidentSince *time.Time // non-nil when an incident is open
}

// PublicCheckStatus holds the public-safe state of a check for a public status
// page. Unlike the authenticated status view, this never exposes targets.
type PublicCheckStatus struct {
	CheckID       string
	Status        string
	IncidentSince *time.Time
}

// StatusCheckView holds the metadata and durable incident timestamp needed to
// render one authenticated status row from runtime-owned state.
type StatusCheckView struct {
	CheckID       string
	Target        string
	IncidentSince *time.Time
}

// PublicStatusCheckView holds the public-safe metadata and durable incident
// timestamp needed to render one public status row from runtime-owned state.
type PublicStatusCheckView struct {
	CheckID       string
	IncidentSince *time.Time
}

// CheckStatuses returns the current status for each reported check owned by
// userID, joined with any open incident.
func (s *Store) CheckStatuses(userID int64) ([]CheckStatus, error) {
	rows, err := s.db.Query(`
		SELECT c.id, c.target, i.started_at
		FROM checks c
		INNER JOIN (
			SELECT DISTINCT check_uid
			FROM check_results
		) reported ON reported.check_uid = c.uid
		LEFT JOIN incidents i
			ON i.check_uid = c.uid AND i.resolved_at IS NULL
		WHERE c.user_id = $1
		  AND c.deleted_at IS NULL
		ORDER BY c.id
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var statuses []CheckStatus
	for rows.Next() {
		var cs CheckStatus
		var startedAt *time.Time
		if err := rows.Scan(&cs.CheckID, &cs.Target, &startedAt); err != nil {
			return nil, err
		}
		cs.Up = startedAt == nil
		cs.IncidentSince = startedAt
		statuses = append(statuses, cs)
	}
	return statuses, rows.Err()
}

// PublicCheckStatuses returns the public-safe status view for the user matched
// by slug. The boolean reports whether the slug exists at all so callers can
// distinguish "no checks yet" from "unknown page".
func (s *Store) PublicCheckStatuses(slug string) ([]PublicCheckStatus, bool, error) {
	var userID int64
	err := s.db.QueryRow(`SELECT id FROM users WHERE public_status_slug = $1`, slug).Scan(&userID)
	if err == sql.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}

	rows, err := s.db.Query(`
		SELECT c.id,
		       CASE
		           WHEN reported.check_uid IS NULL THEN 'pending'
		           WHEN i.started_at IS NULL THEN 'up'
		           ELSE 'down'
		       END AS status,
		       i.started_at
		FROM checks c
		LEFT JOIN (
			SELECT DISTINCT check_uid
			FROM check_results
		) reported ON reported.check_uid = c.uid
		LEFT JOIN incidents i
			ON i.check_uid = c.uid AND i.resolved_at IS NULL
		WHERE c.user_id = $1
		  AND c.deleted_at IS NULL
		ORDER BY c.id
	`, userID)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()

	var statuses []PublicCheckStatus
	for rows.Next() {
		var (
			status    PublicCheckStatus
			startedAt *time.Time
		)
		if err := rows.Scan(&status.CheckID, &status.Status, &startedAt); err != nil {
			return nil, false, err
		}
		status.IncidentSince = startedAt
		statuses = append(statuses, status)
	}
	return statuses, true, rows.Err()
}

// StatusCheckViews returns all active checks owned by userID plus any open
// incident timestamp. Runtime-owned status APIs merge this metadata with
// monitoring state in memory.
func (s *Store) StatusCheckViews(userID int64) ([]StatusCheckView, error) {
	rows, err := s.db.Query(`
		SELECT c.id, c.target, i.started_at
		FROM checks c
		LEFT JOIN incidents i
			ON i.check_uid = c.uid AND i.resolved_at IS NULL
		WHERE c.user_id = $1
		  AND c.deleted_at IS NULL
		ORDER BY c.id
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var views []StatusCheckView
	for rows.Next() {
		var (
			view      StatusCheckView
			startedAt *time.Time
		)
		if err := rows.Scan(&view.CheckID, &view.Target, &startedAt); err != nil {
			return nil, err
		}
		view.IncidentSince = startedAt
		views = append(views, view)
	}
	return views, rows.Err()
}

// PublicStatusCheckViews returns all active checks for the user matched by
// slug, plus any open incident timestamp. The boolean reports whether the slug
// exists at all so callers can distinguish "no checks yet" from "unknown page".
func (s *Store) PublicStatusCheckViews(slug string) ([]PublicStatusCheckView, bool, error) {
	var userID int64
	err := s.db.QueryRow(`SELECT id FROM users WHERE public_status_slug = $1`, slug).Scan(&userID)
	if err == sql.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}

	rows, err := s.db.Query(`
		SELECT c.id, i.started_at
		FROM checks c
		LEFT JOIN incidents i
			ON i.check_uid = c.uid AND i.resolved_at IS NULL
		WHERE c.user_id = $1
		  AND c.deleted_at IS NULL
		ORDER BY c.id
	`, userID)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()

	var views []PublicStatusCheckView
	for rows.Next() {
		var (
			view      PublicStatusCheckView
			startedAt *time.Time
		)
		if err := rows.Scan(&view.CheckID, &startedAt); err != nil {
			return nil, false, err
		}
		view.IncidentSince = startedAt
		views = append(views, view)
	}
	return views, true, rows.Err()
}

// OpenIncident records a new incident for checkID. Returns true if an incident
// was already open (caller should skip alerting to avoid duplicate notifications).
func (s *Store) OpenIncident(checkID string) (alreadyOpen bool, err error) {
	return s.openIncidentWithNotification(checkID, nil)
}

// OpenIncidentWithNotification records a new incident and durable webhook work
// for the "down" transition in one transaction.
func (s *Store) OpenIncidentWithNotification(checkID string, request *NotificationRequest) (alreadyOpen bool, err error) {
	return s.openIncidentWithNotification(checkID, request)
}

// ResolveIncident marks the open incident for checkID as resolved. It returns
// true when an open incident was actually closed.
func (s *Store) ResolveIncident(checkID string) (resolved bool, err error) {
	return s.resolveIncidentWithNotification(checkID, nil)
}

// ResolveIncidentWithNotification resolves an incident and durable webhook work
// for the "up" transition in one transaction.
func (s *Store) ResolveIncidentWithNotification(checkID string, request *NotificationRequest) (resolved bool, err error) {
	return s.resolveIncidentWithNotification(checkID, request)
}

// SeedChecks inserts checks that do not already exist in the database.
// Existing checks (matched by id) are left unchanged. Used to bootstrap
// from YAML config on startup without overwriting DB-managed checks.
// If userID is non-zero, newly inserted checks are assigned to that user.
func (s *Store) SeedChecks(checks []checks.Check, userID int64) error {
	for _, c := range checks {
		_, err := s.db.Exec(`
			INSERT INTO checks (id, type, target, webhook, user_id, interval_seconds)
			VALUES ($1, $2, $3, $4, NULLIF($5, 0), $6)
			ON CONFLICT DO NOTHING
		`, c.ID, string(c.Type), c.Target, c.Webhook, userID, c.Interval)
		if err != nil {
			return err
		}
	}
	return nil
}

// ListChecks returns all checks owned by userID.
func (s *Store) ListChecks(userID int64) ([]checks.Check, error) {
	rows, err := s.db.Query(`
		SELECT id, type, target, webhook, interval_seconds
		FROM checks
		WHERE user_id = $1
		  AND deleted_at IS NULL
		ORDER BY id
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var checks []checks.Check
	for rows.Next() {
		c, err := scanCheck(rows)
		if err != nil {
			return nil, err
		}
		checks = append(checks, c)
	}
	return checks, rows.Err()
}

// ListAllChecks returns all checks regardless of owner. Used by probes.
func (s *Store) ListAllChecks() ([]checks.Check, error) {
	rows, err := s.db.Query(`
		SELECT id, type, target, webhook, interval_seconds
		FROM checks
		WHERE deleted_at IS NULL
		ORDER BY id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var checks []checks.Check
	for rows.Next() {
		c, err := scanCheck(rows)
		if err != nil {
			return nil, err
		}
		checks = append(checks, c)
	}
	return checks, rows.Err()
}

// GetCheck returns a single check by id, or (nil, nil) if not found.
func (s *Store) GetCheck(id string) (*checks.Check, error) {
	c, err := scanCheck(s.db.QueryRow(`
		SELECT id, type, target, webhook, interval_seconds
		FROM checks
		WHERE id = $1
		  AND deleted_at IS NULL
	`, id))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// CreateCheck inserts a new check owned by userID.
func (s *Store) CreateCheck(c checks.Check, userID int64) error {
	_, err := s.db.Exec(`INSERT INTO checks (id, type, target, webhook, user_id, interval_seconds) VALUES ($1, $2, $3, $4, $5, $6)`,
		c.ID, string(c.Type), c.Target, c.Webhook, userID, c.Interval)
	return err
}

// UpdateCheck replaces type, target, webhook, and interval_seconds for a check owned by userID.
func (s *Store) UpdateCheck(c checks.Check, userID int64) error {
	_, err := s.db.Exec(`
		UPDATE checks
		SET type = $1, target = $2, webhook = $3, interval_seconds = $4
		WHERE id = $5
		  AND user_id = $6
		  AND deleted_at IS NULL
	`,
		string(c.Type), c.Target, c.Webhook, c.Interval, c.ID, userID)
	return err
}

// DeleteCheck removes a check owned by userID. It returns whether an active
// owned check was deleted; unauthorized, missing, or already-deleted checks are
// treated as idempotent no-ops.
func (s *Store) DeleteCheck(id string, userID int64) (bool, error) {
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()

	var checkUID int64
	err = tx.QueryRow(`
		SELECT uid
		FROM checks
		WHERE id = $1
		  AND user_id = $2
		  AND deleted_at IS NULL
		FOR UPDATE
	`, id, userID).Scan(&checkUID)
	if err == sql.ErrNoRows {
		return false, tx.Commit()
	}
	if err != nil {
		return false, err
	}

	now := time.Now().UTC()

	if _, err := tx.Exec(`
		UPDATE incident_notifications n
		SET state = $1,
		    next_attempt_at = NULL,
		    updated_at = $2
		FROM incidents i
		WHERE i.id = n.incident_id
		  AND i.check_uid = $3
		  AND n.state NOT IN ($1, $4)
	`, notificationStateSuperseded, now, checkUID, notificationStateDelivered); err != nil {
		return false, err
	}

	if _, err := tx.Exec(`
		UPDATE incidents
		SET resolved_at = $1
		WHERE check_uid = $2
		  AND resolved_at IS NULL
	`, now, checkUID); err != nil {
		return false, err
	}

	if _, err := tx.Exec(`
		UPDATE checks
		SET deleted_at = $1
		WHERE uid = $2
	`, now, checkUID); err != nil {
		return false, err
	}

	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

// Incident represents a recorded outage for a check.
type Incident struct {
	ID               int64
	CheckID          string
	StartedAt        time.Time
	ResolvedAt       *time.Time
	DownNotification *IncidentNotification
	UpNotification   *IncidentNotification
}

// ListIncidents returns the most recent limit incidents for userID ordered
// newest first.
func (s *Store) ListIncidents(userID int64, limit int) ([]Incident, error) {
	rows, err := s.db.Query(`
		SELECT
			i.id,
			c.id,
			i.started_at,
			i.resolved_at,
			down_n.id,
			down_n.state,
			down_n.attempts,
			down_n.last_error,
			down_n.last_attempt_at,
			down_n.next_attempt_at,
			down_n.delivered_at,
			up_n.id,
			up_n.state,
			up_n.attempts,
			up_n.last_error,
			up_n.last_attempt_at,
			up_n.next_attempt_at,
			up_n.delivered_at
		FROM incidents i
		INNER JOIN checks c
			ON c.uid = i.check_uid
		LEFT JOIN incident_notifications down_n
			ON down_n.incident_id = i.id AND down_n.event = $2
		LEFT JOIN incident_notifications up_n
			ON up_n.incident_id = i.id AND up_n.event = $3
		WHERE i.user_id = $1
		ORDER BY i.started_at DESC
		LIMIT $4
	`, userID, notificationEventDown, notificationEventUp, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var incidents []Incident
	for rows.Next() {
		var inc Incident
		var (
			downID, upID                                          sql.NullInt64
			downState, upState                                    sql.NullString
			downAttempts, upAttempts                              sql.NullInt32
			downError, upError                                    sql.NullString
			downLastAttemptAt, downNextAttemptAt, downDeliveredAt sql.NullTime
			upLastAttemptAt, upNextAttemptAt, upDeliveredAt       sql.NullTime
		)
		if err := rows.Scan(
			&inc.ID,
			&inc.CheckID,
			&inc.StartedAt,
			&inc.ResolvedAt,
			&downID,
			&downState,
			&downAttempts,
			&downError,
			&downLastAttemptAt,
			&downNextAttemptAt,
			&downDeliveredAt,
			&upID,
			&upState,
			&upAttempts,
			&upError,
			&upLastAttemptAt,
			&upNextAttemptAt,
			&upDeliveredAt,
		); err != nil {
			return nil, err
		}
		inc.DownNotification = scanIncidentNotification(downID, downState, downAttempts, downError, downLastAttemptAt, downNextAttemptAt, downDeliveredAt)
		inc.UpNotification = scanIncidentNotification(upID, upState, upAttempts, upError, upLastAttemptAt, upNextAttemptAt, upDeliveredAt)
		incidents = append(incidents, inc)
	}
	return incidents, rows.Err()
}

// EvictOldResults deletes check_results older than the given cutoff.
// Returns the number of rows deleted.
func (s *Store) EvictOldResults(cutoff time.Time) (int64, error) {
	res, err := s.db.Exec(`DELETE FROM check_results WHERE timestamp < $1`, cutoff)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// Close closes the database connection.
func (s *Store) Close() error {
	return s.db.Close()
}

type rowScanner interface {
	Scan(dest ...any) error
}

// scanCheck works for both *sql.Row and *sql.Rows via their shared Scan method.
func scanCheck(scanner rowScanner) (checks.Check, error) {
	var c checks.Check
	var checkType string
	if err := scanner.Scan(&c.ID, &checkType, &c.Target, &c.Webhook, &c.Interval); err != nil {
		return checks.Check{}, err
	}
	c.Type = checks.Type(checkType)
	return c, nil
}
