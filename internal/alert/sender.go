package alert

import (
	"log/slog"
	"sync"
	"time"

	"github.com/tmater/wacht/internal/logx"
	"github.com/tmater/wacht/internal/network"
	"github.com/tmater/wacht/internal/store"
)

const (
	defaultWebhookWorkers      = 2
	defaultWebhookClaimBatch   = 1
	defaultWebhookPollInterval = time.Second
	defaultWebhookStaleAfter   = 20 * time.Second
	maxWebhookRetryDelay       = 5 * time.Minute
)

type notificationStore interface {
	ClaimDueIncidentNotifications(now, staleBefore time.Time, limit int) ([]store.NotificationJob, error)
	MarkIncidentNotificationDelivered(id int64, deliveredAt time.Time) error
	MarkIncidentNotificationRetry(id int64, attemptedAt, nextAttemptAt time.Time, lastError string) error
}

type sendFunc func(url string, payload []byte) error

// Sender delivers webhooks from durable DB-backed jobs so result ingestion
// never has to choose between blocking and dropping alerts.
type Sender struct {
	store        notificationStore
	send         sendFunc
	pollInterval time.Duration
	staleAfter   time.Duration
	claimBatch   int
	stop         chan struct{}
	wg           sync.WaitGroup
	once         sync.Once
}

// NewSender creates a durable webhook sender backed by the store.
func NewSender(st notificationStore, policy network.Policy) *Sender {
	return newSender(st, policy, defaultWebhookWorkers, defaultWebhookClaimBatch, defaultWebhookPollInterval, defaultWebhookStaleAfter, nil)
}

func newSender(st notificationStore, policy network.Policy, workers, claimBatch int, pollInterval, staleAfter time.Duration, send sendFunc) *Sender {
	if workers < 0 {
		workers = 1
	}
	if claimBatch <= 0 {
		claimBatch = 1
	}
	if pollInterval <= 0 {
		pollInterval = time.Second
	}
	if staleAfter <= 0 {
		staleAfter = 4 * webhookTimeout
	}
	if send == nil {
		client := policy.NewHTTPClient(webhookTimeout, 3*time.Second, false)
		send = func(url string, payload []byte) error {
			return Fire(client, url, payload)
		}
	}

	s := &Sender{
		store:        st,
		send:         send,
		pollInterval: pollInterval,
		staleAfter:   staleAfter,
		claimBatch:   claimBatch,
		stop:         make(chan struct{}),
	}
	if st == nil {
		return s
	}

	for i := 0; i < workers; i++ {
		s.wg.Add(1)
		go s.worker()
	}

	return s
}

func (s *Sender) worker() {
	defer s.wg.Done()
	for {
		if s.runBatch() {
			continue
		}

		select {
		case <-s.stop:
			return
		case <-time.After(s.pollInterval):
		}
	}
}

func (s *Sender) runBatch() bool {
	if s == nil || s.store == nil {
		return false
	}

	now := time.Now().UTC()
	jobs, err := s.store.ClaimDueIncidentNotifications(now, now.Add(-s.staleAfter), s.claimBatch)
	if err != nil {
		slog.Default().Error("claim webhook jobs failed", "component", "alert", "err", err)
		return false
	}
	if len(jobs) == 0 {
		return false
	}

	for _, job := range jobs {
		s.dispatch(job)
	}
	return true
}

func (s *Sender) dispatch(job store.NotificationJob) {
	if err := s.send(job.WebhookURL, job.Payload); err != nil {
		attemptedAt := time.Now().UTC()
		nextAttemptAt := attemptedAt.Add(nextRetryDelayWithBackoff(job.Attempts))
		if markErr := s.store.MarkIncidentNotificationRetry(job.ID, attemptedAt, nextAttemptAt, err.Error()); markErr != nil {
			slog.Default().Error("record webhook retry failed", "component", "alert", "check_id", job.CheckID, "event", job.Event, "job_id", job.ID, "webhook_host", logx.URLHost(job.WebhookURL), "err", markErr)
			return
		}
		slog.Default().Warn("webhook delivery failed", "component", "alert", "check_id", job.CheckID, "event", job.Event, "job_id", job.ID, "attempt", job.Attempts, "webhook_host", logx.URLHost(job.WebhookURL), "next_attempt_at", nextAttemptAt, "err", err)
		return
	}

	deliveredAt := time.Now().UTC()
	if err := s.store.MarkIncidentNotificationDelivered(job.ID, deliveredAt); err != nil {
		slog.Default().Error("record webhook delivery failed", "component", "alert", "check_id", job.CheckID, "event", job.Event, "job_id", job.ID, "webhook_host", logx.URLHost(job.WebhookURL), "err", err)
		return
	}
	slog.Default().Info("webhook delivered", "component", "alert", "check_id", job.CheckID, "event", job.Event, "job_id", job.ID, "attempt", job.Attempts, "webhook_host", logx.URLHost(job.WebhookURL))
}

func nextRetryDelayWithBackoff(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	delay := time.Second << min(attempt-1, 10)
	if delay > maxWebhookRetryDelay {
		return maxWebhookRetryDelay
	}
	return delay
}

// Close stops background workers. Pending work remains durable in the database.
func (s *Sender) Close() {
	if s == nil {
		return
	}
	s.once.Do(func() {
		close(s.stop)
		s.wg.Wait()
	})
}
