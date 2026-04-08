package monitoring

import (
	"testing"
	"time"
)

func TestNewRuntimeInitializesMachineRegistries(t *testing.T) {
	runtime := NewRuntime(
		[]string{"check-a", "check-b", "check-a", ""},
		[]string{"probe-a", "probe-b", "probe-a", ""},
	)

	if got, want := len(runtime.probes), 2; got != want {
		t.Fatalf("len(probes) = %d, want %d", got, want)
	}
	if got, want := len(runtime.quorums), 2; got != want {
		t.Fatalf("len(quorums) = %d, want %d", got, want)
	}
	if got, want := len(runtime.quorums["check-a"].checks), 2; got != want {
		t.Fatalf("len(quorum children) = %d, want %d", got, want)
	}

	probe, err := runtime.ProbeSnapshot("probe-a")
	if err != nil {
		t.Fatalf("ProbeSnapshot: %v", err)
	}
	if probe.State != ProbeStateOffline {
		t.Fatalf("probe state = %q, want %q", probe.State, ProbeStateOffline)
	}

	check, err := runtime.CheckSnapshot("check-a", "probe-a")
	if err != nil {
		t.Fatalf("CheckSnapshot: %v", err)
	}
	if check.State != CheckStateMissing {
		t.Fatalf("check state = %q, want %q", check.State, CheckStateMissing)
	}

	quorum, err := runtime.QuorumSnapshot("check-a")
	if err != nil {
		t.Fatalf("QuorumSnapshot: %v", err)
	}
	if quorum.State != QuorumStatePending {
		t.Fatalf("quorum state = %q, want %q", quorum.State, QuorumStatePending)
	}
}

func TestRuntimeRoutesHeartbeatToProbeMachine(t *testing.T) {
	runtime := NewRuntime([]string{"check-a"}, []string{"probe-a"})
	at := time.Date(2026, 4, 7, 10, 0, 0, 0, time.UTC)

	transition, err := runtime.ReceiveHeartbeat("probe-a", at)
	if err != nil {
		t.Fatalf("ReceiveHeartbeat: %v", err)
	}
	if transition.From != ProbeStateOffline || transition.To != ProbeStateOnline {
		t.Fatalf("heartbeat transition = %+v, want offline -> online", transition)
	}

	probe, err := runtime.ProbeSnapshot("probe-a")
	if err != nil {
		t.Fatalf("ProbeSnapshot: %v", err)
	}
	if probe.LastHeartbeatAt == nil || !probe.LastHeartbeatAt.Equal(at) {
		t.Fatalf("last heartbeat = %v, want %v", probe.LastHeartbeatAt, at)
	}
}

func TestRuntimeObserveCheckDownRecomputesQuorum(t *testing.T) {
	runtime := NewRuntime([]string{"check-a"}, []string{"probe-a", "probe-b", "probe-c"})
	at := time.Date(2026, 4, 7, 10, 0, 0, 0, time.UTC)
	expiresAt := at.Add(30 * time.Second)

	if _, err := runtime.ObserveCheckDown("check-a", "probe-a", at, &expiresAt, "timeout"); err != nil {
		t.Fatalf("ObserveCheckDown probe-a: %v", err)
	}
	if _, err := runtime.ObserveCheckDown("check-a", "probe-b", at, &expiresAt, "timeout"); err != nil {
		t.Fatalf("ObserveCheckDown probe-b: %v", err)
	}

	secondAt := at.Add(time.Second)
	secondExpiry := secondAt.Add(30 * time.Second)
	if _, err := runtime.ObserveCheckDown("check-a", "probe-a", secondAt, &secondExpiry, "timeout"); err != nil {
		t.Fatalf("ObserveCheckDown probe-a second: %v", err)
	}
	update, err := runtime.ObserveCheckDown("check-a", "probe-b", secondAt, &secondExpiry, "timeout")
	if err != nil {
		t.Fatalf("ObserveCheckDown probe-b second: %v", err)
	}

	if update.CheckTransition.To != CheckStateDown {
		t.Fatalf("check transition to = %q, want %q", update.CheckTransition.To, CheckStateDown)
	}
	if update.QuorumTransition.To != QuorumStateDown {
		t.Fatalf("quorum transition to = %q, want %q", update.QuorumTransition.To, QuorumStateDown)
	}
	if update.Quorum.LastStableState != QuorumStateDown {
		t.Fatalf("last stable state = %q, want %q", update.Quorum.LastStableState, QuorumStateDown)
	}
}

func TestRuntimeLoseEvidenceTurnsStableCheckIntoError(t *testing.T) {
	runtime := NewRuntime([]string{"check-a"}, []string{"probe-a", "probe-b", "probe-c"})
	at := time.Date(2026, 4, 7, 10, 0, 0, 0, time.UTC)
	expiresAt := at.Add(30 * time.Second)

	if _, err := runtime.ObserveCheckUp("check-a", "probe-a", at, &expiresAt); err != nil {
		t.Fatalf("ObserveCheckUp probe-a: %v", err)
	}
	if _, err := runtime.ObserveCheckUp("check-a", "probe-b", at, &expiresAt); err != nil {
		t.Fatalf("ObserveCheckUp probe-b: %v", err)
	}
	secondAt := at.Add(time.Second)
	secondExpiry := secondAt.Add(30 * time.Second)
	if _, err := runtime.ObserveCheckUp("check-a", "probe-a", secondAt, &secondExpiry); err != nil {
		t.Fatalf("ObserveCheckUp probe-a second: %v", err)
	}
	if _, err := runtime.ObserveCheckUp("check-a", "probe-b", secondAt, &secondExpiry); err != nil {
		t.Fatalf("ObserveCheckUp probe-b second: %v", err)
	}

	if _, err := runtime.RecomputeCheck("check-a"); err != nil {
		t.Fatalf("RecomputeCheck: %v", err)
	}

	update, err := runtime.LoseCheckEvidence("check-a", "probe-b")
	if err != nil {
		t.Fatalf("LoseCheckEvidence: %v", err)
	}

	if update.QuorumTransition.To != QuorumStateError {
		t.Fatalf("quorum transition to = %q, want %q", update.QuorumTransition.To, QuorumStateError)
	}
	if update.Quorum.LastStableState != QuorumStateUp {
		t.Fatalf("last stable state = %q, want %q", update.Quorum.LastStableState, QuorumStateUp)
	}
}

func TestRuntimeErrorsForUnknownEntities(t *testing.T) {
	runtime := NewRuntime([]string{"check-a"}, []string{"probe-a"})

	if _, err := runtime.ProbeSnapshot("missing"); err != ErrUnknownProbe {
		t.Fatalf("ProbeSnapshot err = %v, want %v", err, ErrUnknownProbe)
	}
	if _, err := runtime.CheckSnapshot("check-a", "missing"); err != ErrUnknownCheckAssignment {
		t.Fatalf("CheckSnapshot err = %v, want %v", err, ErrUnknownCheckAssignment)
	}
	if _, err := runtime.CheckSnapshot("missing", "probe-a"); err != ErrUnknownCheck {
		t.Fatalf("CheckSnapshot missing check err = %v, want %v", err, ErrUnknownCheck)
	}
	if _, err := runtime.QuorumSnapshot("missing"); err != ErrUnknownCheck {
		t.Fatalf("QuorumSnapshot err = %v, want %v", err, ErrUnknownCheck)
	}
}
