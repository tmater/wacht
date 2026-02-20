package store

import (
	"database/sql"
	"log"
	"time"

	"github.com/tmater/wacht/internal/proto"
	_ "modernc.org/sqlite"
)

// Store handles persistence of check results.
type Store struct {
	db *sql.DB
}

// New opens the SQLite database and creates tables if they don't exist.
func New(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}

	// Single connection prevents concurrent write contention in SQLite.
	db.SetMaxOpenConns(1)

	if _, err = db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		return nil, err
	}

	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS check_results (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			check_id    TEXT NOT NULL,
			probe_id    TEXT NOT NULL,
			type        TEXT NOT NULL,
			target      TEXT NOT NULL,
			up          BOOLEAN NOT NULL,
			latency_ms  INTEGER NOT NULL,
			error       TEXT,
			timestamp   DATETIME NOT NULL
		)
	`)
	if err != nil {
		return nil, err
	}

	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS probes (
			probe_id        TEXT PRIMARY KEY,
			version         TEXT NOT NULL,
			registered_at   DATETIME NOT NULL,
			last_seen_at    DATETIME NOT NULL
		)
	`)
	if err != nil {
		return nil, err
	}

	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS incidents (
			id              INTEGER PRIMARY KEY AUTOINCREMENT,
			check_id        TEXT NOT NULL,
			started_at      DATETIME NOT NULL,
			resolved_at     DATETIME
		)
	`)
	if err != nil {
		return nil, err
	}

	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS checks (
			id      TEXT PRIMARY KEY,
			type    TEXT NOT NULL,
			target  TEXT NOT NULL,
			webhook TEXT NOT NULL DEFAULT ''
		)
	`)
	if err != nil {
		return nil, err
	}

	log.Printf("store: database ready at %s", path)
	return &Store{db: db}, nil
}

