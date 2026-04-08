package monitoring

import (
	"errors"
	"testing"
	"time"

	"github.com/tmater/wacht/internal/checks"
	"github.com/tmater/wacht/internal/proto"
	"github.com/tmater/wacht/internal/store"
)

type fakeResultStore struct {
	saveResultFn             func(r proto.CheckResult) error
	persistMonitoringWriteFn func(write store.MonitoringWrite) (store.MonitoringWrite, bool, error)
	savedResults             []proto.CheckResult
	persistedWrites          []store.MonitoringWrite
}

func (f *fakeResultStore) SaveResult(r proto.CheckResult) error {
	f.savedResults = append(f.savedResults, r)
	if f.saveResultFn != nil {
		return f.saveResultFn(r)
	}
	return nil
}

func (f *fakeResultStore) PersistMonitoringWrite(write store.MonitoringWrite) (store.MonitoringWrite, bool, error) {
	f.persistedWrites = append(f.persistedWrites, write)
	if f.persistMonitoringWriteFn != nil {
		return f.persistMonitoringWriteFn(write)
	}
	return write, false, nil
}

func applyResultSequence(t *testing.T, runtime *Runtime, st *fakeResultStore, check checks.Check, results []proto.CheckResult) {
	t.Helper()
	for _, result := range results {
		if err := ApplyResult(runtime, st, check, result); err != nil {
			t.Fatalf("ApplyResult(%+v) error = %v", result, err)
		}
	}
}

func TestApplyResultRollsBackRuntimeWhenPersistFails(t *testing.T) {
	persistErr := errors.New("persist failed")
	st := &fakeResultStore{
		persistMonitoringWriteFn: func(write store.MonitoringWrite) (store.MonitoringWrite, bool, error) {
			return store.MonitoringWrite{}, false, persistErr
		},
	}
	runtime := NewRuntime([]string{"check-a"}, []string{"probe-a", "probe-b"})
	check := checks.NewCheck("check-a", "http", "https://example.com", "", 30)
	at := time.Date(2026, time.April, 8, 12, 0, 0, 0, time.UTC)

	err := ApplyResult(runtime, st, check, proto.CheckResult{
		CheckID:   "check-a",
		ProbeID:   "probe-a",
		Up:        false,
		Error:     "timeout",
		Timestamp: at,
		Type:      string(check.Type),
		Target:    check.Target,
	})
	if !errors.Is(err, persistErr) {
		t.Fatalf("ApplyResult() error = %v, want %v", err, persistErr)
	}

	exec, err := runtime.CheckSnapshot("check-a", "probe-a")
	if err != nil {
		t.Fatalf("CheckSnapshot() error = %v", err)
	}
	if exec.State != CheckStateMissing {
		t.Fatalf("check state = %q, want %q", exec.State, CheckStateMissing)
	}
	if exec.StreakLen != 0 {
		t.Fatalf("streak = %d, want 0", exec.StreakLen)
	}
	if !exec.LastResultAt.IsZero() {
		t.Fatalf("last result at = %v, want zero", exec.LastResultAt)
	}

	quorum, err := runtime.QuorumSnapshot("check-a")
	if err != nil {
		t.Fatalf("QuorumSnapshot() error = %v", err)
	}
	if quorum.State != QuorumStatePending {
		t.Fatalf("quorum state = %q, want %q", quorum.State, QuorumStatePending)
	}
	if quorum.IncidentOpen {
		t.Fatal("IncidentOpen = true, want false")
	}
}

