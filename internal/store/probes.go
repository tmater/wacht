package store

import (
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

var (
	// ErrInvalidProbeID reports an API-created probe ID that is empty or uses
	// unsupported characters.
	ErrInvalidProbeID = errors.New("store: invalid probe id")
	// ErrProbeAlreadyExists reports that a requested probe ID is already taken.
	ErrProbeAlreadyExists = errors.New("store: probe id already exists")

	probeIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)
)

// ProbeSeed is a pre-provisioned probe credential loaded from config.
type ProbeSeed struct {
	ProbeID string
	Secret  string
}

// ProbeCredential is a newly provisioned reusable probe credential. Secret is
// returned to the caller at creation time; only its hash is stored.
type ProbeCredential struct {
	ProbeID string
	Secret  string
}

// Probe is an authenticated or stored probe record.
type Probe struct {
	ProbeID      string
	Version      string
	RegisteredAt *time.Time
	LastSeenAt   *time.Time
}

// hashProbeSecret derives the stored secret hash for probe authentication.
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
			INSERT INTO probes (probe_id, secret_hash, provisioned_by, version, created_at, registered_at, last_seen_at, revoked_at)
			VALUES ($1, $2, 'config', '', $3, NULL, NULL, NULL)
			ON CONFLICT (probe_id) DO UPDATE
			SET secret_hash = excluded.secret_hash,
			    provisioned_by = 'config',
			    revoked_at = NULL
		`, probe.ProbeID, hashProbeSecret(probe.Secret), now)
		if err != nil {
			return err
		}
	}

	rows, err := s.db.Query(`SELECT probe_id FROM probes WHERE provisioned_by = 'config'`)
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
		if _, err := s.db.Exec(`UPDATE probes SET revoked_at=$1 WHERE probe_id=$2`, time.Now().UTC(), probeID); err != nil {
			return err
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	return nil
}

// CreateProbeCredential provisions one API-managed probe credential. If
// requestedProbeID is blank, a unique probe ID is generated.
func (s *Store) CreateProbeCredential(requestedProbeID string) (ProbeCredential, error) {
	probeID := strings.TrimSpace(requestedProbeID)
	if probeID != "" {
		return s.insertProbeCredential(probeID)
	}

	for i := 0; i < 8; i++ {
		generated, err := randomProbeID()
		if err != nil {
			return ProbeCredential{}, err
		}
		credential, err := s.insertProbeCredential(generated)
		if errors.Is(err, ErrProbeAlreadyExists) {
			continue
		}
		return credential, err
	}

	return ProbeCredential{}, fmt.Errorf("%w: generated id collision", ErrProbeAlreadyExists)
}

func (s *Store) insertProbeCredential(probeID string) (ProbeCredential, error) {
	probeID = strings.TrimSpace(probeID)
	if !probeIDPattern.MatchString(probeID) {
		return ProbeCredential{}, fmt.Errorf("%w: %q", ErrInvalidProbeID, probeID)
	}

	secret, err := randomHexToken(32)
	if err != nil {
		return ProbeCredential{}, err
	}
	now := time.Now().UTC()

	var insertedProbeID string
	err = s.db.QueryRow(`
		INSERT INTO probes (probe_id, secret_hash, provisioned_by, version, created_at, registered_at, last_seen_at, revoked_at)
		VALUES ($1, $2, 'api', '', $3, NULL, NULL, NULL)
		ON CONFLICT (probe_id) DO NOTHING
		RETURNING probe_id
	`, probeID, hashProbeSecret(secret), now).Scan(&insertedProbeID)
	if err == sql.ErrNoRows {
		return ProbeCredential{}, ErrProbeAlreadyExists
	}
	if err != nil {
		return ProbeCredential{}, err
	}

	return ProbeCredential{ProbeID: insertedProbeID, Secret: secret}, nil
}

func randomProbeID() (string, error) {
	suffix, err := randomHexToken(6)
	if err != nil {
		return "", err
	}
	return "probe-" + suffix, nil
}

// AuthenticateProbe returns the active probe record for the given probe_id and
// secret. Returns nil if the credentials are invalid.
func (s *Store) AuthenticateProbe(probeID, secret string) (*Probe, error) {
	var (
		probe        Probe
		secretHash   string
		registeredAt sql.NullTime
		lastSeen     sql.NullTime
	)
	err := s.db.QueryRow(`
		SELECT probe_id, version, registered_at, last_seen_at, secret_hash
		FROM probes
		WHERE probe_id = $1 AND revoked_at IS NULL
	`, probeID).Scan(
		&probe.ProbeID,
		&probe.Version,
		&registeredAt,
		&lastSeen,
		&secretHash,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if subtle.ConstantTimeCompare([]byte(secretHash), []byte(hashProbeSecret(secret))) != 1 {
		return nil, nil
	}
	if registeredAt.Valid {
		t := registeredAt.Time
		probe.RegisteredAt = &t
	}
	if lastSeen.Valid {
		t := lastSeen.Time
		probe.LastSeenAt = &t
	}
	return &probe, nil
}

// RegisterProbe records a successful authenticated startup for a probe.
func (s *Store) RegisterProbe(probeID, version string) error {
	now := time.Now().UTC()
	_, err := s.db.Exec(`
		UPDATE probes
		SET version = $1,
		    registered_at = COALESCE(registered_at, $2),
		    last_seen_at = $2
		WHERE probe_id = $3 AND revoked_at IS NULL
	`, version, now, probeID)
	return err
}

// updateProbeHeartbeatTx refreshes a probe's persisted last-seen metadata
// inside an existing transaction.
func updateProbeHeartbeatTx(tx *sql.Tx, probeID string, at time.Time) (time.Time, error) {
	heartbeatAt := normalizeTime(at)
	_, err := tx.Exec(`
		UPDATE probes
		SET last_seen_at = $1
		WHERE probe_id = $2 AND revoked_at IS NULL
	`, heartbeatAt, probeID)
	if err != nil {
		return time.Time{}, err
	}
	return heartbeatAt, nil
}
