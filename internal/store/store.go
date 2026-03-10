package store

import (
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

// Store handles persistence of check results.
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
	_, err := s.db.Exec(`
		INSERT INTO check_results (check_id, probe_id, type, target, up, latency_ms, error, timestamp)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`,
		r.CheckID,
		r.ProbeID,
		r.Type,
		r.Target,
		r.Up,
		r.Latency/time.Millisecond,
		r.Error,
		r.Timestamp,
	)
	return err
}

// RecentResultsByProbe returns the last n results for a specific probe+check,
// ordered newest first. Used for consecutive failure detection.
func (s *Store) RecentResultsByProbe(checkID, probeID string, n int) ([]proto.CheckResult, error) {
	rows, err := s.db.Query(`
		SELECT probe_id, up
		FROM check_results
		WHERE check_id = $1 AND probe_id = $2
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
			WHERE check_id = $1
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

// CheckStatuses returns the current status for each reported check owned by
// userID, joined with any open incident.
func (s *Store) CheckStatuses(userID int64) ([]CheckStatus, error) {
	rows, err := s.db.Query(`
		SELECT c.id, c.target, i.started_at
		FROM checks c
		INNER JOIN (
			SELECT DISTINCT check_id
			FROM check_results
		) reported ON reported.check_id = c.id
		LEFT JOIN incidents i
			ON i.check_id = c.id AND i.resolved_at IS NULL
		WHERE c.user_id = $1
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

// OpenIncident records a new incident for checkID. Returns true if an incident
// was already open (caller should skip alerting to avoid duplicate notifications).
func (s *Store) OpenIncident(checkID string) (alreadyOpen bool, err error) {
	var incidentID int64
	err = s.db.QueryRow(`
		INSERT INTO incidents (check_id, user_id, started_at)
		VALUES ($1, (SELECT user_id FROM checks WHERE id = $1), $2)
		ON CONFLICT (check_id) WHERE resolved_at IS NULL DO NOTHING
		RETURNING id
	`, checkID, time.Now().UTC()).Scan(&incidentID)
	if err == sql.ErrNoRows {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return false, nil
}

// ResolveIncident marks the open incident for checkID as resolved. It returns
// true when an open incident was actually closed.
func (s *Store) ResolveIncident(checkID string) (resolved bool, err error) {
	res, err := s.db.Exec(`UPDATE incidents SET resolved_at=$1 WHERE check_id=$2 AND resolved_at IS NULL`, time.Now().UTC(), checkID)
	if err != nil {
		return false, err
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return rows > 0, nil
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
			ON CONFLICT (id) DO NOTHING
		`, c.ID, string(c.Type), c.Target, c.Webhook, userID, c.Interval)
		if err != nil {
			return err
		}
	}
	return nil
}

// ListChecks returns all checks owned by userID.
func (s *Store) ListChecks(userID int64) ([]checks.Check, error) {
	rows, err := s.db.Query(`SELECT id, type, target, webhook, interval_seconds FROM checks WHERE user_id=$1 ORDER BY id`, userID)
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
	rows, err := s.db.Query(`SELECT id, type, target, webhook, interval_seconds FROM checks ORDER BY id`)
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
	c, err := scanCheck(s.db.QueryRow(`SELECT id, type, target, webhook, interval_seconds FROM checks WHERE id=$1`, id))
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
	_, err := s.db.Exec(`UPDATE checks SET type=$1, target=$2, webhook=$3, interval_seconds=$4 WHERE id=$5 AND user_id=$6`,
		string(c.Type), c.Target, c.Webhook, c.Interval, c.ID, userID)
	return err
}

// DeleteCheck removes a check owned by userID.
func (s *Store) DeleteCheck(id string, userID int64) error {
	_, err := s.db.Exec(`DELETE FROM checks WHERE id=$1 AND user_id=$2`, id, userID)
	return err
}

// Incident represents a recorded outage for a check.
type Incident struct {
	ID         int64
	CheckID    string
	StartedAt  time.Time
	ResolvedAt *time.Time
}

// ListIncidents returns the most recent limit incidents for userID ordered
// newest first.
func (s *Store) ListIncidents(userID int64, limit int) ([]Incident, error) {
	rows, err := s.db.Query(`
		SELECT id, check_id, started_at, resolved_at
		FROM incidents
		WHERE user_id = $1
		ORDER BY started_at DESC
		LIMIT $2
	`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var incidents []Incident
	for rows.Next() {
		var inc Incident
		if err := rows.Scan(&inc.ID, &inc.CheckID, &inc.StartedAt, &inc.ResolvedAt); err != nil {
			return nil, err
		}
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