func TestApplyResultDoesNotResolveWithoutOpenIncident(t *testing.T) {
	st := &fakeResultStore{}
	runtime := NewRuntime([]string{"check-a"}, []string{"probe-a", "probe-b"})
	check := checks.NewCheck("check-a", "http", "https://example.com", "https://hooks.example.com/wacht", 30)
	at := time.Date(2026, time.April, 8, 12, 0, 0, 0, time.UTC)

	results := []proto.CheckResult{
		{CheckID: "check-a", ProbeID: "probe-a", Up: false, Error: "timeout", Timestamp: at},
		{CheckID: "check-a", ProbeID: "probe-b", Up: false, Error: "timeout", Timestamp: at.Add(time.Second)},
		{CheckID: "check-a", ProbeID: "probe-a", Up: false, Error: "timeout", Timestamp: at.Add(2 * time.Second)},
		{CheckID: "check-a", ProbeID: "probe-b", Up: false, Error: "timeout", Timestamp: at.Add(3 * time.Second)},
		{CheckID: "check-a", ProbeID: "probe-a", Up: true, Timestamp: at.Add(4 * time.Second)},
		{CheckID: "check-a", ProbeID: "probe-b", Up: true, Timestamp: at.Add(5 * time.Second)},
		{CheckID: "check-a", ProbeID: "probe-a", Up: true, Timestamp: at.Add(6 * time.Second)},
		{CheckID: "check-a", ProbeID: "probe-b", Up: true, Timestamp: at.Add(7 * time.Second)},
	}
	applyResultSequence(t, runtime, st, check, results)

	initialDownWrite := st.persistedWrites[3]
	if initialDownWrite.IncidentCheckID != "" {
		t.Fatalf("initial down IncidentCheckID = %q, want empty", initialDownWrite.IncidentCheckID)
	}

	finalWrite := st.persistedWrites[len(st.persistedWrites)-1]
	if finalWrite.IncidentCheckID != "" {
		t.Fatalf("resolve IncidentCheckID = %q, want empty", finalWrite.IncidentCheckID)
	}
	if finalWrite.ResolveIncident {
		t.Fatal("ResolveIncident = true, want false")
	}

	quorum, err := runtime.QuorumSnapshot("check-a")
	if err != nil {
		t.Fatalf("QuorumSnapshot() error = %v", err)
	}
	if quorum.State != QuorumStateUp {
		t.Fatalf("quorum state = %q, want %q", quorum.State, QuorumStateUp)
	}
	if quorum.IncidentOpen {
		t.Fatal("IncidentOpen = true, want false")
	}
}

func TestApplyResultResolvesExistingIncident(t *testing.T) {
	st := &fakeResultStore{}
	runtime := NewRuntime([]string{"check-a"}, []string{"probe-a", "probe-b"})
	check := checks.NewCheck("check-a", "http", "https://example.com", "https://hooks.example.com/wacht", 30)
	at := time.Date(2026, time.April, 8, 12, 0, 0, 0, time.UTC)

	results := []proto.CheckResult{
		{CheckID: "check-a", ProbeID: "probe-a", Up: true, Timestamp: at},
		{CheckID: "check-a", ProbeID: "probe-b", Up: true, Timestamp: at.Add(time.Second)},
		{CheckID: "check-a", ProbeID: "probe-a", Up: true, Timestamp: at.Add(2 * time.Second)},
		{CheckID: "check-a", ProbeID: "probe-b", Up: true, Timestamp: at.Add(3 * time.Second)},
		{CheckID: "check-a", ProbeID: "probe-a", Up: false, Error: "timeout", Timestamp: at.Add(4 * time.Second)},
		{CheckID: "check-a", ProbeID: "probe-b", Up: false, Error: "timeout", Timestamp: at.Add(5 * time.Second)},
		{CheckID: "check-a", ProbeID: "probe-a", Up: false, Error: "timeout", Timestamp: at.Add(6 * time.Second)},
		{CheckID: "check-a", ProbeID: "probe-b", Up: false, Error: "timeout", Timestamp: at.Add(7 * time.Second)},
		{CheckID: "check-a", ProbeID: "probe-a", Up: true, Timestamp: at.Add(8 * time.Second)},
		{CheckID: "check-a", ProbeID: "probe-b", Up: true, Timestamp: at.Add(9 * time.Second)},
		{CheckID: "check-a", ProbeID: "probe-a", Up: true, Timestamp: at.Add(10 * time.Second)},
		{CheckID: "check-a", ProbeID: "probe-b", Up: true, Timestamp: at.Add(11 * time.Second)},
	}
	applyResultSequence(t, runtime, st, check, results)

	openWrite := st.persistedWrites[7]
	if openWrite.IncidentCheckID != "check-a" {
		t.Fatalf("open IncidentCheckID = %q, want check-a", openWrite.IncidentCheckID)
	}
	if openWrite.ResolveIncident {
		t.Fatal("open ResolveIncident = true, want false")
	}

	resolveWrite := st.persistedWrites[len(st.persistedWrites)-1]
	if resolveWrite.IncidentCheckID != "check-a" {
		t.Fatalf("resolve IncidentCheckID = %q, want check-a", resolveWrite.IncidentCheckID)
	}
	if !resolveWrite.ResolveIncident {
		t.Fatal("ResolveIncident = false, want true")
	}

	quorum, err := runtime.QuorumSnapshot("check-a")
	if err != nil {
		t.Fatalf("QuorumSnapshot() error = %v", err)
	}
	if quorum.State != QuorumStateUp {
		t.Fatalf("quorum state = %q, want %q", quorum.State, QuorumStateUp)
	}
	if quorum.IncidentOpen {
		t.Fatal("IncidentOpen = true, want false")
	}
}

