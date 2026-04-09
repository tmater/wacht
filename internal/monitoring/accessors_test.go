package monitoring

import (
	"testing"
	"time"
)

func TestRuntimeEnsureCheckCreatesExplicitPendingState(t *testing.T) {
	runtime := NewRuntime(nil, []string{"probe-a", "probe-b"})

	quorum := runtime.EnsureCheck("check-a")
	if quorum.CheckID != "check-a" {
		t.Fatalf("CheckID = %q, want check-a", quorum.CheckID)
	}
	if quorum.State != QuorumStatePending {
		t.Fatalf("State = %q, want %q", quorum.State, QuorumStatePending)
	}

	snapshot, err := runtime.QuorumSnapshot("check-a")
	if err != nil {
		t.Fatalf("QuorumSnapshot() error = %v", err)
	}
	if snapshot.State != QuorumStatePending {
		t.Fatalf("snapshot.State = %q, want %q", snapshot.State, QuorumStatePending)
	}
}

func TestRuntimeQuorumSnapshotsReturnsPendingForUnknownChecks(t *testing.T) {
	runtime := NewRuntime([]string{"known-check"}, []string{"probe-a"})

	quorums := runtime.QuorumSnapshots([]string{"missing-check", "known-check", "missing-check"})
	if len(quorums) != 2 {
		t.Fatalf("len(quorums) = %d, want 2", len(quorums))
	}
	if quorums[0].CheckID != "missing-check" || quorums[0].State != QuorumStatePending {
		t.Fatalf("quorums[0] = %#v, want pending missing-check", quorums[0])
	}
	if quorums[1].CheckID != "known-check" || quorums[1].State != QuorumStatePending {
		t.Fatalf("quorums[1] = %#v, want pending known-check", quorums[1])
	}
}

func TestRuntimeRemoveCheckDeletesRuntimeState(t *testing.T) {
	runtime := NewRuntime([]string{"check-a"}, []string{"probe-a"})

	runtime.RemoveCheck("check-a")

	if _, err := runtime.QuorumSnapshot("check-a"); err != ErrUnknownCheck {
		t.Fatalf("QuorumSnapshot() error = %v, want %v", err, ErrUnknownCheck)
	}
}

func TestRuntimeProbeSnapshotsReturnsSortedSnapshots(t *testing.T) {
	runtime := NewRuntime(nil, []string{"probe-c", "probe-a", "probe-b"})
	heartbeatAt := time.Date(2026, time.April, 8, 12, 0, 0, 0, time.UTC)

	if _, err := runtime.ReceiveHeartbeat("probe-b", heartbeatAt); err != nil {
		t.Fatalf("ReceiveHeartbeat probe-b: %v", err)
	}

	probes := runtime.ProbeSnapshots()
	if len(probes) != 3 {
		t.Fatalf("len(probes) = %d, want 3", len(probes))
	}
	if probes[0].ProbeID != "probe-a" || probes[1].ProbeID != "probe-b" || probes[2].ProbeID != "probe-c" {
		t.Fatalf("probe order = %#v, want probe-a/probe-b/probe-c", probes)
	}
	if probes[1].LastHeartbeatAt == nil || !probes[1].LastHeartbeatAt.Equal(heartbeatAt) {
		t.Fatalf("probe-b LastHeartbeatAt = %v, want %s", probes[1].LastHeartbeatAt, heartbeatAt)
	}
}
