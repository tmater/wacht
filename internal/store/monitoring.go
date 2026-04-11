package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

var (
	// ErrInvalidMonitoringJournalKind reports a blank journal kind.
	ErrInvalidMonitoringJournalKind = errors.New("store: invalid monitoring journal kind")
	// ErrInvalidMonitoringProbeWrite reports an incomplete probe heartbeat write.
	ErrInvalidMonitoringProbeWrite = errors.New("store: invalid monitoring probe write")
	// ErrInvalidMonitoringIncidentWrite reports an incomplete incident write.
	ErrInvalidMonitoringIncidentWrite = errors.New("store: invalid monitoring incident write")
)

// MonitoringJournalRecord is one append-only replay entry for rebuilding
// monitoring runtime transitions after restart. Its typed fields are the
// durable journal contract for probe and check events.
type MonitoringJournalRecord struct {
	ID         int64
	Kind       string
	CheckID    string
	ProbeID    string
	Message    string
	ExpiresAt  *time.Time
	OccurredAt time.Time
	RecordedAt time.Time
}

// MonitoringWrite groups journal, probe heartbeat, and incident writes into one
// commit boundary.
type MonitoringWrite struct {
	JournalRecords       []MonitoringJournalRecord
	ProbeHeartbeatID     string
	ProbeHeartbeatAt     time.Time
	IncidentCheckID      string
	ResolveIncident      bool
	IncidentNotification *NotificationRequest
}

// MonitoringJournalAfter returns the append-only recovery tail with IDs
// strictly greater than afterID.
func (s *Store) MonitoringJournalAfter(afterID int64) ([]MonitoringJournalRecord, error) {
	if afterID < 0 {
		afterID = 0
	}

	rows, err := s.db.Query(`
		SELECT id, kind, check_id, probe_id, message, expires_at, occurred_at, recorded_at
		FROM monitoring_journal
		WHERE id > $1
		ORDER BY id ASC
	`, afterID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []MonitoringJournalRecord
	for rows.Next() {
		var (
			record    MonitoringJournalRecord
			checkID   sql.NullString
			probeID   sql.NullString
			message   sql.NullString
			expiresAt sql.NullTime
		)
		if err := rows.Scan(
			&record.ID,
			&record.Kind,
			&checkID,
			&probeID,
			&message,
			&expiresAt,
			&record.OccurredAt,
			&record.RecordedAt,
		); err != nil {
			return nil, err
		}
		if checkID.Valid {
			record.CheckID = checkID.String
		}
		if probeID.Valid {
			record.ProbeID = probeID.String
		}
		if message.Valid {
			record.Message = message.String
		}
		if expiresAt.Valid {
			expiry := expiresAt.Time
			record.ExpiresAt = &expiry
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

// PersistMonitoringWrite commits journal, probe heartbeat, and incident writes
// in one transaction so runtime recovery data and durable side effects do not
// drift. It returns the persisted write with generated IDs filled in.
func (s *Store) PersistMonitoringWrite(write MonitoringWrite) (MonitoringWrite, error) {
	if write.ProbeHeartbeatID == "" && !write.ProbeHeartbeatAt.IsZero() {
		return MonitoringWrite{}, ErrInvalidMonitoringProbeWrite
	}
	if write.IncidentCheckID == "" && (write.ResolveIncident || write.IncidentNotification != nil) {
		return MonitoringWrite{}, ErrInvalidMonitoringIncidentWrite
	}

	if len(write.JournalRecords) == 0 && write.ProbeHeartbeatID == "" && write.IncidentCheckID == "" {
		return MonitoringWrite{}, nil
	}

	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return MonitoringWrite{}, err
	}
	defer tx.Rollback()

	persisted := write
	persisted.JournalRecords = make([]MonitoringJournalRecord, 0, len(write.JournalRecords))

	for _, record := range write.JournalRecords {
		saved, err := appendMonitoringJournalTx(tx, record)
		if err != nil {
			return MonitoringWrite{}, err
		}
		persisted.JournalRecords = append(persisted.JournalRecords, saved)
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

	if err := tx.Commit(); err != nil {
		return MonitoringWrite{}, err
	}
	return persisted, nil
}

// appendMonitoringJournalTx normalizes and appends one recovery journal record
// inside an existing transaction.
func appendMonitoringJournalTx(tx *sql.Tx, record MonitoringJournalRecord) (MonitoringJournalRecord, error) {
	record.Kind = strings.TrimSpace(record.Kind)
	if record.Kind == "" {
		return MonitoringJournalRecord{}, ErrInvalidMonitoringJournalKind
	}
	record.CheckID = strings.TrimSpace(record.CheckID)
	record.ProbeID = strings.TrimSpace(record.ProbeID)
	record.Message = strings.TrimSpace(record.Message)
	record.ExpiresAt = normalizeOptionalTime(record.ExpiresAt)

	record.RecordedAt = normalizeTime(record.RecordedAt)
	if record.OccurredAt.IsZero() {
		record.OccurredAt = record.RecordedAt
	} else {
		record.OccurredAt = normalizeTime(record.OccurredAt)
	}

	err := tx.QueryRow(`
		INSERT INTO monitoring_journal (
			kind, check_id, probe_id, message, expires_at, occurred_at, recorded_at
		)
		VALUES ($1, NULLIF($2, ''), NULLIF($3, ''), $4, $5, $6, $7)
		RETURNING id
	`, record.Kind, record.CheckID, record.ProbeID, record.Message, record.ExpiresAt, record.OccurredAt, record.RecordedAt).Scan(&record.ID)
	if err != nil {
		return MonitoringJournalRecord{}, err
	}
	return record, nil
}

// applyMonitoringIncidentTx applies the optional incident side effect for a
// monitoring write inside an existing transaction.
func applyMonitoringIncidentTx(tx *sql.Tx, checkID string, resolve bool, request *NotificationRequest) (bool, error) {
	if checkID == "" {
		return false, nil
	}

	if resolve {
		return resolveIncidentWithNotificationTx(tx, checkID, request, time.Now().UTC())
	}

	alreadyOpen, err := openIncidentWithNotificationTx(tx, checkID, request, time.Now().UTC())
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

// normalizeOptionalTime applies normalizeTime to optional timestamps.
func normalizeOptionalTime(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}

	normalized := t.UTC()
	return &normalized
}
