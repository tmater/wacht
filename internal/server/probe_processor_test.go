package server

import (
	"errors"
	"testing"

	probeapi "github.com/tmater/wacht/internal/api/probe"
	"github.com/tmater/wacht/internal/checks"
	"github.com/tmater/wacht/internal/monitoring"
	"github.com/tmater/wacht/internal/proto"
	"github.com/tmater/wacht/internal/store"
)

type fakeProbeStore struct {
	registerProbeFn          func(probeID, version string) error
	getCheckFn               func(id string) (*checks.Check, error)
	persistMonitoringWriteFn func(write store.MonitoringWrite) (store.MonitoringWrite, bool, error)
	registerProbeID          string
	registerVersion          string
	persistedWrites          []store.MonitoringWrite
}

// RegisterProbe records register calls made by probe processor tests.
func (f *fakeProbeStore) RegisterProbe(probeID, version string) error {
	f.registerProbeID = probeID
	f.registerVersion = version
	if f.registerProbeFn != nil {
		return f.registerProbeFn(probeID, version)
	}
	return nil
}

// GetCheck returns stubbed check metadata for probe processor tests.
func (f *fakeProbeStore) GetCheck(id string) (*checks.Check, error) {
	if f.getCheckFn != nil {
		return f.getCheckFn(id)
	}
	return nil, nil
}

// PersistMonitoringWrite records runtime persistence writes for probe
// processor tests.
func (f *fakeProbeStore) PersistMonitoringWrite(write store.MonitoringWrite) (store.MonitoringWrite, bool, error) {
	f.persistedWrites = append(f.persistedWrites, write)
	if f.persistMonitoringWriteFn != nil {
		return f.persistMonitoringWriteFn(write)
	}
	return write, false, nil
}

// processSequence feeds a deterministic result stream through the probe
// processor so tests can focus on aggregate outcomes.
func processSequence(t *testing.T, p *ProbeProcessor, checkID string, steps []struct {
	probeID string
	up      bool
}) {
	t.Helper()
	for _, step := range steps {
		message := ""
		if !step.up {
			message = "timeout"
		}
		if err := p.Process(&store.Probe{ProbeID: step.probeID}, proto.CheckResult{
			CheckID: checkID,
			Up:      step.up,
			Error:   message,
		}); err != nil {
			t.Fatalf("Process(%s, up=%t) error = %v", step.probeID, step.up, err)
		}
	}
}

// TestProbeProcessorHeartbeatUpdatesAuthenticatedProbe verifies that the HTTP
// heartbeat adapter delegates to runtime-owned monitoring writes.
func TestProbeProcessorHeartbeatUpdatesAuthenticatedProbe(t *testing.T) {
	s := &fakeProbeStore{}
	p := NewProbeProcessor(s, monitoring.NewRuntime(nil, []string{"probe-1"}))

	err := p.Heartbeat(&store.Probe{ProbeID: "probe-1"}, probeapi.HeartbeatRequest{})
	if err != nil {
		t.Fatalf("Heartbeat() error = %v", err)
	}
	if len(s.persistedWrites) != 1 {
		t.Fatalf("persisted writes = %d, want 1", len(s.persistedWrites))
	}
	write := s.persistedWrites[0]
	if len(write.JournalRecords) != 1 {
		t.Fatalf("journal records = %d, want 1", len(write.JournalRecords))
	}
	if write.JournalRecords[0].Kind != string(monitoring.ProbeTriggerReceiveHeartbeat) {
		t.Fatalf("journal kind = %q, want %q", write.JournalRecords[0].Kind, monitoring.ProbeTriggerReceiveHeartbeat)
	}
	if write.JournalRecords[0].ProbeID != "probe-1" {
		t.Fatalf("journal ProbeID = %q, want probe-1", write.JournalRecords[0].ProbeID)
	}
	if write.ProbeHeartbeatID != "probe-1" {
		t.Fatalf("ProbeHeartbeatID = %q, want probe-1", write.ProbeHeartbeatID)
	}
	if write.ProbeHeartbeatAt.IsZero() {
		t.Fatal("expected ProbeHeartbeatAt to be set")
	}

	probeState, err := p.runtime.ProbeSnapshot("probe-1")
	if err != nil {
		t.Fatalf("ProbeSnapshot() error = %v", err)
	}
	if probeState.State != monitoring.ProbeStateOnline {
		t.Fatalf("probe state = %q, want %q", probeState.State, monitoring.ProbeStateOnline)
	}
	if probeState.LastHeartbeatAt == nil {
		t.Fatal("expected LastHeartbeatAt to be set")
	}
}

