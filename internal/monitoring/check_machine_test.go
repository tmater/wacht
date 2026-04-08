package monitoring

import (
	"testing"
	"time"
)

func TestCheckMachineTransitionsAndMetadata(t *testing.T) {
	at := time.Date(2026, 4, 7, 10, 0, 0, 0, time.UTC)
	check := NewCheckMachine("check-a", "probe-a")

	expiresAt := at.Add(30 * time.Second)
	transition, err := check.ObserveDown(at, &expiresAt, "timeout")
	if err != nil {
		t.Fatalf("ObserveDown: %v", err)
	}
	if transition.From != CheckStateMissing || transition.To != CheckStateMissing {
		t.Fatalf("first down transition = %+v, want missing -> missing", transition)
	}

	state := check.Snapshot()
	if state.StreakLen != 1 {
		t.Fatalf("streak = %d, want 1", state.StreakLen)
	}
	if state.LastOutcome != CheckStateDown {
		t.Fatalf("last outcome = %q, want %q", state.LastOutcome, CheckStateDown)
	}
	if state.LastError != "timeout" {
		t.Fatalf("last error = %q, want %q", state.LastError, "timeout")
	}
	if state.ExpiresAt.IsZero() || !state.ExpiresAt.Equal(expiresAt) {
		t.Fatalf("expiresAt = %v, want %v", state.ExpiresAt, expiresAt)
	}

	secondExpiry := at.Add(time.Second + 30*time.Second)
	transition, err = check.ObserveDown(at.Add(time.Second), &secondExpiry, "timeout")
	if err != nil {
		t.Fatalf("ObserveDown reentry: %v", err)
	}
	if transition.From != CheckStateMissing || transition.To != CheckStateDown {
		t.Fatalf("second down transition = %+v, want missing -> down", transition)
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
	if got := check.Snapshot().ExpiresAt; !got.IsZero() {
		t.Fatalf("expiresAt after losing evidence = %v, want nil", got)
	}
	if state := check.Snapshot(); state.LastOutcome != "" {
		t.Fatalf("last outcome after losing evidence = %q, want empty", state.LastOutcome)
	}
	if got := check.Snapshot().StreakLen; got != 0 {
		t.Fatalf("streak after losing evidence = %d, want 0", got)
	}

	thirdExpiry := at.Add(2*time.Second + 30*time.Second)
	transition, err = check.ObserveUp(at.Add(2*time.Second), &thirdExpiry)
	if err != nil {
		t.Fatalf("ObserveUp: %v", err)
	}
	if transition.From != CheckStateMissing || transition.To != CheckStateMissing {
		t.Fatalf("first up transition = %+v, want missing -> missing", transition)
	}
	if got := check.Snapshot().StreakLen; got != 1 {
		t.Fatalf("streak after direction change = %d, want 1", got)
	}

	fourthExpiry := at.Add(3*time.Second + 30*time.Second)
	transition, err = check.ObserveUp(at.Add(3*time.Second), &fourthExpiry)
	if err != nil {
		t.Fatalf("ObserveUp second: %v", err)
	}
	if transition.From != CheckStateMissing || transition.To != CheckStateUp {
		t.Fatalf("second up transition = %+v, want missing -> up", transition)
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

func TestCheckMachineSnapshotReturnsDetachedCopy(t *testing.T) {
	at := time.Date(2026, 4, 7, 10, 0, 0, 0, time.UTC)
	expiresAt := at.Add(30 * time.Second)
	check := NewCheckMachine("check-a", "probe-a")

	if _, err := check.ObserveDown(at, &expiresAt, "timeout"); err != nil {
		t.Fatalf("ObserveDown: %v", err)
	}

	snapshot := check.Snapshot()
	snapshot.LastOutcome = CheckStateUp
	snapshot.LastResultAt = at.Add(time.Hour)
	snapshot.ExpiresAt = at.Add(2 * time.Hour)

	state := check.Snapshot()
	if state.LastOutcome != CheckStateDown {
		t.Fatal("machine last outcome was mutated through snapshot")
	}
	if state.LastResultAt.IsZero() || !state.LastResultAt.Equal(at) {
		t.Fatalf("machine last result at = %v, want %v", state.LastResultAt, at)
	}
	if state.ExpiresAt.IsZero() || !state.ExpiresAt.Equal(expiresAt) {
		t.Fatalf("machine expiresAt = %v, want %v", state.ExpiresAt, expiresAt)
	}
}
