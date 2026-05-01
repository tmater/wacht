package monitoring

import (
	"testing"
	"time"
)

// TestNewRuntimeInitializesMachineRegistries verifies the runtime boot shape
// for active checks and probes.
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

// TestRuntimeRoutesHeartbeatToProbeMachine verifies that heartbeat routing
// updates the owning probe runtime state.
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

// TestRuntimeObserveCheckDownRecomputesQuorum verifies that repeated down
// observations update both child state and aggregate quorum.
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
	update, err = runtime.ObserveCheckDown("check-a", "probe-a", secondAt.Add(time.Second), ptrTime(secondExpiry.Add(time.Second)), "timeout")
	if err != nil {
		t.Fatalf("ObserveCheckDown probe-a third: %v", err)
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

// TestRuntimeLoseEvidenceTurnsStableCheckIntoError verifies that missing
// evidence degrades a previously stable quorum into error.
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

// TestRuntimeExpireHeartbeatInvalidatesAssignedCheckEvidence verifies that a
// stale probe heartbeat removes that probe's quorum contribution immediately.
func TestRuntimeExpireHeartbeatInvalidatesAssignedCheckEvidence(t *testing.T) {
	runtime := NewRuntime([]string{"check-a"}, []string{"probe-a", "probe-b", "probe-c"})
	at := time.Date(2026, 4, 7, 10, 0, 0, 0, time.UTC)
	expiresAt := at.Add(30 * time.Second)

	for _, probeID := range []string{"probe-a", "probe-b"} {
		if _, err := runtime.ObserveCheckUp("check-a", probeID, at, &expiresAt); err != nil {
			t.Fatalf("ObserveCheckUp %s first: %v", probeID, err)
		}
	}
	secondAt := at.Add(time.Second)
	secondExpiry := secondAt.Add(30 * time.Second)
	for _, probeID := range []string{"probe-a", "probe-b"} {
		if _, err := runtime.ObserveCheckUp("check-a", probeID, secondAt, &secondExpiry); err != nil {
			t.Fatalf("ObserveCheckUp %s second: %v", probeID, err)
		}
	}

	if _, err := runtime.ReceiveHeartbeat("probe-a", at); err != nil {
		t.Fatalf("ReceiveHeartbeat probe-a: %v", err)
	}
	if _, err := runtime.ReceiveHeartbeat("probe-b", at); err != nil {
		t.Fatalf("ReceiveHeartbeat probe-b: %v", err)
	}
	if _, err := runtime.ExpireHeartbeat("probe-a"); err != nil {
		t.Fatalf("ExpireHeartbeat: %v", err)
	}

	check, err := runtime.CheckSnapshot("check-a", "probe-a")
	if err != nil {
		t.Fatalf("CheckSnapshot() error = %v", err)
	}
	if check.State != CheckStateMissing {
		t.Fatalf("check state = %q, want %q", check.State, CheckStateMissing)
	}
	if check.LastOutcome != "" {
		t.Fatalf("last outcome = %q, want empty", check.LastOutcome)
	}

	quorum, err := runtime.QuorumSnapshot("check-a")
	if err != nil {
		t.Fatalf("QuorumSnapshot() error = %v", err)
	}
	if quorum.State != QuorumStateError {
		t.Fatalf("quorum state = %q, want %q", quorum.State, QuorumStateError)
	}
	if quorum.LastStableState != QuorumStateUp {
		t.Fatalf("last stable state = %q, want %q", quorum.LastStableState, QuorumStateUp)
	}
}

// TestRuntimeMarkProbeErrorInvalidatesAssignedChecksUntilFreshResults
// verifies that probe-wide runtime failure propagates to assigned checks
// without being cleared by heartbeat recovery alone.
func TestRuntimeMarkProbeErrorInvalidatesAssignedChecksUntilFreshResults(t *testing.T) {
	runtime := NewRuntime([]string{"check-a"}, []string{"probe-a", "probe-b", "probe-c"})
	at := time.Date(2026, 4, 7, 10, 0, 0, 0, time.UTC)
	expiresAt := at.Add(30 * time.Second)

	for _, probeID := range []string{"probe-a", "probe-b"} {
		if _, err := runtime.ObserveCheckUp("check-a", probeID, at, &expiresAt); err != nil {
			t.Fatalf("ObserveCheckUp %s first: %v", probeID, err)
		}
	}
	secondAt := at.Add(time.Second)
	secondExpiry := secondAt.Add(30 * time.Second)
	for _, probeID := range []string{"probe-a", "probe-b"} {
		if _, err := runtime.ObserveCheckUp("check-a", probeID, secondAt, &secondExpiry); err != nil {
			t.Fatalf("ObserveCheckUp %s second: %v", probeID, err)
		}
	}

	thirdAt := at.Add(2 * time.Second)
	thirdExpiry := thirdAt.Add(30 * time.Second)
	for _, probeID := range []string{"probe-a", "probe-b"} {
		if _, err := runtime.ObserveCheckDown("check-a", probeID, thirdAt, &thirdExpiry, "timeout"); err != nil {
			t.Fatalf("ObserveCheckDown %s first: %v", probeID, err)
		}
	}

	fourthAt := at.Add(3 * time.Second)
	fourthExpiry := fourthAt.Add(30 * time.Second)
	for _, probeID := range []string{"probe-a", "probe-b"} {
		if _, err := runtime.ObserveCheckDown("check-a", probeID, fourthAt, &fourthExpiry, "timeout"); err != nil {
			t.Fatalf("ObserveCheckDown %s second: %v", probeID, err)
		}
	}
	if _, err := runtime.ObserveCheckDown("check-a", "probe-a", at.Add(4*time.Second), ptrTime(at.Add(34*time.Second)), "timeout"); err != nil {
		t.Fatalf("ObserveCheckDown probe-a third: %v", err)
	}

	if _, err := runtime.MarkProbeError("probe-a", "transport failed"); err != nil {
		t.Fatalf("MarkProbeError: %v", err)
	}

	check, err := runtime.CheckSnapshot("check-a", "probe-a")
	if err != nil {
		t.Fatalf("CheckSnapshot() error = %v", err)
	}
	if check.State != CheckStateError {
		t.Fatalf("check state = %q, want %q", check.State, CheckStateError)
	}
	if check.LastError != "transport failed" {
		t.Fatalf("last error = %q, want transport failed", check.LastError)
	}

	quorum, err := runtime.QuorumSnapshot("check-a")
	if err != nil {
		t.Fatalf("QuorumSnapshot() error = %v", err)
	}
	if quorum.State != QuorumStateError {
		t.Fatalf("quorum state = %q, want %q", quorum.State, QuorumStateError)
	}
	if quorum.LastStableState != QuorumStateDown {
		t.Fatalf("last stable state = %q, want %q", quorum.LastStableState, QuorumStateDown)
	}
	if !quorum.IncidentOpen {
		t.Fatal("IncidentOpen = false, want true")
	}

	recoveredAt := at.Add(2 * time.Minute)
	if _, err := runtime.ReceiveHeartbeat("probe-a", recoveredAt); err != nil {
		t.Fatalf("ReceiveHeartbeat recovery: %v", err)
	}

	check, err = runtime.CheckSnapshot("check-a", "probe-a")
	if err != nil {
		t.Fatalf("CheckSnapshot() after recovery error = %v", err)
	}
	if check.State != CheckStateError {
		t.Fatalf("check state after recovery = %q, want %q", check.State, CheckStateError)
	}
}

// TestRuntimeErrorsForUnknownEntities verifies the runtime's stable error
// contract for missing probes, checks, and assignments.
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
