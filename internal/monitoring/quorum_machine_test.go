package monitoring

import (
	"testing"
	"time"
)

func TestQuorumMachineRecompute(t *testing.T) {
	quorum := NewQuorumMachine("check-a", []string{"probe-a", "probe-b", "probe-c"})
	at := time.Date(2026, 4, 7, 10, 0, 0, 0, time.UTC)
	expiresAt := at.Add(30 * time.Second)

	if _, err := quorum.ObserveDown("probe-a", at, &expiresAt, "timeout"); err != nil {
		t.Fatalf("ObserveDown probe-a: %v", err)
	}
	if _, err := quorum.ObserveDown("probe-b", at, &expiresAt, "timeout"); err != nil {
		t.Fatalf("ObserveDown probe-b first: %v", err)
	}

	secondAt := at.Add(time.Second)
	secondExpiry := secondAt.Add(30 * time.Second)
	if _, err := quorum.ObserveDown("probe-a", secondAt, &secondExpiry, "timeout"); err != nil {
		t.Fatalf("ObserveDown probe-a second: %v", err)
	}
	update, err := quorum.ObserveDown("probe-b", secondAt, &secondExpiry, "timeout")
	if err != nil {
		t.Fatalf("ObserveDown probe-b second: %v", err)
	}
	update, err = quorum.ObserveDown("probe-a", secondAt.Add(time.Second), ptrTime(secondExpiry.Add(time.Second)), "timeout")
	if err != nil {
		t.Fatalf("ObserveDown probe-a third: %v", err)
	}

	transition := update.QuorumTransition
	if transition.To != QuorumStateDown {
		t.Fatalf("quorum transition to = %q, want %q", transition.To, QuorumStateDown)
	}
	if transition.LastStableState != QuorumStateDown {
		t.Fatalf("last stable state = %q, want %q", transition.LastStableState, QuorumStateDown)
	}

	update, err = quorum.LoseEvidence("probe-b")
	if err != nil {
		t.Fatalf("LoseEvidence: %v", err)
	}

	transition = update.QuorumTransition
	if transition.To != QuorumStateError {
		t.Fatalf("quorum transition to = %q, want %q", transition.To, QuorumStateError)
	}
	if quorum.Snapshot().LastStableState != QuorumStateDown {
		t.Fatalf("stored last stable state = %q, want %q", quorum.Snapshot().LastStableState, QuorumStateDown)
	}
}

func TestQuorumMachineDoesNotOpenIncidentForStaggeredFlap(t *testing.T) {
	quorum := NewQuorumMachine("check-a", []string{"probe-a", "probe-b", "probe-c"})
	at := time.Date(2026, 4, 7, 10, 0, 0, 0, time.UTC)

	steps := []struct {
		probeID string
		up      bool
	}{
		{probeID: "probe-c", up: false},
		{probeID: "probe-b", up: true},
		{probeID: "probe-c", up: false},
		{probeID: "probe-a", up: true},
		{probeID: "probe-a", up: false},
		{probeID: "probe-b", up: true},
		{probeID: "probe-a", up: false},
	}

	for i, step := range steps {
		observedAt := at.Add(time.Duration(i) * time.Second)
		expiresAt := observedAt.Add(30 * time.Second)
		var err error
		if step.up {
			_, err = quorum.ObserveUp(step.probeID, observedAt, &expiresAt)
		} else {
			_, err = quorum.ObserveDown(step.probeID, observedAt, &expiresAt, "timeout")
		}
		if err != nil {
			t.Fatalf("step %d (%+v) error = %v", i, step, err)
		}
	}

	if quorum.Snapshot().IncidentOpen {
		t.Fatal("IncidentOpen = true, want false")
	}
	if quorum.Snapshot().LastStableState == QuorumStateDown {
		t.Fatalf("last stable state = %q, want not down", quorum.Snapshot().LastStableState)
	}
}