// TestProbeProcessorRegisterRecordsVersion verifies that registration still
// writes probe startup metadata directly.
func TestProbeProcessorRegisterRecordsVersion(t *testing.T) {
	s := &fakeProbeStore{}
	p := NewProbeProcessor(s, monitoring.NewRuntime(nil, []string{"probe-1"}))

	err := p.Register(&store.Probe{ProbeID: "probe-1"}, probeapi.RegisterRequest{Version: "v1.2.3"})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if s.registerProbeID != "probe-1" {
		t.Fatalf("RegisterProbe probeID = %q, want probe-1", s.registerProbeID)
	}
	if s.registerVersion != "v1.2.3" {
		t.Fatalf("RegisterProbe version = %q, want v1.2.3", s.registerVersion)
	}
}

// TestProbeProcessorProcessNormalizesResultAndCreatesQuorum verifies result
// normalization and first-time quorum creation on ingestion.
func TestProbeProcessorProcessNormalizesResultAndCreatesQuorum(t *testing.T) {
	s := &fakeProbeStore{
		getCheckFn: func(id string) (*checks.Check, error) {
			check := checks.NewCheck(id, "http", "https://example.com", "", 0)
			return &check, nil
		},
	}

	runtime := monitoring.NewRuntime(nil, []string{"probe-1", "probe-2", "probe-3"})
	p := NewProbeProcessor(s, runtime)

	err := p.Process(&store.Probe{ProbeID: "probe-1"}, proto.CheckResult{
		CheckID: "site",
		Up:      true,
	})
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	if len(s.persistedWrites) != 1 {
		t.Fatalf("persisted writes = %d, want 1", len(s.persistedWrites))
	}

	journal := s.persistedWrites[0].JournalRecords
	if len(journal) != 1 {
		t.Fatalf("journal records = %d, want 1", len(journal))
	}
	if journal[0].Kind != string(monitoring.CheckTriggerObserveUp) {
		t.Fatalf("journal kind = %q, want %q", journal[0].Kind, monitoring.CheckTriggerObserveUp)
	}
	if journal[0].CheckID != "site" {
		t.Fatalf("journal CheckID = %q, want site", journal[0].CheckID)
	}
	if journal[0].ProbeID != "probe-1" {
		t.Fatalf("journal ProbeID = %q, want probe-1", journal[0].ProbeID)
	}
	if journal[0].OccurredAt.IsZero() {
		t.Fatal("expected journal OccurredAt to be set from normalized result timestamp")
	}
	if journal[0].ExpiresAt == nil || !journal[0].ExpiresAt.After(journal[0].OccurredAt) {
		t.Fatalf("ExpiresAt = %v, want future expiry", journal[0].ExpiresAt)
	}
	if s.persistedWrites[0].IncidentCheckID != "" {
		t.Fatalf("IncidentCheckID = %q, want empty", s.persistedWrites[0].IncidentCheckID)
	}

	quorum, err := runtime.QuorumSnapshot("site")
	if err != nil {
		t.Fatalf("QuorumSnapshot() error = %v", err)
	}
	if quorum.State != monitoring.QuorumStatePending {
		t.Fatalf("quorum state = %q, want %q", quorum.State, monitoring.QuorumStatePending)
	}
}

