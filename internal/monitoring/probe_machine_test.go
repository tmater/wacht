package monitoring

import (
	"testing"
	"time"
)

func TestProbeMachineTransitions(t *testing.T) {
	at := time.Date(2026, 4, 7, 10, 0, 0, 0, time.UTC)
	probe := NewProbeMachine("probe-a")

	transition, err := probe.ReceiveHeartbeat(at)
	if err != nil {
		t.Fatalf("ReceiveHeartbeat: %v", err)
	}
	if transition.From != ProbeStateOffline || transition.To != ProbeStateOnline {
		t.Fatalf("first heartbeat transition = %+v, want offline -> online", transition)
	}

	transition, err = probe.ReceiveHeartbeat(at.Add(time.Second))
	if err != nil {
		t.Fatalf("ReceiveHeartbeat reentry: %v", err)
	}
	if !transition.Reentry {
		t.Fatal("expected repeated heartbeat on online probe to be reentry")
	}

	transition, err = probe.MarkError("dial failed")
	if err != nil {
		t.Fatalf("MarkError: %v", err)
	}
	if transition.To != ProbeStateError {
		t.Fatalf("error transition to = %q, want %q", transition.To, ProbeStateError)
	}
	if got := probe.Snapshot().LastError; got != "dial failed" {
		t.Fatalf("last error = %q, want %q", got, "dial failed")
	}

	transition, err = probe.ReceiveHeartbeat(at.Add(2 * time.Second))
	if err != nil {
		t.Fatalf("ReceiveHeartbeat recovery: %v", err)
	}
	if transition.From != ProbeStateError || transition.To != ProbeStateOnline {
		t.Fatalf("recovery transition = %+v, want error -> online", transition)
	}
	if got := probe.Snapshot().LastError; got != "" {
		t.Fatalf("last error = %q, want cleared", got)
	}

	transition, err = probe.ExpireHeartbeat()
	if err != nil {
		t.Fatalf("ExpireHeartbeat: %v", err)
	}
	if transition.To != ProbeStateOffline {
		t.Fatalf("expiry transition to = %q, want %q", transition.To, ProbeStateOffline)
	}
}
