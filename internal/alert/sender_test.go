package alert

import (
	"errors"
	"sync"
	"testing"
)

func TestSender_EnqueueDeliversInBackground(t *testing.T) {
	var (
		mu        sync.Mutex
		delivered []AlertPayload
	)

	sender := newSender(1, 4, func(url string, payload AlertPayload) error {
		mu.Lock()
		delivered = append(delivered, payload)
		mu.Unlock()
		return nil
	})
	defer sender.Close()

	ok := sender.Enqueue("https://hooks.example.com/a", AlertPayload{CheckID: "check-1"})
	if !ok {
		t.Fatal("expected enqueue to succeed")
	}

	sender.Close()

	mu.Lock()
	defer mu.Unlock()
	if len(delivered) != 1 {
		t.Fatalf("expected 1 delivered payload, got %d", len(delivered))
	}
	if delivered[0].CheckID != "check-1" {
		t.Fatalf("expected check-1, got %s", delivered[0].CheckID)
	}
}

func TestSender_EnqueueDropsWhenQueueFull(t *testing.T) {
	block := make(chan struct{})
	sender := newSender(1, 1, func(url string, payload AlertPayload) error {
		<-block
		return nil
	})
	defer func() {
		close(block)
		sender.Close()
	}()

	if !sender.Enqueue("https://hooks.example.com/a", AlertPayload{CheckID: "check-1"}) {
		t.Fatal("expected first enqueue to succeed")
	}
	if !sender.Enqueue("https://hooks.example.com/b", AlertPayload{CheckID: "check-2"}) {
		t.Fatal("expected second enqueue to fill queue")
	}
	if sender.Enqueue("https://hooks.example.com/c", AlertPayload{CheckID: "check-3"}) {
		t.Fatal("expected third enqueue to fail when queue is full")
	}
}

func TestSender_CloseDrainsQueuedWork(t *testing.T) {
	var count int
	sender := newSender(1, 2, func(url string, payload AlertPayload) error {
		count++
		return nil
	})

	if !sender.Enqueue("https://hooks.example.com/a", AlertPayload{CheckID: "check-1"}) {
		t.Fatal("expected enqueue to succeed")
	}
	if !sender.Enqueue("https://hooks.example.com/b", AlertPayload{CheckID: "check-2"}) {
		t.Fatal("expected enqueue to succeed")
	}

	sender.Close()

	if count != 2 {
		t.Fatalf("expected 2 deliveries after Close, got %d", count)
	}
}

func TestSender_SendErrorsDoNotPanic(t *testing.T) {
	sender := newSender(1, 1, func(url string, payload AlertPayload) error {
		return errors.New("boom")
	})

	if !sender.Enqueue("https://hooks.example.com/a", AlertPayload{CheckID: "check-1"}) {
		t.Fatal("expected enqueue to succeed")
	}

	sender.Close()
}
