package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

var (
	// ErrInvalidMonitoringProbeWrite reports an incomplete probe heartbeat write.
	ErrInvalidMonitoringProbeWrite = errors.New("store: invalid monitoring probe write")
	// ErrInvalidMonitoringIncidentWrite reports an incomplete incident write.
	ErrInvalidMonitoringIncidentWrite = errors.New("store: invalid monitoring incident write")
	// ErrInvalidMonitoringCheckStateWrite reports an incomplete per-(check, probe)
	// current-state write.
	ErrInvalidMonitoringCheckStateWrite = errors.New("store: invalid monitoring check state write")
)

// CheckStateWrite is one bounded persisted current-state row for a
// (check, probe) assignment.
type CheckStateWrite struct {
	CheckID      string
	ProbeID      string
	LastResultAt time.Time
	LastOutcome  string
	StreakLen    int
	ExpiresAt    time.Time
	State        string
	LastError    string
}

// PersistedProbeState is the compact persisted probe liveness snapshot needed
// for runtime recovery.
type PersistedProbeState struct {
	ProbeID    string
	LastSeenAt *time.Time
}

// PersistedCheckState is the compact persisted per-(check, probe) snapshot
// needed for runtime recovery.
type PersistedCheckState struct {
	CheckID      string
	ProbeID      string
	LastResultAt time.Time
	LastOutcome  string
	StreakLen    int
	ExpiresAt    time.Time
	State        string
	LastError    string
}

// MonitoringWrite groups current-state, probe heartbeat, and incident writes
// into one commit boundary.
type MonitoringWrite struct {
	CheckStateWrites     []CheckStateWrite
	ProbeHeartbeatID     string
	ProbeHeartbeatAt     time.Time
	IncidentCheckID      string
	ResolveIncident      bool
	IncidentNotification *NotificationRequest
}

// ActiveProbeStates returns all active probes plus their last-seen timestamps
// for runtime recovery.
func (s *Store) ActiveProbeStates() ([]PersistedProbeState, error) {
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

	var probes []PersistedProbeState
	for rows.Next() {
		var (
			state      PersistedProbeState
			lastSeenAt sql.NullTime
		)
		if err := rows.Scan(&state.ProbeID, &lastSeenAt); err != nil {
			return nil, err
		}
		if lastSeenAt.Valid {
			t := lastSeenAt.Time
			state.LastSeenAt = &t
		}
		probes = append(probes, state)
	}
	return probes, rows.Err()
}

