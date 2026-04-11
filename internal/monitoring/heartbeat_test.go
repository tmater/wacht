package monitoring

import (
	"errors"
	"testing"
	"time"

	"github.com/tmater/wacht/internal/store"
)

type fakeHeartbeatStore struct {
	persistMonitoringWriteFn func(write store.MonitoringWrite) (store.MonitoringWrite, error)
	persistedWrites          []store.MonitoringWrite
}

// PersistMonitoringWrite captures heartbeat writes for monitoring tests.
func (f *fakeHeartbeatStore) PersistMonitoringWrite(write store.MonitoringWrite) (store.MonitoringWrite, error) {
	f.persistedWrites = append(f.persistedWrites, write)
	if f.persistMonitoringWriteFn != nil {
		return f.persistMonitoringWriteFn(write)
	}
	return write, nil
}

// TestApplyHeartbeatPersistsProbeRuntimeTransition verifies that one accepted
// heartbeat updates runtime state and emits the expected persistence write.
func TestApplyHeartbeatPersistsProbeRuntimeTransition(t *testing.T) {
	st := &fakeHeartbeatStore{}
	runtime := NewRuntime([]string{"check-a"}, []string{"probe-a"})
	at := time.Date(2026, time.April, 8, 14, 0, 0, 0, time.UTC)

	if err := ApplyHeartbeat(runtime, st, "probe-a", at); err != nil {
		t.Fatalf("ApplyHeartbeat() error = %v", err)
	}

	if len(st.persistedWrites) != 1 {
		t.Fatalf("persisted writes = %d, want 1", len(st.persistedWrites))
	}
	write := st.persistedWrites[0]
	if write.ProbeHeartbeatID != "probe-a" {
		t.Fatalf("ProbeHeartbeatID = %q, want probe-a", write.ProbeHeartbeatID)
	}
	if !write.ProbeHeartbeatAt.Equal(at) {
		t.Fatalf("ProbeHeartbeatAt = %s, want %s", write.ProbeHeartbeatAt, at)
	}

	probe, err := runtime.ProbeSnapshot("probe-a")
	if err != nil {
		t.Fatalf("ProbeSnapshot() error = %v", err)
	}
	if probe.State != ProbeStateOnline {
		t.Fatalf("probe state = %q, want %q", probe.State, ProbeStateOnline)
	}
	if probe.LastHeartbeatAt == nil || !probe.LastHeartbeatAt.Equal(at) {
		t.Fatalf("LastHeartbeatAt = %v, want %v", probe.LastHeartbeatAt, at)
	}
}

// TestApplyHeartbeatRollsBackRuntimeWhenPersistFails verifies that runtime
// heartbeat state is reverted when the durable write fails.
func TestApplyHeartbeatRollsBackRuntimeWhenPersistFails(t *testing.T) {
	persistErr := errors.New("persist failed")
	st := &fakeHeartbeatStore{
		persistMonitoringWriteFn: func(write store.MonitoringWrite) (store.MonitoringWrite, error) {
			return store.MonitoringWrite{}, persistErr
		},
	}
	runtime := NewRuntime([]string{"check-a"}, []string{"probe-a"})
	at := time.Date(2026, time.April, 8, 14, 0, 0, 0, time.UTC)

	err := ApplyHeartbeat(runtime, st, "probe-a", at)
	if !errors.Is(err, persistErr) {
		t.Fatalf("ApplyHeartbeat() error = %v, want %v", err, persistErr)
	}

	probe, err := runtime.ProbeSnapshot("probe-a")
	if err != nil {
		t.Fatalf("ProbeSnapshot() error = %v", err)
	}
	if probe.State != ProbeStateOffline {
		t.Fatalf("probe state = %q, want %q", probe.State, ProbeStateOffline)
	}
	if probe.LastHeartbeatAt != nil {
		t.Fatalf("LastHeartbeatAt = %v, want nil", probe.LastHeartbeatAt)
	}
}
