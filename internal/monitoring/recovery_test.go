package monitoring

import (
	"testing"
	"time"

	"github.com/tmater/wacht/internal/checks"
	"github.com/tmater/wacht/internal/store"
)

// fakeRecoveryStore is a minimal recoveryStore test double for boot and
// current-state recovery scenarios.
type fakeRecoveryStore struct {
	checks              []checks.Check
	probes              []store.PersistedProbeState
	checkStates         []store.PersistedCheckState
	openIncidentCheckID []string
}

func testRecoveryCheck(checkID, id, checkType, target string, interval int) checks.Check {
	check := checks.NewCheck(id, checkType, target, "", interval)
	check.ID = checkID
	return check
}

func (f *fakeRecoveryStore) ListAllChecks() ([]checks.Check, error) {
	return append([]checks.Check(nil), f.checks...), nil
}

func (f *fakeRecoveryStore) ActiveProbeStates() ([]store.PersistedProbeState, error) {
	probes := make([]store.PersistedProbeState, 0, len(f.probes))
	for _, probe := range f.probes {
		if probe.LastSeenAt != nil {
			lastSeenAt := *probe.LastSeenAt
			probe.LastSeenAt = &lastSeenAt
		}
		probes = append(probes, probe)
	}
	return probes, nil
}

func (f *fakeRecoveryStore) PersistedCheckStates() ([]store.PersistedCheckState, error) {
	return append([]store.PersistedCheckState(nil), f.checkStates...), nil
}

func (f *fakeRecoveryStore) OpenIncidentCheckIDs() ([]string, error) {
	return append([]string(nil), f.openIncidentCheckID...), nil
}

func TestLoadRuntimeUsesMetadataDefaultsWithoutRecoveryData(t *testing.T) {
	checkA := testRecoveryCheck("00000000-0000-0000-0000-000000000201", "check-a", "http", "https://a.example.com", 30)
	checkB := testRecoveryCheck("00000000-0000-0000-0000-000000000202", "check-b", "http", "https://b.example.com", 30)
	recovered, err := LoadRuntime(&fakeRecoveryStore{
		checks: []checks.Check{
			checkA,
			checkB,
		},
		probes: []store.PersistedProbeState{
			{ProbeID: "probe-a"},
			{ProbeID: "probe-b"},
		},
	})
	if err != nil {
		t.Fatalf("LoadRuntime: %v", err)
	}

	probe, err := recovered.ProbeSnapshot("probe-a")
	if err != nil {
		t.Fatalf("ProbeSnapshot: %v", err)
	}
	if probe.State != ProbeStateOffline {
		t.Fatalf("probe state = %q, want %q", probe.State, ProbeStateOffline)
	}

	check, err := recovered.CheckSnapshot(checkA.ID, "probe-a")
	if err != nil {
		t.Fatalf("CheckSnapshot: %v", err)
	}
	if check.State != CheckStateMissing {
		t.Fatalf("check state = %q, want %q", check.State, CheckStateMissing)
	}

	quorum, err := recovered.QuorumSnapshot(checkB.ID)
	if err != nil {
		t.Fatalf("QuorumSnapshot: %v", err)
	}
	if quorum.State != QuorumStatePending {
		t.Fatalf("quorum state = %q, want %q", quorum.State, QuorumStatePending)
	}
}

func TestLoadRuntimeRestoresPersistedCurrentStateAndOpenIncident(t *testing.T) {
	firstAt := time.Date(2026, time.April, 8, 7, 0, 0, 0, time.UTC)
	secondAt := firstAt.Add(time.Second)
	check := testRecoveryCheck("00000000-0000-0000-0000-000000000203", "check-a", "http", "https://a.example.com", 30)
	checkID := check.ID
	recovered, err := LoadRuntime(&fakeRecoveryStore{
		checks: []checks.Check{
			check,
		},
		probes: []store.PersistedProbeState{
			{ProbeID: "probe-a", LastSeenAt: &firstAt},
			{ProbeID: "probe-b", LastSeenAt: &secondAt},
			{ProbeID: "probe-c"},
		},
		checkStates: []store.PersistedCheckState{
			{
				CheckID:      checkID,
				ProbeID:      "probe-a",
				LastResultAt: firstAt,
				LastOutcome:  "down",
				StreakLen:    2,
				ExpiresAt:    firstAt.Add(30 * time.Second),
				State:        "down",
				LastError:    "timeout",
			},
			{
				CheckID:      checkID,
				ProbeID:      "probe-b",
				LastResultAt: secondAt,
				LastOutcome:  "down",
				StreakLen:    2,
				ExpiresAt:    secondAt.Add(30 * time.Second),
				State:        "down",
				LastError:    "timeout",
			},
		},
		openIncidentCheckID: []string{checkID},
	})
	if err != nil {
		t.Fatalf("LoadRuntime: %v", err)
	}

	probeA, err := recovered.ProbeSnapshot("probe-a")
	if err != nil {
		t.Fatalf("ProbeSnapshot probe-a: %v", err)
	}
	if probeA.State != ProbeStateOnline {
		t.Fatalf("probe-a state = %q, want %q", probeA.State, ProbeStateOnline)
	}
	if probeA.LastHeartbeatAt == nil || !probeA.LastHeartbeatAt.Equal(firstAt) {
		t.Fatalf("probe-a last heartbeat = %v, want %v", probeA.LastHeartbeatAt, firstAt)
	}

	probeC, err := recovered.ProbeSnapshot("probe-c")
	if err != nil {
		t.Fatalf("ProbeSnapshot probe-c: %v", err)
	}
	if probeC.State != ProbeStateOffline {
		t.Fatalf("probe-c state = %q, want %q", probeC.State, ProbeStateOffline)
	}

	state, err := recovered.CheckSnapshot(checkID, "probe-b")
	if err != nil {
		t.Fatalf("CheckSnapshot: %v", err)
	}
	if state.State != CheckStateDown {
		t.Fatalf("check state = %q, want %q", state.State, CheckStateDown)
	}
	if !state.LastResultAt.Equal(secondAt) {
		t.Fatalf("check LastResultAt = %s, want %s", state.LastResultAt, secondAt)
	}

	quorum, err := recovered.QuorumSnapshot(checkID)
	if err != nil {
		t.Fatalf("QuorumSnapshot: %v", err)
	}
	if quorum.State != QuorumStateDown {
		t.Fatalf("quorum state = %q, want %q", quorum.State, QuorumStateDown)
	}
	if quorum.LastStableState != QuorumStateDown {
		t.Fatalf("last stable state = %q, want %q", quorum.LastStableState, QuorumStateDown)
	}
	if !quorum.IncidentOpen {
		t.Fatal("IncidentOpen = false, want true")
	}
}