// PersistedCheckStates returns all compact per-(check, probe) snapshots needed
// to rebuild runtime state after restart.
func (s *Store) PersistedCheckStates() ([]PersistedCheckState, error) {
	rows, err := s.db.Query(`
		SELECT check_id::text, probe_id, last_result_at, last_outcome, streak_len, expires_at, state, last_error
		FROM check_probe_state
		ORDER BY check_id, probe_id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var states []PersistedCheckState
	for rows.Next() {
		var (
			state     PersistedCheckState
			streakLen int32
		)
		if err := rows.Scan(
			&state.CheckID,
			&state.ProbeID,
			&state.LastResultAt,
			&state.LastOutcome,
			&streakLen,
			&state.ExpiresAt,
			&state.State,
			&state.LastError,
		); err != nil {
			return nil, err
		}
		state.StreakLen = int(streakLen)
		states = append(states, state)
	}
	return states, rows.Err()
}

// OpenIncidentCheckIDs returns active check IDs with unresolved
// incidents so runtime recovery can restore incident-open semantics without
// replaying a log.
func (s *Store) OpenIncidentCheckIDs() ([]string, error) {
	rows, err := s.db.Query(`
		SELECT i.check_id::text
		FROM incidents i
		JOIN checks c ON c.id = i.check_id
		WHERE i.resolved_at IS NULL
		  AND c.deleted_at IS NULL
		ORDER BY i.check_id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var checkIDs []string
	for rows.Next() {
		var checkID string
		if err := rows.Scan(&checkID); err != nil {
			return nil, err
		}
		checkIDs = append(checkIDs, checkID)
	}
	return checkIDs, rows.Err()
}

// PersistMonitoringWrite commits current-state, probe heartbeat, and incident
// writes in one transaction so runtime recovery data and durable side effects
// do not drift.
func (s *Store) PersistMonitoringWrite(write MonitoringWrite) (MonitoringWrite, error) {
	writes, err := s.PersistMonitoringBatch([]MonitoringWrite{write})
	if err != nil {
		return MonitoringWrite{}, err
	}
	if len(writes) == 0 {
		return MonitoringWrite{}, nil
	}
	return writes[0], nil
}

// PersistMonitoringBatch commits multiple current-state / incident write units
// in one transaction, preserving input order while reducing commit overhead for
// batched probe result ingestion.
func (s *Store) PersistMonitoringBatch(writes []MonitoringWrite) ([]MonitoringWrite, error) {
	for _, write := range writes {
		if write.ProbeHeartbeatID == "" && !write.ProbeHeartbeatAt.IsZero() {
			return nil, ErrInvalidMonitoringProbeWrite
		}
		if write.IncidentCheckID == "" && (write.ResolveIncident || write.IncidentNotification != nil) {
			return nil, ErrInvalidMonitoringIncidentWrite
		}
		for _, state := range write.CheckStateWrites {
			if _, err := normalizeCheckStateWrite(state); err != nil {
				return nil, err
			}
		}
	}

	nonEmpty := false
	for _, write := range writes {
		if len(write.CheckStateWrites) > 0 || write.ProbeHeartbeatID != "" || write.IncidentCheckID != "" {
			nonEmpty = true
			break
		}
	}
	if !nonEmpty {
		return nil, nil
	}

	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	persisted := make([]MonitoringWrite, 0, len(writes))
	for _, write := range writes {
		saved, err := persistMonitoringWriteTx(tx, write)
		if err != nil {
			return nil, err
		}
		persisted = append(persisted, saved)
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return persisted, nil
}

func persistMonitoringWriteTx(tx *sql.Tx, write MonitoringWrite) (MonitoringWrite, error) {
	if write.ProbeHeartbeatID == "" && !write.ProbeHeartbeatAt.IsZero() {
		return MonitoringWrite{}, ErrInvalidMonitoringProbeWrite
	}
	if write.IncidentCheckID == "" && (write.ResolveIncident || write.IncidentNotification != nil) {
		return MonitoringWrite{}, ErrInvalidMonitoringIncidentWrite
	}
	for _, state := range write.CheckStateWrites {
		if _, err := normalizeCheckStateWrite(state); err != nil {
			return MonitoringWrite{}, err
		}
	}

	if len(write.CheckStateWrites) == 0 && write.ProbeHeartbeatID == "" && write.IncidentCheckID == "" {
		return MonitoringWrite{}, nil
	}

	persisted := write
	persisted.CheckStateWrites = make([]CheckStateWrite, 0, len(write.CheckStateWrites))

	for _, state := range write.CheckStateWrites {
		saved, err := upsertCheckStateTx(tx, state)
		if err != nil {
			return MonitoringWrite{}, err
		}
		persisted.CheckStateWrites = append(persisted.CheckStateWrites, saved)
	}

	if write.ProbeHeartbeatID != "" {
		heartbeatAt, err := updateProbeHeartbeatTx(tx, write.ProbeHeartbeatID, write.ProbeHeartbeatAt)
		if err != nil {
			return MonitoringWrite{}, err
		}
		persisted.ProbeHeartbeatAt = heartbeatAt
	}

	if _, err := applyMonitoringIncidentTx(
		tx,
		write.IncidentCheckID,
		write.ResolveIncident,
		write.IncidentNotification,
	); err != nil {
		return MonitoringWrite{}, err
	}
	return persisted, nil
}

func upsertCheckStateTx(tx *sql.Tx, state CheckStateWrite) (CheckStateWrite, error) {
	state, err := normalizeCheckStateWrite(state)
	if err != nil {
		return CheckStateWrite{}, err
	}
	checkID, err := normalizeCheckID(state.CheckID)
	if err != nil {
		return CheckStateWrite{}, ErrInvalidMonitoringCheckStateWrite
	}
	state.CheckID = checkID

	_, err = tx.Exec(`
		INSERT INTO check_probe_state (
			check_id, probe_id, last_result_at, last_outcome, streak_len, expires_at, state, last_error
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (check_id, probe_id) DO UPDATE
		SET last_result_at = excluded.last_result_at,
		    last_outcome = excluded.last_outcome,
		    streak_len = excluded.streak_len,
		    expires_at = excluded.expires_at,
		    state = excluded.state,
		    last_error = excluded.last_error
	`, checkID, state.ProbeID, state.LastResultAt, state.LastOutcome, state.StreakLen, state.ExpiresAt, state.State, state.LastError)
	if err != nil {
		return CheckStateWrite{}, err
	}
	return state, nil
}

func normalizeCheckStateWrite(state CheckStateWrite) (CheckStateWrite, error) {
	state.CheckID = strings.TrimSpace(state.CheckID)
	state.ProbeID = strings.TrimSpace(state.ProbeID)
	state.LastOutcome = strings.TrimSpace(state.LastOutcome)
	state.State = strings.TrimSpace(state.State)
	state.LastError = strings.TrimSpace(state.LastError)
	if state.CheckID == "" || state.ProbeID == "" || state.State == "" || state.StreakLen < 0 {
		return CheckStateWrite{}, ErrInvalidMonitoringCheckStateWrite
	}
	if state.LastOutcome == "" && state.State != "missing" {
		return CheckStateWrite{}, ErrInvalidMonitoringCheckStateWrite
	}

	state.LastResultAt = normalizeTime(state.LastResultAt)
	if state.ExpiresAt.IsZero() {
		state.ExpiresAt = state.LastResultAt
	} else {
		state.ExpiresAt = state.ExpiresAt.UTC()
	}
	return state, nil
}

// applyMonitoringIncidentTx applies the optional incident side effect for a
// monitoring write inside an existing transaction.
func applyMonitoringIncidentTx(tx *sql.Tx, checkID string, resolve bool, request *NotificationRequest) (bool, error) {
	if checkID == "" {
		return false, nil
	}
	checkID, err := normalizeCheckID(checkID)
	if err != nil {
		return false, ErrInvalidMonitoringIncidentWrite
	}

	if resolve {
		return resolveIncidentWithNotificationByCheckIDTx(tx, checkID, request, time.Now().UTC())
	}

	alreadyOpen, err := openIncidentWithNotificationByCheckIDTx(tx, checkID, request, time.Now().UTC())
	if err != nil {
		return false, err
	}
	return !alreadyOpen, nil
}

// normalizeTime coerces zero or local times into a UTC timestamp suitable for
// durable monitoring records.
func normalizeTime(t time.Time) time.Time {
	if t.IsZero() {
		return time.Now().UTC()
	}
	return t.UTC()
}
