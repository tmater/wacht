package store

import (
	"database/sql"
	"time"
)

const (
	notificationEventDown = "down"
	notificationEventUp   = "up"

	notificationStatePending    = "pending"
	notificationStateProcessing = "processing"
	notificationStateRetrying   = "retrying"
	notificationStateDelivered  = "delivered"
	notificationStateSuperseded = "superseded"
)

// NotificationRequest captures the durable work needed to deliver a webhook.
type NotificationRequest struct {
	WebhookURL string
	Payload    []byte
}

// IncidentNotification summarizes the delivery state for one incident transition.
type IncidentNotification struct {
	ID            int64
	State         string
	Attempts      int
	LastError     string
	LastAttemptAt *time.Time
	NextAttemptAt *time.Time
	DeliveredAt   *time.Time
}

// NotificationJob is a claimed webhook delivery ready for dispatch.
type NotificationJob struct {
	ID         int64
	IncidentID int64
	CheckID    string
	Event      string
	WebhookURL string
	Payload    []byte
	Attempts   int
}

func insertIncidentNotification(tx *sql.Tx, incidentID int64, event string, request *NotificationRequest, now time.Time) error {
	if request == nil || request.WebhookURL == "" || len(request.Payload) == 0 {
		return nil
	}

	_, err := tx.Exec(`
		INSERT INTO incident_notifications (
			incident_id, event, state, webhook_url, payload, attempts, next_attempt_at, created_at, updated_at
		)
		VALUES ($1, $2, $3, $4, $5::jsonb, 0, $6, $6, $6)
		ON CONFLICT (incident_id, event) DO NOTHING
	`, incidentID, event, notificationStatePending, request.WebhookURL, string(request.Payload), now)
	return err
}

