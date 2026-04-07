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
	update, err := quorum.ObserveDown("probe-b", at, &expiresAt, "timeout")
	if err != nil {
		t.Fatalf("ObserveDown probe-b: %v", err)
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
