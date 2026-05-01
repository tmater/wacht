package main

import (
	"reflect"
	"testing"

	"github.com/tmater/wacht/internal/proto"
)

func TestSchedulerReconcile_LeavesUnchangedChecksRunning(t *testing.T) {
	s, hooks := newTestScheduler()
	check := proto.ProbeCheck{ID: "00000000-0000-0000-0000-000000000101", Name: "a", Type: "http", Target: "http://example.com", Interval: 1}

	s.Reconcile([]proto.ProbeCheck{check})
	if !reflect.DeepEqual(hooks.started, []string{"00000000-0000-0000-0000-000000000101"}) {
		t.Fatalf("started = %v, want [00000000-0000-0000-0000-000000000101]", hooks.started)
	}
	if len(hooks.cancelled) != 0 {
		t.Fatalf("cancelled = %v, want none", hooks.cancelled)
	}

	hooks.started = nil
	hooks.cancelled = nil
	s.Reconcile([]proto.ProbeCheck{check})

	if len(hooks.started) != 0 {
		t.Fatalf("started = %v, want none for unchanged check", hooks.started)
	}
	if len(hooks.cancelled) != 0 {
		t.Fatalf("cancelled = %v, want none for unchanged check", hooks.cancelled)
	}
}

func TestSchedulerReconcile_ReplacesChangedAndRemovedChecks(t *testing.T) {
	s, hooks := newTestScheduler()
	initialA := proto.ProbeCheck{ID: "00000000-0000-0000-0000-000000000101", Name: "a", Type: "http", Target: "http://example.com", Interval: 1}
	initialB := proto.ProbeCheck{ID: "00000000-0000-0000-0000-000000000102", Name: "b", Type: "tcp", Target: "example.com:443", Interval: 5}
	s.Reconcile([]proto.ProbeCheck{initialA, initialB})

	hooks.started = nil
	hooks.cancelled = nil
	updatedA := initialA
	updatedA.Interval = 10
	newC := proto.ProbeCheck{ID: "00000000-0000-0000-0000-000000000103", Name: "c", Type: "dns", Target: "example.com", Interval: 15}
	s.Reconcile([]proto.ProbeCheck{updatedA, newC})

	if got, want := toSet(hooks.started), map[string]int{
		"00000000-0000-0000-0000-000000000101": 1,
		"00000000-0000-0000-0000-000000000103": 1,
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("started = %v, want %v", got, want)
	}
	if got, want := toSet(hooks.cancelled), map[string]int{
		"00000000-0000-0000-0000-000000000101": 1,
		"00000000-0000-0000-0000-000000000102": 1,
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("cancelled = %v, want %v", got, want)
	}
	if got := s.running["00000000-0000-0000-0000-000000000101"].check; got != updatedA {
		t.Fatalf("running[101] = %#v, want %#v", got, updatedA)
	}
	if _, ok := s.running["00000000-0000-0000-0000-000000000102"]; ok {
		t.Fatal("running[102] still present after removal")
	}
	if got := s.running["00000000-0000-0000-0000-000000000103"].check; got != newC {
		t.Fatalf("running[103] = %#v, want %#v", got, newC)
	}
}

type testHooks struct {
	started   []string
	cancelled []string
}

func newTestScheduler() (*scheduler, *testHooks) {
	s := &scheduler{
		running: make(map[string]runningCheck),
	}
	hooks := &testHooks{}
	s.startWorker = func(check proto.ProbeCheck) runningCheck {
		hooks.started = append(hooks.started, check.ID)
		return runningCheck{
			check: check,
			cancel: func() {
				hooks.cancelled = append(hooks.cancelled, check.ID)
			},
		}
	}
	return s, hooks
}

func toSet(values []string) map[string]int {
	set := make(map[string]int, len(values))
	for _, value := range values {
		set[value]++
	}
	return set
}
