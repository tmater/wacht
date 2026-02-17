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

// Close closes the database connection.
func (s *Store) Close() error {
	return s.db.Close()
}
