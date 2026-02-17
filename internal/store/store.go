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

// Close closes the database connection.
func (s *Store) Close() error {
	return s.db.Close()
}