// TestProbeProcessorProcessOpensIncidentOnStableUpToDownTransition verifies
// durable incident opening on a stable runtime-owned outage transition.
func TestProbeProcessorProcessOpensIncidentOnStableUpToDownTransition(t *testing.T) {
	s := &fakeProbeStore{
		getCheckFn: func(id string) (*checks.Check, error) {
			check := checks.NewCheck(id, "http", "https://example.com", "https://hooks.example.com/wacht", 0)
			return &check, nil
		},
	}

	runtime := monitoring.NewRuntime(nil, []string{"probe-1", "probe-2"})
	p := NewProbeProcessor(s, runtime)

	steps := []struct {
		probeID string
		up      bool
	}{
		{probeID: "probe-1", up: true},
		{probeID: "probe-2", up: true},
		{probeID: "probe-1", up: true},
		{probeID: "probe-2", up: true},
		{probeID: "probe-1", up: false},
		{probeID: "probe-2", up: false},
		{probeID: "probe-1", up: false},
		{probeID: "probe-2", up: false},
	}
	processSequence(t, p, "site", steps)

	write := s.persistedWrites[len(s.persistedWrites)-1]
	if write.IncidentCheckID != "site" {
		t.Fatalf("IncidentCheckID = %q, want site", write.IncidentCheckID)
	}
	if write.ResolveIncident {
		t.Fatal("ResolveIncident = true, want false")
	}
	if write.IncidentNotification == nil {
		t.Fatal("expected durable down notification request")
	}
	if string(write.IncidentNotification.Payload) != `{"check_id":"site","target":"https://example.com","status":"down","probes_down":2,"probes_total":2}` {
		t.Fatalf("payload = %s", write.IncidentNotification.Payload)
	}

	quorum, err := runtime.QuorumSnapshot("site")
	if err != nil {
		t.Fatalf("QuorumSnapshot() error = %v", err)
	}
	if quorum.State != monitoring.QuorumStateDown {
		t.Fatalf("quorum state = %q, want %q", quorum.State, monitoring.QuorumStateDown)
	}
	if !quorum.IncidentOpen {
		t.Fatal("IncidentOpen = false, want true")
	}
}

// TestProbeProcessorProcessResolvesIncidentOnStableDownToUpTransition
// verifies durable incident resolution on a stable recovery transition.
func TestProbeProcessorProcessResolvesIncidentOnStableDownToUpTransition(t *testing.T) {
	s := &fakeProbeStore{
		getCheckFn: func(id string) (*checks.Check, error) {
			check := checks.NewCheck(id, "tcp", "db.example.com:5432", "https://hooks.example.com/wacht", 0)
			return &check, nil
		},
	}

	runtime := monitoring.NewRuntime(nil, []string{"probe-1", "probe-2"})
	p := NewProbeProcessor(s, runtime)

	steps := []struct {
		probeID string
		up      bool
	}{
		{probeID: "probe-1", up: true},
		{probeID: "probe-2", up: true},
		{probeID: "probe-1", up: true},
		{probeID: "probe-2", up: true},
		{probeID: "probe-1", up: false},
		{probeID: "probe-2", up: false},
		{probeID: "probe-1", up: false},
		{probeID: "probe-2", up: false},
		{probeID: "probe-1", up: true},
		{probeID: "probe-2", up: true},
		{probeID: "probe-1", up: true},
		{probeID: "probe-2", up: true},
	}
	processSequence(t, p, "db", steps)

	write := s.persistedWrites[len(s.persistedWrites)-1]
	if write.IncidentCheckID != "db" {
		t.Fatalf("IncidentCheckID = %q, want db", write.IncidentCheckID)
	}
	if !write.ResolveIncident {
		t.Fatal("ResolveIncident = false, want true")
	}
	if write.IncidentNotification == nil {
		t.Fatal("expected durable recovery notification request")
	}
	if string(write.IncidentNotification.Payload) != `{"check_id":"db","target":"db.example.com:5432","status":"up","probes_down":0,"probes_total":2}` {
		t.Fatalf("payload = %s", write.IncidentNotification.Payload)
	}

	quorum, err := runtime.QuorumSnapshot("db")
	if err != nil {
		t.Fatalf("QuorumSnapshot() error = %v", err)
	}
	if quorum.State != monitoring.QuorumStateUp {
		t.Fatalf("quorum state = %q, want %q", quorum.State, monitoring.QuorumStateUp)
	}
	if quorum.IncidentOpen {
		t.Fatal("IncidentOpen = true, want false")
	}
}

