package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

var (
	// ErrInvalidMonitoringJournalKind reports a blank journal kind.
	ErrInvalidMonitoringJournalKind = errors.New("store: invalid monitoring journal kind")
	// ErrInvalidMonitoringPayload reports malformed or empty snapshot JSON payloads.
	ErrInvalidMonitoringPayload = errors.New("store: invalid monitoring payload")
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

// MonitoringSnapshot is one captured recovery baseline for the monitoring
// runtime so boot only has to replay the journal tail after it.
type MonitoringSnapshot struct {
	ID            int64
	LastJournalID int64
	Payload       json.RawMessage
	CapturedAt    time.Time
}

// MonitoringWrite groups journal, snapshot, and incident writes into one
// commit boundary.
type MonitoringWrite struct {
	JournalRecords       []MonitoringJournalRecord
	Snapshot             *MonitoringSnapshot
	IncidentCheckID      string
	ResolveIncident      bool
	IncidentNotification *NotificationRequest
}

// AppendMonitoringJournal appends one runtime recovery record.
func (s *Store) AppendMonitoringJournal(record MonitoringJournalRecord) (MonitoringJournalRecord, error) {
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return MonitoringJournalRecord{}, err
	}
	defer tx.Rollback()

	saved, err := appendMonitoringJournalTx(tx, record)
	if err != nil {
		return MonitoringJournalRecord{}, err
	}
	if err := tx.Commit(); err != nil {
		return MonitoringJournalRecord{}, err
	}
	return saved, nil
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

// AppendMonitoringSnapshot appends one captured runtime image.
func (s *Store) AppendMonitoringSnapshot(snapshot MonitoringSnapshot) (MonitoringSnapshot, error) {
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return MonitoringSnapshot{}, err
	}
	defer tx.Rollback()

	saved, err := appendMonitoringSnapshotTx(tx, snapshot)
	if err != nil {
		return MonitoringSnapshot{}, err
	}
	if err := tx.Commit(); err != nil {
		return MonitoringSnapshot{}, err
	}
	return saved, nil
}

// LatestMonitoringSnapshot returns the newest captured runtime image, or nil
// when no snapshot has been persisted yet.
func (s *Store) LatestMonitoringSnapshot() (*MonitoringSnapshot, error) {
	var snapshot MonitoringSnapshot
	err := s.db.QueryRow(`
		SELECT id, last_journal_id, payload, captured_at
		FROM monitoring_snapshots
		ORDER BY id DESC
		LIMIT 1
	`).Scan(&snapshot.ID, &snapshot.LastJournalID, &snapshot.Payload, &snapshot.CapturedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &snapshot, nil
}

// PersistMonitoringWrite commits journal, snapshot, and incident writes in one
// transaction so runtime recovery data and durable side effects do not drift.
// It returns the persisted write with generated IDs filled in, plus whether the
// incident side effect actually changed durable state.
func (s *Store) PersistMonitoringWrite(write MonitoringWrite) (MonitoringWrite, bool, error) {
	if write.IncidentCheckID == "" && (write.ResolveIncident || write.IncidentNotification != nil) {
		return MonitoringWrite{}, false, ErrInvalidMonitoringIncidentWrite
	}

	if len(write.JournalRecords) == 0 && write.Snapshot == nil && write.IncidentCheckID == "" {
		return MonitoringWrite{}, false, nil
	}

	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return MonitoringWrite{}, false, err
	}
	defer tx.Rollback()

	persisted := write
	persisted.JournalRecords = make([]MonitoringJournalRecord, 0, len(write.JournalRecords))

	for _, record := range write.JournalRecords {
		saved, err := appendMonitoringJournalTx(tx, record)
		if err != nil {
			return MonitoringWrite{}, false, err
		}
		persisted.JournalRecords = append(persisted.JournalRecords, saved)
	}

	if write.Snapshot != nil {
		snapshot := *write.Snapshot
		if len(persisted.JournalRecords) > 0 {
			snapshot.LastJournalID = persisted.JournalRecords[len(persisted.JournalRecords)-1].ID
		}

		saved, err := appendMonitoringSnapshotTx(tx, snapshot)
		if err != nil {
			return MonitoringWrite{}, false, err
		}
		persisted.Snapshot = &saved
	}

	incidentApplied, err := applyMonitoringIncidentTx(
		tx,
		write.IncidentCheckID,
		write.ResolveIncident,
		write.IncidentNotification,
	)
	if err != nil {
		return MonitoringWrite{}, false, err
	}

	if err := tx.Commit(); err != nil {
		return MonitoringWrite{}, false, err
	}
	return persisted, incidentApplied, nil
}

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

func appendMonitoringSnapshotTx(tx *sql.Tx, snapshot MonitoringSnapshot) (MonitoringSnapshot, error) {
	payload, err := normalizeMonitoringPayload(snapshot.Payload)
	if err != nil {
		return MonitoringSnapshot{}, err
	}
	snapshot.Payload = payload
	snapshot.CapturedAt = normalizeTime(snapshot.CapturedAt)
	if snapshot.LastJournalID < 0 {
		snapshot.LastJournalID = 0
	}

	err = tx.QueryRow(`
		INSERT INTO monitoring_snapshots (last_journal_id, payload, captured_at)
		VALUES ($1, $2::jsonb, $3)
		RETURNING id
	`, snapshot.LastJournalID, string(snapshot.Payload), snapshot.CapturedAt).Scan(&snapshot.ID)
	if err != nil {
		return MonitoringSnapshot{}, err
	}
	return snapshot, nil
}

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

func normalizeMonitoringPayload(payload json.RawMessage) (json.RawMessage, error) {
	payload = json.RawMessage(strings.TrimSpace(string(payload)))
	if !json.Valid(payload) {
		return nil, ErrInvalidMonitoringPayload
	}
	return payload, nil
}

func normalizeTime(t time.Time) time.Time {
	if t.IsZero() {
		return time.Now().UTC()
	}
	return t.UTC()
}

func normalizeOptionalTime(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}

	normalized := t.UTC()
	return &normalized
}
