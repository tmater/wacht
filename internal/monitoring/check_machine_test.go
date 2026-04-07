package monitoring

import (
	"testing"
	"time"
)

func TestCheckMachineTransitionsAndMetadata(t *testing.T) {
	at := time.Date(2026, 4, 7, 10, 0, 0, 0, time.UTC)
	expiresAt := at.Add(30 * time.Second)
	check := NewCheckMachine("check-a", "probe-a")

	transition, err := check.ObserveDown(at, &expiresAt, "timeout")
	if err != nil {
		t.Fatalf("ObserveDown: %v", err)
	}
	if transition.From != CheckStateMissing || transition.To != CheckStateDown {
		t.Fatalf("first down transition = %+v, want missing -> down", transition)
	}

	state := check.Snapshot()
	if state.StreakLen != 1 {
		t.Fatalf("streak = %d, want 1", state.StreakLen)
	}
	if state.LastOutcomeUp == nil || *state.LastOutcomeUp {
		t.Fatal("expected last outcome to be down")
	}
	if state.LastError != "timeout" {
		t.Fatalf("last error = %q, want %q", state.LastError, "timeout")
	}
	if state.ExpiresAt == nil || !state.ExpiresAt.Equal(expiresAt) {
		t.Fatalf("expiresAt = %v, want %v", state.ExpiresAt, expiresAt)
	}

	transition, err = check.ObserveDown(at.Add(time.Second), &expiresAt, "timeout")
	if err != nil {
		t.Fatalf("ObserveDown reentry: %v", err)
	}
	if !transition.Reentry {
		t.Fatal("expected repeated down observation to be reentry")
	}
	if got := check.Snapshot().StreakLen; got != 2 {
		t.Fatalf("streak after repeated down = %d, want 2", got)
	}

	transition, err = check.MarkError("probe payload invalid")
	if err != nil {
		t.Fatalf("MarkError: %v", err)
	}
	if transition.To != CheckStateError {
		t.Fatalf("error transition to = %q, want %q", transition.To, CheckStateError)
	}

	transition, err = check.LoseEvidence()
	if err != nil {
		t.Fatalf("LoseEvidence: %v", err)
	}
	if transition.To != CheckStateMissing {
		t.Fatalf("lose evidence transition to = %q, want %q", transition.To, CheckStateMissing)
	}
	if got := check.Snapshot().ExpiresAt; got != nil {
		t.Fatalf("expiresAt after losing evidence = %v, want nil", got)
	}

	transition, err = check.ObserveUp(at.Add(2*time.Second), &expiresAt)
	if err != nil {
		t.Fatalf("ObserveUp: %v", err)
	}
	if transition.From != CheckStateMissing || transition.To != CheckStateUp {
		t.Fatalf("up transition = %+v, want missing -> up", transition)
	}
	if got := check.Snapshot().StreakLen; got != 1 {
		t.Fatalf("streak after direction change = %d, want 1", got)
	}
}

func TestCheckMachineSupportsExplicitReentryCases(t *testing.T) {
	check := NewCheckMachine("check-a", "probe-a")
	if _, err := check.LoseEvidence(); err != nil {
		t.Fatalf("LoseEvidence from missing should be reentry, got %v", err)
	}
	if _, err := check.MarkError("bad payload"); err != nil {
		t.Fatalf("MarkError: %v", err)
	}
	if _, err := check.MarkError("still bad"); err != nil {
		t.Fatalf("MarkError reentry: %v", err)
	}
}
