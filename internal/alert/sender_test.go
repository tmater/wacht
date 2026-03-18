package alert

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/tmater/wacht/internal/network"
	"github.com/tmater/wacht/internal/store"
)

type fakeNotificationStore struct {
	mu          sync.Mutex
	jobs        []store.NotificationJob
	deliveredID int64
	retriedID   int64
	retryAt     time.Time
	retryErr    string
}

func (f *fakeNotificationStore) ClaimDueIncidentNotifications(now, staleBefore time.Time, limit int) ([]store.NotificationJob, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if len(f.jobs) == 0 {
		return nil, nil
	}
	jobs := append([]store.NotificationJob(nil), f.jobs...)
	f.jobs = nil
	return jobs, nil
}

func (f *fakeNotificationStore) MarkIncidentNotificationDelivered(id int64, deliveredAt time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deliveredID = id
	return nil
}

func (f *fakeNotificationStore) MarkIncidentNotificationRetry(id int64, attemptedAt, nextAttemptAt time.Time, lastError string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.retriedID = id
	f.retryAt = nextAttemptAt
	f.retryErr = lastError
	return nil
}

func TestSenderRunBatchRecordsDeliveryOutcome(t *testing.T) {
	tests := []struct {
		name            string
		job             store.NotificationJob
		sendErr         error
		wantDeliveredID int64
		wantRetriedID   int64
		wantRetryErr    string
	}{
		{
			name:            "delivered",
			job:             store.NotificationJob{ID: 7, CheckID: "check-1", Event: "down", WebhookURL: "https://hooks.example.com/a", Payload: []byte(`{"status":"down"}`), Attempts: 1},
			wantDeliveredID: 7,
		},
		{
			name:          "retry on failure",
			job:           store.NotificationJob{ID: 9, CheckID: "check-2", Event: "up", WebhookURL: "https://hooks.example.com/b", Payload: []byte(`{"status":"up"}`), Attempts: 3},
			sendErr:       errors.New("boom"),
			wantRetriedID: 9,
			wantRetryErr:  "boom",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := &fakeNotificationStore{jobs: []store.NotificationJob{tt.job}}

			var sentPayload []byte
			sender := newSender(st, network.Policy{}, 0, 1, time.Hour, time.Hour, func(url string, payload []byte) error {
				sentPayload = append([]byte(nil), payload...)
				return tt.sendErr
			})
			defer sender.Close()

			startedAt := time.Now()
			if !sender.runBatch() {
				t.Fatal("expected runBatch to process claimed job")
			}
			if string(sentPayload) != string(tt.job.Payload) {
				t.Fatalf("payload = %s, want %s", sentPayload, tt.job.Payload)
			}
			if st.deliveredID != tt.wantDeliveredID {
				t.Fatalf("deliveredID = %d, want %d", st.deliveredID, tt.wantDeliveredID)
			}
			if st.retriedID != tt.wantRetriedID {
				t.Fatalf("retriedID = %d, want %d", st.retriedID, tt.wantRetriedID)
			}
			if tt.wantRetryErr == "" {
				if !st.retryAt.IsZero() {
					t.Fatalf("retryAt = %s, want zero time", st.retryAt)
				}
				return
			}
			if st.retryErr != tt.wantRetryErr {
				t.Fatalf("retryErr = %q, want %q", st.retryErr, tt.wantRetryErr)
			}
			if !st.retryAt.After(startedAt) {
				t.Fatalf("retryAt = %s, want retry time after %s", st.retryAt, startedAt)
			}
		})
	}
}

func TestNextRetryDelayBackoffCaps(t *testing.T) {
	if got := nextRetryDelayWithBackoff(1); got != time.Second {
		t.Fatalf("attempt 1 delay = %s, want 1s", got)
	}
	if got := nextRetryDelayWithBackoff(4); got != 8*time.Second {
		t.Fatalf("attempt 4 delay = %s, want 8s", got)
	}
	if got := nextRetryDelayWithBackoff(30); got != maxWebhookRetryDelay {
		t.Fatalf("attempt 30 delay = %s, want %s", got, maxWebhookRetryDelay)
	}
}
