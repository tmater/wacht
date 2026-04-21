package main

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/tmater/wacht/internal/proto"
)

type fakeResultPoster struct {
	mu     sync.Mutex
	calls  [][]proto.CheckResult
	postFn func([]proto.CheckResult) error
	callCh chan []proto.CheckResult
}

func (f *fakeResultPoster) PostResults(_ context.Context, results []proto.CheckResult) error {
	batch := append([]proto.CheckResult(nil), results...)
	f.mu.Lock()
	f.calls = append(f.calls, batch)
	f.mu.Unlock()

	if f.callCh != nil {
		select {
		case f.callCh <- batch:
		default:
		}
	}
	if f.postFn != nil {
		return f.postFn(batch)
	}
	return nil
}

func TestResultBatcherFlushesOnInterval(t *testing.T) {
	poster := &fakeResultPoster{callCh: make(chan []proto.CheckResult, 1)}
	batcher := newResultBatcher(poster, 10*time.Millisecond, 8)
	defer batcher.Close()

	batcher.Enqueue(proto.CheckResult{CheckID: "check-1", Up: true})
	batcher.Enqueue(proto.CheckResult{CheckID: "check-2", Up: false})

	select {
	case batch := <-poster.callCh:
		if len(batch) != 2 {
			t.Fatalf("batch len = %d, want 2", len(batch))
		}
	case <-time.After(250 * time.Millisecond):
		t.Fatal("timed out waiting for interval flush")
	}
}

func TestResultBatcherFlushesWhenBatchIsFull(t *testing.T) {
	poster := &fakeResultPoster{callCh: make(chan []proto.CheckResult, 1)}
	batcher := newResultBatcher(poster, time.Hour, 2)
	defer batcher.Close()

	batcher.Enqueue(proto.CheckResult{CheckID: "check-1", Up: true})
	batcher.Enqueue(proto.CheckResult{CheckID: "check-2", Up: true})

	select {
	case batch := <-poster.callCh:
		if len(batch) != 2 {
			t.Fatalf("batch len = %d, want 2", len(batch))
		}
	case <-time.After(250 * time.Millisecond):
		t.Fatal("timed out waiting for size-based flush")
	}
}

func TestResultBatcherRetriesFailedBatchOnNextFlush(t *testing.T) {
	poster := &fakeResultPoster{callCh: make(chan []proto.CheckResult, 2)}
	attempts := 0
	poster.postFn = func(results []proto.CheckResult) error {
		attempts++
		if attempts == 1 {
			return errors.New("temporary failure")
		}
		return nil
	}

	batcher := newResultBatcher(poster, 10*time.Millisecond, 8)
	defer batcher.Close()

	batcher.Enqueue(proto.CheckResult{CheckID: "check-1", Up: true})

	timeout := time.After(500 * time.Millisecond)
	for attempts < 2 {
		select {
		case <-poster.callCh:
		case <-timeout:
			t.Fatalf("attempts = %d, want 2", attempts)
		}
	}
}

func TestResultBatcherCloseFlushesPendingResults(t *testing.T) {
	poster := &fakeResultPoster{callCh: make(chan []proto.CheckResult, 1)}
	batcher := newResultBatcher(poster, time.Hour, 8)

	batcher.Enqueue(proto.CheckResult{CheckID: "check-1", Up: true})
	batcher.Close()

	select {
	case batch := <-poster.callCh:
		if len(batch) != 1 {
			t.Fatalf("batch len = %d, want 1", len(batch))
		}
	case <-time.After(250 * time.Millisecond):
		t.Fatal("timed out waiting for shutdown flush")
	}
}