// TestProbeProcessorProcessRejectsInvalidProbeID verifies that authenticated
// probe identity cannot be overridden by request payloads.
func TestProbeProcessorProcessRejectsInvalidProbeID(t *testing.T) {
	p := NewProbeProcessor(&fakeProbeStore{}, monitoring.NewRuntime(nil, []string{"probe-1"}))

	err := p.Process(&store.Probe{ProbeID: "probe-1"}, proto.CheckResult{
		CheckID: "site",
		ProbeID: "probe-2",
	})
	var badRequest *badRequestError
	if !errors.As(err, &badRequest) {
		t.Fatalf("Process() error = %v, want badRequestError", err)
	}
	if badRequest.Error() != "probe_id does not match authenticated probe" {
		t.Fatalf("bad request = %q", badRequest.Error())
	}
}

// TestProbeProcessorProcessRejectsUnknownCheckID verifies that ingestion
// rejects results for checks missing from store metadata.
func TestProbeProcessorProcessRejectsUnknownCheckID(t *testing.T) {
	p := NewProbeProcessor(&fakeProbeStore{}, monitoring.NewRuntime(nil, []string{"probe-1"}))

	err := p.Process(&store.Probe{ProbeID: "probe-1"}, proto.CheckResult{
		CheckID: "missing",
	})
	var badRequest *badRequestError
	if !errors.As(err, &badRequest) {
		t.Fatalf("Process() error = %v, want badRequestError", err)
	}
	if badRequest.Error() != "unknown check_id" {
		t.Fatalf("bad request = %q", badRequest.Error())
	}
}

// TestProbeProcessorProcessPropagatesPersistError verifies that runtime
// persistence failures still surface to the caller.
func TestProbeProcessorProcessPropagatesPersistError(t *testing.T) {
	persistErr := errors.New("write failed")
	s := &fakeProbeStore{
		getCheckFn: func(id string) (*checks.Check, error) {
			check := checks.NewCheck(id, "http", "https://example.com", "", 0)
			return &check, nil
		},
		persistMonitoringWriteFn: func(write store.MonitoringWrite) (store.MonitoringWrite, bool, error) {
			return store.MonitoringWrite{}, false, persistErr
		},
	}

	runtime := monitoring.NewRuntime(nil, []string{"probe-1"})
	p := NewProbeProcessor(s, runtime)
	err := p.Process(&store.Probe{ProbeID: "probe-1"}, proto.CheckResult{
		CheckID: "site",
		Up:      true,
	})
	if !errors.Is(err, persistErr) {
		t.Fatalf("Process() error = %v, want %v", err, persistErr)
	}
	if len(s.persistedWrites) != 1 {
		t.Fatalf("persisted writes = %d, want 1 attempted persist", len(s.persistedWrites))
	}

	quorum, qErr := runtime.QuorumSnapshot("site")
	if qErr != nil {
		t.Fatalf("QuorumSnapshot() error = %v", qErr)
	}
	if quorum.State != monitoring.QuorumStatePending {
		t.Fatalf("quorum state = %q, want %q", quorum.State, monitoring.QuorumStatePending)
	}
}
