package main

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	probeapi "github.com/tmater/wacht/internal/api/probe"
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

	batcher.Enqueue(proto.CheckResult{CheckID: "00000000-0000-0000-0000-000000000101", CheckName: "check-1", Up: true})
	batcher.Enqueue(proto.CheckResult{CheckID: "00000000-0000-0000-0000-000000000102", CheckName: "check-2", Up: false})

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

	batcher.Enqueue(proto.CheckResult{CheckID: "00000000-0000-0000-0000-000000000101", CheckName: "check-1", Up: true})
	batcher.Enqueue(proto.CheckResult{CheckID: "00000000-0000-0000-0000-000000000102", CheckName: "check-2", Up: true})

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

	batcher.Enqueue(proto.CheckResult{CheckID: "00000000-0000-0000-0000-000000000101", CheckName: "check-1", Up: true})

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

	batcher.Enqueue(proto.CheckResult{CheckID: "00000000-0000-0000-0000-000000000101", CheckName: "check-1", Up: true})
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

func TestResultBatcherDropsBatchOnNonRetryableError(t *testing.T) {
	poster := &fakeResultPoster{callCh: make(chan []proto.CheckResult, 1)}
	poster.postFn = func(results []proto.CheckResult) error {
		return &probeapi.ResponseError{
			Method:     "POST",
			Path:       probeapi.PathResults,
			StatusCode: 401,
			Status:     "401 Unauthorized",
			Expected:   204,
		}
	}

	batcher := newResultBatcher(poster, 10*time.Millisecond, 8)
	defer batcher.Close()

	batcher.Enqueue(proto.CheckResult{CheckID: "00000000-0000-0000-0000-000000000101", CheckName: "check-1", Up: true})

	select {
	case <-poster.callCh:
	case <-time.After(250 * time.Millisecond):
		t.Fatal("timed out waiting for non-retryable flush")
	}

	time.Sleep(50 * time.Millisecond)
	if got := batcher.pendingCount(); got != 0 {
		t.Fatalf("pending count = %d, want 0", got)
	}
	if attempts := len(poster.calls); attempts != 1 {
		t.Fatalf("upload attempts = %d, want 1", attempts)
	}
}

func TestResultBatcherCapsPendingQueueByDroppingOldestResults(t *testing.T) {
	poster := &fakeResultPoster{}
	batcher := newResultBatcher(poster, time.Hour, 8)
	batcher.maxPending = 3
	defer batcher.Close()

	for _, item := range []struct {
		checkID   string
		checkName string
	}{
		{checkID: "00000000-0000-0000-0000-000000000001", checkName: "check-1"},
		{checkID: "00000000-0000-0000-0000-000000000002", checkName: "check-2"},
		{checkID: "00000000-0000-0000-0000-000000000003", checkName: "check-3"},
		{checkID: "00000000-0000-0000-0000-000000000004", checkName: "check-4"},
		{checkID: "00000000-0000-0000-0000-000000000005", checkName: "check-5"},
	} {
		batcher.Enqueue(proto.CheckResult{CheckID: item.checkID, CheckName: item.checkName, Up: true})
	}

	if got := batcher.pendingCount(); got != 3 {
		t.Fatalf("pending count = %d, want 3", got)
	}

	batch := batcher.takeBatch()
	if len(batch) != 3 {
		t.Fatalf("batch len = %d, want 3", len(batch))
	}
	if batch[0].CheckName != "check-3" || batch[1].CheckName != "check-4" || batch[2].CheckName != "check-5" {
		t.Fatalf("kept batch = %#v, want newest check-3/check-4/check-5", batch)
	}
}
