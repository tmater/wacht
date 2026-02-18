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
			check_id        TEXT PRIMARY KEY,
			started_at      DATETIME NOT NULL,
			resolved_at     DATETIME
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
		LEFT JOIN incidents i ON cr.check_id = i.check_id AND i.resolved_at IS NULL
		ORDER BY cr.check_id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var statuses []CheckStatus
	for rows.Next() {
		var cs CheckStatus
		var startedAt *time.Time
		if err := rows.Scan(&cs.CheckID, &cs.Target, &cs.Up, &startedAt); err != nil {
			return nil, err
		}
		cs.IncidentSince = startedAt
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

// Close closes the database connection.
func (s *Store) Close() error {
	return s.db.Close()
}
