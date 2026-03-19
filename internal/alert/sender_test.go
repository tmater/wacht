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
	claim       func() []store.NotificationJob
	deliveredID int64
	retriedID   int64
	retryAt     time.Time
	retryErr    string
}

func (f *fakeNotificationStore) ClaimDueIncidentNotifications(now, staleBefore time.Time, limit int) ([]store.NotificationJob, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.claim != nil {
		return f.claim(), nil
	}
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

func testJob(id int64, checkID, event, status string, attempts int) store.NotificationJob {
	return store.NotificationJob{
		ID:         id,
		IncidentID: id,
		CheckID:    checkID,
		Event:      event,
		WebhookURL: "https://hooks.example.com/a",
		Payload:    []byte(`{"status":"` + status + `"}`),
		Attempts:   attempts,
	}
}

func TestSenderRunBatchRecordsDeliveryOutcome(t *testing.T) {
	tests := []struct {
		name            string
		jobs            []store.NotificationJob
		sendErr         error
		stopAfterFirst  bool
		wantResult      batchResult
		wantSent        int
		wantDeliveredID int64
		wantRetriedID   int64
		wantRetryErr    string
	}{
		{
			name:            "delivered",
			jobs:            []store.NotificationJob{testJob(7, "check-1", "down", "down", 1)},
			wantResult:      batchProcessed,
			wantSent:        1,
			wantDeliveredID: 7,
		},
		{
			name:          "retry on failure",
			jobs:          []store.NotificationJob{testJob(9, "check-2", "up", "up", 3)},
			sendErr:       errors.New("boom"),
			wantResult:    batchProcessed,
			wantSent:      1,
			wantRetriedID: 9,
			wantRetryErr:  "boom",
		},
		{
			name:            "stop after first send",
			jobs:            []store.NotificationJob{testJob(7, "check-1", "down", "down", 1), testJob(8, "check-1", "up", "up", 1)},
			stopAfterFirst:  true,
			wantResult:      batchStopped,
			wantSent:        1,
			wantDeliveredID: 7,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := &fakeNotificationStore{jobs: append([]store.NotificationJob(nil), tt.jobs...)}

			var (
				sender       *Sender
				sentPayloads [][]byte
			)
			sender = newSender(st, network.Policy{}, 0, len(tt.jobs), time.Hour, time.Hour, func(url string, payload []byte) error {
				sentPayloads = append(sentPayloads, append([]byte(nil), payload...))
				if tt.stopAfterFirst && len(sentPayloads) == 1 {
					sender.once.Do(func() {
						close(sender.stop)
					})
				}
				return tt.sendErr
			})
			defer sender.Close()

			startedAt := time.Now()
			if got := sender.runBatch(); got != tt.wantResult {
				t.Fatalf("runBatch = %v, want %v", got, tt.wantResult)
			}
			if len(sentPayloads) != tt.wantSent {
				t.Fatalf("sent %d payloads, want %d", len(sentPayloads), tt.wantSent)
			}
			if tt.wantSent > 0 && string(sentPayloads[0]) != string(tt.jobs[0].Payload) {
				t.Fatalf("first payload = %s, want %s", sentPayloads[0], tt.jobs[0].Payload)
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

func TestSenderCloseReturnsUnderSustainedLoad(t *testing.T) {
	started := make(chan struct{})
	var (
		nextID int64
		once   sync.Once
	)
	st := &fakeNotificationStore{
		claim: func() []store.NotificationJob {
			nextID++
			return []store.NotificationJob{testJob(nextID, "check-1", "down", "down", 1)}
		},
	}

	sender := newSender(st, network.Policy{}, 1, 1, time.Hour, time.Hour, func(url string, payload []byte) error {
		once.Do(func() { close(started) })
		return nil
	})

	<-started

	done := make(chan struct{})
	go func() {
		sender.Close()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(250 * time.Millisecond):
		t.Fatal("Close blocked under sustained load")
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