// SaveResult persists a check result to the database.
func (s *Store) SaveResult(r proto.CheckResult) error {
	_, err := s.db.Exec(`
		INSERT INTO check_results (check_id, probe_id, type, target, up, latency_ms, error, timestamp)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
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
		WHERE check_id = ? AND probe_id = ?
		ORDER BY id DESC
		LIMIT ?
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
			WHERE check_id = ?
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

// RegisterProbe inserts or updates a probe record on startup.
func (s *Store) RegisterProbe(probeID, version string) error {
	now := time.Now().UTC()
	_, err := s.db.Exec(`
		INSERT INTO probes (probe_id, version, registered_at, last_seen_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(probe_id) DO UPDATE SET version=excluded.version, registered_at=excluded.registered_at, last_seen_at=excluded.last_seen_at
	`, probeID, version, now, now)
	return err
}

// UpdateProbeHeartbeat updates last_seen_at for a registered probe.
func (s *Store) UpdateProbeHeartbeat(probeID string) error {
	_, err := s.db.Exec(`UPDATE probes SET last_seen_at=? WHERE probe_id=?`, time.Now().UTC(), probeID)
	return err
}

// IsProbeRegistered reports whether a probe_id exists in the probes table.
func (s *Store) IsProbeRegistered(probeID string) (bool, error) {
	var count int
	err := s.db.QueryRow(`SELECT COUNT(1) FROM probes WHERE probe_id=?`, probeID).Scan(&count)
	return count > 0, err
}

// ProbeStatus holds a probe's last_seen_at for staleness checks.
type ProbeStatus struct {
	ProbeID    string
	LastSeenAt time.Time
}

// AllProbeStatuses returns the last_seen_at for all registered probes.
func (s *Store) AllProbeStatuses() ([]ProbeStatus, error) {
	rows, err := s.db.Query(`SELECT probe_id, last_seen_at FROM probes`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var statuses []ProbeStatus
	for rows.Next() {
		var ps ProbeStatus
		if err := rows.Scan(&ps.ProbeID, &ps.LastSeenAt); err != nil {
			return nil, err
		}
		statuses = append(statuses, ps)
	}
	return statuses, rows.Err()
}

// CheckStatus holds the current state of a check for the status page.
type CheckStatus struct {
	CheckID       string
	Target        string
	Up            bool
	IncidentSince *time.Time // non-nil when an incident is open
}

// CheckStatuses returns the current status for each check that has received
// at least one result, joined with any open incident.
func (s *Store) CheckStatuses() ([]CheckStatus, error) {
	rows, err := s.db.Query(`
		SELECT cr.check_id, cr.target, cr.up, i.started_at
		FROM check_results cr
		INNER JOIN (
			SELECT check_id, MAX(id) AS max_id
			FROM check_results
			GROUP BY check_id
		) latest ON cr.id = latest.max_id
		LEFT JOIN (
			SELECT check_id, MIN(started_at) AS started_at
			FROM incidents
			WHERE resolved_at IS NULL
			GROUP BY check_id
		) i ON cr.check_id = i.check_id
		ORDER BY cr.check_id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var statuses []CheckStatus
	for rows.Next() {
		var cs CheckStatus
		var startedAt *string
		if err := rows.Scan(&cs.CheckID, &cs.Target, &cs.Up, &startedAt); err != nil {
			return nil, err
		}
		if startedAt != nil {
			// SQLite stores time.Time as "2006-01-02 15:04:05.999999999 +0000 UTC"
			// Try several formats in order of likelihood.
			var t time.Time
			var parseErr error
			for _, layout := range []string{
				"2006-01-02 15:04:05.999999999 +0000 UTC",
				"2006-01-02 15:04:05 +0000 UTC",
				"2006-01-02 15:04:05",
				time.RFC3339,
			} {
				t, parseErr = time.Parse(layout, *startedAt)
				if parseErr == nil {
					break
				}
			}
			if parseErr != nil {
				return nil, parseErr
			}
			cs.IncidentSince = &t
		}
		statuses = append(statuses, cs)
	}
	return statuses, rows.Err()
}

// OpenIncident records a new incident for checkID. Returns true if an incident
// was already open (caller should skip alerting to avoid duplicate notifications).
func (s *Store) OpenIncident(checkID string) (alreadyOpen bool, err error) {
	var count int
	err = s.db.QueryRow(`SELECT COUNT(1) FROM incidents WHERE check_id=? AND resolved_at IS NULL`, checkID).Scan(&count)
	if err != nil {
		return false, err
	}
	if count > 0 {
		return true, nil
	}
	_, err = s.db.Exec(`INSERT INTO incidents (check_id, started_at) VALUES (?, ?)`, checkID, time.Now().UTC())
	return false, err
}

// ResolveIncident marks the open incident for checkID as resolved.
func (s *Store) ResolveIncident(checkID string) error {
	_, err := s.db.Exec(`UPDATE incidents SET resolved_at=? WHERE check_id=? AND resolved_at IS NULL`, time.Now().UTC(), checkID)
	return err
}

// Check represents a monitored endpoint stored in the database.
type Check struct {
	ID      string `json:"ID"`
	Type    string `json:"Type"`
	Target  string `json:"Target"`
	Webhook string `json:"Webhook"`
}

// SeedChecks inserts checks that do not already exist in the database.
// Existing checks (matched by id) are left unchanged. Used to bootstrap
// from YAML config on startup without overwriting DB-managed checks.
func (s *Store) SeedChecks(checks []Check) error {
	for _, c := range checks {
		_, err := s.db.Exec(`
			INSERT INTO checks (id, type, target, webhook)
			VALUES (?, ?, ?, ?)
			ON CONFLICT(id) DO NOTHING
		`, c.ID, c.Type, c.Target, c.Webhook)
		if err != nil {
			return err
		}
	}
	return nil
}

// ListChecks returns all checks from the database.
func (s *Store) ListChecks() ([]Check, error) {
	rows, err := s.db.Query(`SELECT id, type, target, webhook FROM checks ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var checks []Check
	for rows.Next() {
		var c Check
		if err := rows.Scan(&c.ID, &c.Type, &c.Target, &c.Webhook); err != nil {
			return nil, err
		}
		checks = append(checks, c)
	}
	return checks, rows.Err()
}

// GetCheck returns a single check by id, or (nil, nil) if not found.
func (s *Store) GetCheck(id string) (*Check, error) {
	var c Check
	err := s.db.QueryRow(`SELECT id, type, target, webhook FROM checks WHERE id=?`, id).
		Scan(&c.ID, &c.Type, &c.Target, &c.Webhook)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// CreateCheck inserts a new check. Returns an error if the id already exists.
func (s *Store) CreateCheck(c Check) error {
	_, err := s.db.Exec(`INSERT INTO checks (id, type, target, webhook) VALUES (?, ?, ?, ?)`,
		c.ID, c.Type, c.Target, c.Webhook)
	return err
}

// UpdateCheck replaces type, target, and webhook for an existing check.
func (s *Store) UpdateCheck(c Check) error {
	_, err := s.db.Exec(`UPDATE checks SET type=?, target=?, webhook=? WHERE id=?`,
		c.Type, c.Target, c.Webhook, c.ID)
	return err
}

// DeleteCheck removes a check by id.
func (s *Store) DeleteCheck(id string) error {
	_, err := s.db.Exec(`DELETE FROM checks WHERE id=?`, id)
	return err
}

// Close closes the database connection.
func (s *Store) Close() error {
	return s.db.Close()
}
