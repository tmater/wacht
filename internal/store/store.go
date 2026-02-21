package store

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"log"
	"time"

	"github.com/tmater/wacht/internal/proto"
	"golang.org/x/crypto/bcrypt"
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
			webhook TEXT NOT NULL DEFAULT '',
			user_id INTEGER
		)
	`)
	if err != nil {
		return nil, err
	}

	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS users (
			id            INTEGER PRIMARY KEY AUTOINCREMENT,
			email         TEXT NOT NULL UNIQUE,
			password_hash TEXT NOT NULL,
			created_at    DATETIME NOT NULL
		)
	`)
	if err != nil {
		return nil, err
	}

	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS sessions (
			token      TEXT PRIMARY KEY,
			user_id    INTEGER NOT NULL REFERENCES users(id),
			created_at DATETIME NOT NULL,
			expires_at DATETIME NOT NULL
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

// ListChecks returns all checks owned by userID.
func (s *Store) ListChecks(userID int64) ([]Check, error) {
	rows, err := s.db.Query(`SELECT id, type, target, webhook FROM checks WHERE user_id=? ORDER BY id`, userID)
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

// ListAllChecks returns all checks regardless of owner. Used by probes.
func (s *Store) ListAllChecks() ([]Check, error) {
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

// CreateCheck inserts a new check owned by userID.
func (s *Store) CreateCheck(c Check, userID int64) error {
	_, err := s.db.Exec(`INSERT INTO checks (id, type, target, webhook, user_id) VALUES (?, ?, ?, ?, ?)`,
		c.ID, c.Type, c.Target, c.Webhook, userID)
	return err
}

// UpdateCheck replaces type, target, and webhook for a check owned by userID.
func (s *Store) UpdateCheck(c Check, userID int64) error {
	_, err := s.db.Exec(`UPDATE checks SET type=?, target=?, webhook=? WHERE id=? AND user_id=?`,
		c.Type, c.Target, c.Webhook, c.ID, userID)
	return err
}

// DeleteCheck removes a check owned by userID.
func (s *Store) DeleteCheck(id string, userID int64) error {
	_, err := s.db.Exec(`DELETE FROM checks WHERE id=? AND user_id=?`, id, userID)
	return err
}

// User represents a registered user.
type User struct {
	ID           int64
	Email        string
	PasswordHash string
	CreatedAt    time.Time
}

// CreateUser hashes the password and inserts a new user. Returns the created user.
func (s *Store) CreateUser(email, password string) (*User, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	res, err := s.db.Exec(`INSERT INTO users (email, password_hash, created_at) VALUES (?, ?, ?)`,
		email, string(hash), now)
	if err != nil {
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	return &User{ID: id, Email: email, PasswordHash: string(hash), CreatedAt: now}, nil
}

// AuthenticateUser verifies email+password and returns the user on success.
func (s *Store) AuthenticateUser(email, password string) (*User, error) {
	var u User
	err := s.db.QueryRow(`SELECT id, email, password_hash, created_at FROM users WHERE email=?`, email).
		Scan(&u.ID, &u.Email, &u.PasswordHash, &u.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)); err != nil {
		return nil, nil // wrong password
	}
	return &u, nil
}

// UserExists reports whether any user exists in the database.
func (s *Store) UserExists() (bool, error) {
	var count int
	err := s.db.QueryRow(`SELECT COUNT(1) FROM users`).Scan(&count)
	return count > 0, err
}

// CreateSession generates a random token, stores it, and returns it.
// Sessions expire after 30 days.
func (s *Store) CreateSession(userID int64) (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	token := hex.EncodeToString(b)
	now := time.Now().UTC()
	_, err := s.db.Exec(`INSERT INTO sessions (token, user_id, created_at, expires_at) VALUES (?, ?, ?, ?)`,
		token, userID, now, now.Add(30*24*time.Hour))
	if err != nil {
		return "", err
	}
	return token, nil
}

// GetSessionUser returns the user for a valid, non-expired session token.
// Returns nil if the token is missing or expired.
func (s *Store) GetSessionUser(token string) (*User, error) {
	var u User
	err := s.db.QueryRow(`
		SELECT u.id, u.email, u.password_hash, u.created_at
		FROM sessions s
		JOIN users u ON u.id = s.user_id
		WHERE s.token = ? AND s.expires_at > ?
	`, token, time.Now().UTC()).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// DeleteSession removes a session token (logout).
func (s *Store) DeleteSession(token string) error {
	_, err := s.db.Exec(`DELETE FROM sessions WHERE token=?`, token)
	return err
}

// Close closes the database connection.
func (s *Store) Close() error {
	return s.db.Close()
}
