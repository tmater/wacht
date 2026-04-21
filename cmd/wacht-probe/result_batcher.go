package main

import (
	"context"
	"log/slog"
	"sync"
	"time"

	probeapi "github.com/tmater/wacht/internal/api/probe"
	"github.com/tmater/wacht/internal/config"
	"github.com/tmater/wacht/internal/proto"
)

const defaultResultBatchMaxSize = 64

type resultPoster interface {
	PostResults(ctx context.Context, results []proto.CheckResult) error
}

// resultBatcher accumulates probe results and flushes them to the server on a
// cadence or when the pending batch grows large enough.
type resultBatcher struct {
	client        resultPoster
	flushInterval time.Duration
	maxBatchSize  int

	mu      sync.Mutex
	pending []proto.CheckResult
	closed  bool

	wakeFlush chan struct{}
	stop      chan struct{}
	done      chan struct{}
}

func newResultBatcher(client resultPoster, flushInterval time.Duration, maxBatchSize int) *resultBatcher {
	if flushInterval <= 0 {
		flushInterval = config.DefaultProbeResultFlushInterval
	}
	if maxBatchSize <= 0 {
		maxBatchSize = defaultResultBatchMaxSize
	}

	b := &resultBatcher{
		client:        client,
		flushInterval: flushInterval,
		maxBatchSize:  maxBatchSize,
		wakeFlush:     make(chan struct{}, 1),
		stop:          make(chan struct{}),
		done:          make(chan struct{}),
	}
	go b.loop()
	return b
}

func (b *resultBatcher) Enqueue(result proto.CheckResult) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return
	}

	b.pending = append(b.pending, result)
	if len(b.pending) >= b.maxBatchSize {
		b.signalFlush()
	}
}

func (b *resultBatcher) Close() {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		<-b.done
		return
	}
	b.closed = true
	close(b.stop)
	b.mu.Unlock()

	<-b.done
}

func (b *resultBatcher) loop() {
	ticker := time.NewTicker(b.flushInterval)
	defer ticker.Stop()
	defer close(b.done)

	for {
		select {
		case <-ticker.C:
			b.flushOne()
		case <-b.wakeFlush:
			b.flushOne()
		case <-b.stop:
			b.flushPendingForShutdown()
			return
		}
	}
}

func (b *resultBatcher) flushPendingForShutdown() {
	for {
		hadBatch, flushed := b.flushOne()
		if !hadBatch || !flushed {
			break
		}
	}
	if remaining := b.pendingCount(); remaining > 0 {
		slog.Default().Warn("dropping buffered results during shutdown", "component", "probe", "count", remaining)
	}
}

func (b *resultBatcher) flushOne() (hadBatch bool, flushed bool) {
	batch := b.takeBatch()
	if len(batch) == 0 {
		return false, true
	}

	ctx, cancel := context.WithTimeout(context.Background(), probeapi.DefaultRequestTimeout)
	err := b.client.PostResults(ctx, batch)
	cancel()
	if err != nil {
		b.requeue(batch)
		slog.Default().Warn("result batch upload failed", "component", "probe", "count", len(batch), "err", err)
		return true, false
	}

	if b.pendingCount() >= b.maxBatchSize {
		b.mu.Lock()
		b.signalFlush()
		b.mu.Unlock()
	}
	return true, true
}

func (b *resultBatcher) takeBatch() []proto.CheckResult {
	b.mu.Lock()
	defer b.mu.Unlock()

	if len(b.pending) == 0 {
		return nil
	}

	size := len(b.pending)
	if size > b.maxBatchSize {
		size = b.maxBatchSize
	}

	batch := append([]proto.CheckResult(nil), b.pending[:size]...)
	b.pending = b.pending[size:]
	return batch
}

func (b *resultBatcher) requeue(batch []proto.CheckResult) {
	b.mu.Lock()
	defer b.mu.Unlock()

	merged := make([]proto.CheckResult, 0, len(batch)+len(b.pending))
	merged = append(merged, batch...)
	merged = append(merged, b.pending...)
	b.pending = merged
}

func (b *resultBatcher) pendingCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.pending)
}

func (b *resultBatcher) signalFlush() {
	select {
	case b.wakeFlush <- struct{}{}:
	default:
	}
}
