package alert

import (
	"log"
	"sync"
)

const (
	defaultWebhookWorkers   = 2
	defaultWebhookQueueSize = 128
)

type sendFunc func(url string, payload AlertPayload) error

type delivery struct {
	url     string
	payload AlertPayload
}

// Sender delivers webhooks using a bounded worker pool so result ingestion
// never blocks on outbound alert delivery.
type Sender struct {
	jobs chan delivery
	send sendFunc
	wg   sync.WaitGroup
	once sync.Once
}

// NewSender creates a webhook sender with a small bounded queue.
func NewSender() *Sender {
	return newSender(defaultWebhookWorkers, defaultWebhookQueueSize, Fire)
}

func newSender(workers, queueSize int, send sendFunc) *Sender {
	if workers <= 0 {
		workers = 1
	}
	if queueSize <= 0 {
		queueSize = 1
	}
	if send == nil {
		send = Fire
	}

	s := &Sender{
		jobs: make(chan delivery, queueSize),
		send: send,
	}

	for i := 0; i < workers; i++ {
		s.wg.Add(1)
		go s.worker()
	}

	return s
}

func (s *Sender) worker() {
	defer s.wg.Done()
	for job := range s.jobs {
		if err := s.send(job.url, job.payload); err != nil {
			log.Printf("alert: webhook failed check_id=%s: %s", job.payload.CheckID, err)
		} else {
			log.Printf("alert: webhook fired check_id=%s url=%s", job.payload.CheckID, job.url)
		}
	}
}

// Enqueue schedules an alert for background delivery. It returns false when the
// queue is full, so callers can fail open without blocking result ingestion.
func (s *Sender) Enqueue(url string, payload AlertPayload) bool {
	select {
	case s.jobs <- delivery{url: url, payload: payload}:
		return true
	default:
		return false
	}
}

// Close stops the worker pool after draining queued work.
func (s *Sender) Close() {
	if s == nil {
		return
	}
	s.once.Do(func() {
		close(s.jobs)
		s.wg.Wait()
	})
}