func TestApplyResultOpensIncidentAfterInitialHealthyBaseline(t *testing.T) {
	st := &fakeResultStore{}
	runtime := NewRuntime([]string{"check-a"}, []string{"probe-a", "probe-b"})
	check := checks.NewCheck("check-a", "http", "https://example.com", "https://hooks.example.com/wacht", 30)
	at := time.Date(2026, time.April, 8, 12, 0, 0, 0, time.UTC)

	results := []proto.CheckResult{
		{CheckID: "check-a", ProbeID: "probe-a", Up: true, Timestamp: at},
		{CheckID: "check-a", ProbeID: "probe-b", Up: true, Timestamp: at.Add(time.Second)},
		{CheckID: "check-a", ProbeID: "probe-a", Up: false, Error: "timeout", Timestamp: at.Add(2 * time.Second)},
		{CheckID: "check-a", ProbeID: "probe-b", Up: false, Error: "timeout", Timestamp: at.Add(3 * time.Second)},
		{CheckID: "check-a", ProbeID: "probe-a", Up: false, Error: "timeout", Timestamp: at.Add(4 * time.Second)},
		{CheckID: "check-a", ProbeID: "probe-b", Up: false, Error: "timeout", Timestamp: at.Add(5 * time.Second)},
	}
	applyResultSequence(t, runtime, st, check, results)

	quorum, err := runtime.QuorumSnapshot("check-a")
	if err != nil {
		t.Fatalf("QuorumSnapshot() error = %v", err)
	}
	if quorum.State != QuorumStateDown {
		t.Fatalf("quorum state = %q, want %q", quorum.State, QuorumStateDown)
	}
	if !quorum.IncidentOpen {
		t.Fatal("IncidentOpen = false, want true")
	}

	openWrite := st.persistedWrites[len(st.persistedWrites)-1]
	if openWrite.IncidentCheckID != "check-a" {
		t.Fatalf("IncidentCheckID = %q, want check-a", openWrite.IncidentCheckID)
	}
	if openWrite.ResolveIncident {
		t.Fatal("ResolveIncident = true, want false")
	}
}

func TestApplyResultDoesNotOpenIncidentDuringFlapping(t *testing.T) {
	st := &fakeResultStore{}
	runtime := NewRuntime([]string{"check-a"}, []string{"probe-a", "probe-b", "probe-c"})
	check := checks.NewCheck("check-a", "http", "https://example.com", "https://hooks.example.com/wacht", 30)
	at := time.Date(2026, time.April, 8, 12, 0, 0, 0, time.UTC)

	results := []proto.CheckResult{
		{CheckID: "check-a", ProbeID: "probe-a", Up: false, Error: "timeout", Timestamp: at},
		{CheckID: "check-a", ProbeID: "probe-b", Up: true, Timestamp: at.Add(time.Second)},
		{CheckID: "check-a", ProbeID: "probe-c", Up: false, Error: "timeout", Timestamp: at.Add(2 * time.Second)},
		{CheckID: "check-a", ProbeID: "probe-a", Up: true, Timestamp: at.Add(3 * time.Second)},
		{CheckID: "check-a", ProbeID: "probe-b", Up: false, Error: "timeout", Timestamp: at.Add(4 * time.Second)},
		{CheckID: "check-a", ProbeID: "probe-c", Up: true, Timestamp: at.Add(5 * time.Second)},
	}
	applyResultSequence(t, runtime, st, check, results)

	for i, write := range st.persistedWrites {
		if write.IncidentCheckID != "" {
			t.Fatalf("write %d IncidentCheckID = %q, want empty", i, write.IncidentCheckID)
		}
	}

	quorum, err := runtime.QuorumSnapshot("check-a")
	if err != nil {
		t.Fatalf("QuorumSnapshot() error = %v", err)
	}
	if quorum.State != QuorumStateUp {
		t.Fatalf("quorum state = %q, want %q", quorum.State, QuorumStateUp)
	}
	if quorum.IncidentOpen {
		t.Fatal("IncidentOpen = true, want false")
	}
}