// ClaimDueIncidentNotifications claims notifications that are ready for delivery.
// Stale processing rows are recovered after staleBefore so crashes do not strand work.
func (s *Store) ClaimDueIncidentNotifications(now, staleBefore time.Time, limit int) ([]NotificationJob, error) {
	if limit <= 0 {
		limit = 1
	}

	rows, err := s.db.Query(`
		WITH due AS (
			SELECT n.id
			FROM incident_notifications n
			JOIN incidents i ON i.id = n.incident_id
			JOIN checks c ON c.id = i.check_id
			WHERE (
				(n.state IN ($1, $2) AND n.next_attempt_at <= $3)
				OR (n.state = $4 AND n.last_attempt_at <= $5)
			)
			AND NOT (n.event = $6 AND i.resolved_at IS NOT NULL)
			ORDER BY n.next_attempt_at ASC, n.id ASC
			LIMIT $7
			FOR UPDATE SKIP LOCKED
		)
		UPDATE incident_notifications n
		SET state = $4,
		    attempts = n.attempts + 1,
		    last_attempt_at = $3,
		    updated_at = $3
		FROM due, incidents i, checks c
		WHERE n.id = due.id
		  AND i.id = n.incident_id
		  AND c.id = i.check_id
		RETURNING n.id, n.incident_id, c.id::text, n.event, n.webhook_url, n.payload, n.attempts
	`, notificationStatePending, notificationStateRetrying, now, notificationStateProcessing, staleBefore, notificationEventDown, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	jobs := make([]NotificationJob, 0, limit)
	for rows.Next() {
		var job NotificationJob
		if err := rows.Scan(&job.ID, &job.IncidentID, &job.CheckID, &job.Event, &job.WebhookURL, &job.Payload, &job.Attempts); err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	return jobs, rows.Err()
}

// MarkIncidentNotificationDelivered records a successful delivery while
// preserving rows that were already superseded during an in-flight send.
func (s *Store) MarkIncidentNotificationDelivered(id int64, deliveredAt time.Time) error {
	_, err := s.db.Exec(`
		UPDATE incident_notifications
		SET state = $2,
		    delivered_at = $1,
		    next_attempt_at = NULL,
		    last_error = NULL,
		    updated_at = $1
		WHERE id = $3
		  AND state = $4
	`, deliveredAt, notificationStateDelivered, id, notificationStateProcessing)
	return err
}

// MarkIncidentNotificationRetry records a failed delivery attempt or supersedes
// stale "down" notifications once the incident has already recovered.
func (s *Store) MarkIncidentNotificationRetry(id int64, attemptedAt, nextAttemptAt time.Time, lastError string) error {
	res, err := s.db.Exec(`
		UPDATE incident_notifications n
		SET state = CASE
				WHEN n.state = $2 THEN $2
				WHEN n.event = $3 AND i.resolved_at IS NOT NULL THEN $2
				ELSE $4
			END,
		    next_attempt_at = CASE
				WHEN n.state = $2 THEN NULL
				WHEN n.event = $3 AND i.resolved_at IS NOT NULL THEN NULL
				ELSE $5::timestamptz
			END,
		    last_error = $6,
		    updated_at = $1
		FROM incidents i
		WHERE n.id = $7
		  AND i.id = n.incident_id
		  AND n.state <> $8
	`, attemptedAt, notificationStateSuperseded, notificationEventDown, notificationStateRetrying, nextAttemptAt, truncateError(lastError), id, notificationStateDelivered)
	if err != nil {
		return err
	}

	rows, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return nil
	}
	return nil
}

func truncateError(message string) string {
	if len(message) <= 512 {
		return message
	}
	return message[:512]
}

func scanIncidentNotification(
	id sql.NullInt64,
	state sql.NullString,
	attempts sql.NullInt32,
	lastError sql.NullString,
	lastAttemptAt sql.NullTime,
	nextAttemptAt sql.NullTime,
	deliveredAt sql.NullTime,
) *IncidentNotification {
	if !id.Valid {
		return nil
	}

	summary := &IncidentNotification{
		ID:        id.Int64,
		State:     state.String,
		LastError: lastError.String,
	}
	if attempts.Valid {
		summary.Attempts = int(attempts.Int32)
	}
	if lastAttemptAt.Valid {
		t := lastAttemptAt.Time
		summary.LastAttemptAt = &t
	}
	if nextAttemptAt.Valid {
		t := nextAttemptAt.Time
		summary.NextAttemptAt = &t
	}
	if deliveredAt.Valid {
		t := deliveredAt.Time
		summary.DeliveredAt = &t
	}
	return summary
}

func openIncidentWithNotificationByCheckIDTx(tx *sql.Tx, checkID string, request *NotificationRequest, now time.Time) (bool, error) {
	var incidentID int64
	err := tx.QueryRow(`
		INSERT INTO incidents (check_id, user_id, started_at)
		SELECT id, user_id, $2
		FROM checks
		WHERE id = $1
		  AND deleted_at IS NULL
		ON CONFLICT (check_id) WHERE resolved_at IS NULL DO NOTHING
		RETURNING id
	`, checkID, now).Scan(&incidentID)
	if err == sql.ErrNoRows {
		return true, nil
	}
	if err != nil {
		return false, err
	}

	if err := insertIncidentNotification(tx, incidentID, notificationEventDown, request, now); err != nil {
		return false, err
	}
	return false, nil
}

func resolveIncidentWithNotificationByCheckIDTx(tx *sql.Tx, checkID string, request *NotificationRequest, now time.Time) (bool, error) {
	var incidentID int64
	err := tx.QueryRow(`
		UPDATE incidents
		SET resolved_at = $1
		WHERE check_id = $2
		  AND resolved_at IS NULL
		RETURNING id
	`, now, checkID).Scan(&incidentID)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	if _, err := tx.Exec(`
		UPDATE incident_notifications
		SET state = $1,
		    next_attempt_at = NULL,
		    updated_at = $2
		WHERE incident_id = $3
		  AND event = $4
		  AND state <> $5
	`, notificationStateSuperseded, now, incidentID, notificationEventDown, notificationStateDelivered); err != nil {
		return false, err
	}

	if err := insertIncidentNotification(tx, incidentID, notificationEventUp, request, now); err != nil {
		return false, err
	}
	return true, nil
}
