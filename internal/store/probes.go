package store

import (
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"time"
)

// ProbeSeed is a pre-provisioned probe credential loaded from config.
type ProbeSeed struct {
	ProbeID string
	Secret  string
}

// Probe is an authenticated or stored probe record.
type Probe struct {
	ProbeID      string
	Version      string
	Status       string
	RegisteredAt time.Time
	LastSeenAt   *time.Time
}

func hashProbeSecret(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

// SeedProbes upserts pre-provisioned probe credentials. Existing timestamps and
// metadata are preserved so probes remain visible across restarts.
func (s *Store) SeedProbes(probes []ProbeSeed) error {
	if len(probes) == 0 {
		return nil
	}

	keep := make(map[string]struct{}, len(probes))
	for _, probe := range probes {
		keep[probe.ProbeID] = struct{}{}
		now := time.Now().UTC()
		_, err := s.db.Exec(`
			INSERT INTO probes (probe_id, secret_hash, status, version, registered_at, last_seen_at)
			VALUES ($1, $2, 'active', '', $3, NULL)
			ON CONFLICT (probe_id) DO UPDATE
			SET secret_hash = excluded.secret_hash,
			    status = 'active'
		`, probe.ProbeID, hashProbeSecret(probe.Secret), now)
		if err != nil {
			return err
		}
	}

	rows, err := s.db.Query(`SELECT probe_id FROM probes`)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var probeID string
		if err := rows.Scan(&probeID); err != nil {
			return err
		}
		if _, ok := keep[probeID]; ok {
			continue
		}
		if _, err := s.db.Exec(`UPDATE probes SET status='revoked' WHERE probe_id=$1`, probeID); err != nil {
			return err
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	return nil
}

// AuthenticateProbe returns the active probe record for the given probe_id and
// secret. Returns nil if the credentials are invalid.
func (s *Store) AuthenticateProbe(probeID, secret string) (*Probe, error) {
	var (
		probe      Probe
		secretHash string
		lastSeen   sql.NullTime
	)
	err := s.db.QueryRow(`
		SELECT probe_id, version, status, registered_at, last_seen_at, secret_hash
		FROM probes
		WHERE probe_id = $1
	`, probeID).Scan(
		&probe.ProbeID,
		&probe.Version,
		&probe.Status,
		&probe.RegisteredAt,
		&lastSeen,
		&secretHash,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if probe.Status != "active" {
		return nil, nil
	}
	if subtle.ConstantTimeCompare([]byte(secretHash), []byte(hashProbeSecret(secret))) != 1 {
		return nil, nil
	}
	if lastSeen.Valid {
		t := lastSeen.Time
		probe.LastSeenAt = &t
	}
	return &probe, nil
}

// RegisterProbe records a successful authenticated startup for a probe.
func (s *Store) RegisterProbe(probeID, version string) error {
	_, err := s.db.Exec(`
		UPDATE probes
		SET version = $1, last_seen_at = $2
		WHERE probe_id = $3 AND status = 'active'
	`, version, time.Now().UTC(), probeID)
	return err
}

// UpdateProbeHeartbeat updates last_seen_at for an active, authenticated probe.
func (s *Store) UpdateProbeHeartbeat(probeID string) error {
	_, err := s.db.Exec(`
		UPDATE probes
		SET last_seen_at = $1
		WHERE probe_id = $2 AND status = 'active'
	`, time.Now().UTC(), probeID)
	return err
}

// ProbeStatus holds a probe's last_seen_at for staleness checks.
type ProbeStatus struct {
	ProbeID    string
	LastSeenAt *time.Time
}

// AllProbeStatuses returns the last_seen_at for all active probes. Internal
// server maintenance code uses this global view.
func (s *Store) AllProbeStatuses() ([]ProbeStatus, error) {
	rows, err := s.db.Query(`
		SELECT probe_id, last_seen_at
		FROM probes
		WHERE status = 'active'
		ORDER BY probe_id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanProbeStatuses(rows)
}

// ProbeStatuses returns the last_seen_at for active probes that have reported
// on checks owned by userID.
func (s *Store) ProbeStatuses(userID int64) ([]ProbeStatus, error) {
	rows, err := s.db.Query(`
		SELECT probe_id, last_seen_at
		FROM probes
		WHERE status = 'active'
		  AND EXISTS (
			SELECT 1
			FROM check_results cr
			INNER JOIN checks c ON c.id = cr.check_id
			WHERE cr.probe_id = probes.probe_id
			  AND c.user_id = $1
		  )
		ORDER BY probe_id
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanProbeStatuses(rows)
}

func scanProbeStatuses(rows *sql.Rows) ([]ProbeStatus, error) {
	var statuses []ProbeStatus
	for rows.Next() {
		var (
			ps       ProbeStatus
			lastSeen sql.NullTime
		)
		if err := rows.Scan(&ps.ProbeID, &lastSeen); err != nil {
			return nil, err
		}
		if lastSeen.Valid {
			t := lastSeen.Time
			ps.LastSeenAt = &t
		}
		statuses = append(statuses, ps)
	}
	return statuses, rows.Err()
}