func TestLoadRuntimeClearsPersistedVotesForProbesWithoutLiveness(t *testing.T) {
	at := time.Date(2026, time.April, 8, 7, 0, 0, 0, time.UTC)
	check := testRecoveryCheck("00000000-0000-0000-0000-000000000204", "check-a", "http", "https://a.example.com", 30)
	checkID := check.ID
	recovered, err := LoadRuntime(&fakeRecoveryStore{
		checks: []checks.Check{
			check,
		},
		probes: []store.PersistedProbeState{
			{ProbeID: "probe-a"},
		},
		checkStates: []store.PersistedCheckState{
			{
				CheckID:      checkID,
				ProbeID:      "probe-a",
				LastResultAt: at,
				LastOutcome:  "up",
				StreakLen:    2,
				ExpiresAt:    at.Add(30 * time.Second),
				State:        "up",
			},
			{
				CheckID:      "missing-check",
				ProbeID:      "probe-a",
				LastResultAt: at,
				LastOutcome:  "down",
				StreakLen:    2,
				ExpiresAt:    at.Add(30 * time.Second),
				State:        "down",
				LastError:    "timeout",
			},
		},
	})
	if err != nil {
		t.Fatalf("LoadRuntime: %v", err)
	}

	state, err := recovered.CheckSnapshot(checkID, "probe-a")
	if err != nil {
		t.Fatalf("CheckSnapshot: %v", err)
	}
	if state.State != CheckStateMissing {
		t.Fatalf("check state = %q, want %q", state.State, CheckStateMissing)
	}
	if state.LastOutcome != "" {
		t.Fatalf("last outcome = %q, want empty", state.LastOutcome)
	}

	quorum, err := recovered.QuorumSnapshot(checkID)
	if err != nil {
		t.Fatalf("QuorumSnapshot: %v", err)
	}
	if quorum.State != QuorumStatePending {
		t.Fatalf("quorum state = %q, want %q", quorum.State, QuorumStatePending)
	}
}

func TestLoadRuntimeDoesNotCountPersistedMissingStateAsVote(t *testing.T) {
	at := time.Date(2026, time.April, 8, 7, 0, 0, 0, time.UTC)
	check := testRecoveryCheck("00000000-0000-0000-0000-000000000205", "check-a", "http", "https://a.example.com", 30)
	checkID := check.ID
	recovered, err := LoadRuntime(&fakeRecoveryStore{
		checks: []checks.Check{
			check,
		},
		probes: []store.PersistedProbeState{
			{ProbeID: "probe-a", LastSeenAt: &at},
		},
		checkStates: []store.PersistedCheckState{
			{
				CheckID:      checkID,
				ProbeID:      "probe-a",
				LastResultAt: at,
				LastOutcome:  "",
				StreakLen:    0,
				ExpiresAt:    at,
				State:        "missing",
			},
		},
	})
	if err != nil {
		t.Fatalf("LoadRuntime: %v", err)
	}

	state, err := recovered.CheckSnapshot(checkID, "probe-a")
	if err != nil {
		t.Fatalf("CheckSnapshot: %v", err)
	}
	if state.State != CheckStateMissing {
		t.Fatalf("check state = %q, want %q", state.State, CheckStateMissing)
	}
	if state.LastOutcome != "" {
		t.Fatalf("last outcome = %q, want empty", state.LastOutcome)
	}

	quorum, err := recovered.QuorumSnapshot(checkID)
	if err != nil {
		t.Fatalf("QuorumSnapshot: %v", err)
	}
	if quorum.State != QuorumStatePending {
		t.Fatalf("quorum state = %q, want %q", quorum.State, QuorumStatePending)
	}
}
