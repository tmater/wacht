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
	getCheckByIDFn           func(checkID string) (*checks.Check, error)
	persistMonitoringWriteFn func(write store.MonitoringWrite) (store.MonitoringWrite, error)
	persistMonitoringBatchFn func(writes []store.MonitoringWrite) ([]store.MonitoringWrite, error)
	registerProbeID          string
	registerVersion          string
	persistedWrites          []store.MonitoringWrite
	persistedBatches         [][]store.MonitoringWrite
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

// GetCheckByID returns stubbed check metadata for probe processor tests.
func (f *fakeProbeStore) GetCheckByID(checkID string) (*checks.Check, error) {
	if f.getCheckByIDFn != nil {
		return f.getCheckByIDFn(checkID)
	}
	return nil, nil
}

// PersistMonitoringWrite records runtime persistence writes for probe
// processor tests.
func (f *fakeProbeStore) PersistMonitoringWrite(write store.MonitoringWrite) (store.MonitoringWrite, error) {
	f.persistedWrites = append(f.persistedWrites, write)
	if f.persistMonitoringWriteFn != nil {
		return f.persistMonitoringWriteFn(write)
	}
	return write, nil
}

func (f *fakeProbeStore) PersistMonitoringBatch(writes []store.MonitoringWrite) ([]store.MonitoringWrite, error) {
	batch := append([]store.MonitoringWrite(nil), writes...)
	f.persistedBatches = append(f.persistedBatches, batch)
	f.persistedWrites = append(f.persistedWrites, batch...)
	if f.persistMonitoringBatchFn != nil {
		return f.persistMonitoringBatchFn(batch)
	}
	return batch, nil
}

// processSequence feeds a deterministic result stream through the probe
// processor so tests can focus on aggregate outcomes.
func processOne(t *testing.T, p *ProbeProcessor, probeID string, result proto.CheckResult) error {
	t.Helper()
	return p.ProcessBatch(&store.Probe{ProbeID: probeID}, []proto.CheckResult{result})
}

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
		if err := processOne(t, p, step.probeID, proto.CheckResult{
			CheckID: checkID,
			Up:      step.up,
			Error:   message,
		}); err != nil {
			t.Fatalf("Process(%s, up=%t) error = %v", step.probeID, step.up, err)
		}
	}
}

func testProbeCheck(checkID, name, checkType, target, webhook string, interval int) checks.Check {
	check := checks.NewCheck(name, checkType, target, webhook, interval)
	check.ID = checkID
	return check
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
	const checkID = "00000000-0000-0000-0000-000000000301"
	s := &fakeProbeStore{
		getCheckByIDFn: func(checkID string) (*checks.Check, error) {
			check := testProbeCheck(checkID, "site", "http", "https://example.com", "", 0)
			return &check, nil
		},
	}

	runtime := monitoring.NewRuntime(nil, []string{"probe-1", "probe-2", "probe-3"})
	p := NewProbeProcessor(s, runtime)

	err := processOne(t, p, "probe-1", proto.CheckResult{
		CheckID:   checkID,
		CheckName: "site",
		Up:        true,
	})
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	if len(s.persistedWrites) != 1 {
		t.Fatalf("persisted writes = %d, want 1", len(s.persistedWrites))
	}

	checkStates := s.persistedWrites[0].CheckStateWrites
	if len(checkStates) != 1 {
		t.Fatalf("check state writes = %d, want 1", len(checkStates))
	}
	if checkStates[0].CheckID != checkID {
		t.Fatalf("check state CheckID = %q, want %s", checkStates[0].CheckID, checkID)
	}
	if checkStates[0].ProbeID != "probe-1" {
		t.Fatalf("check state ProbeID = %q, want probe-1", checkStates[0].ProbeID)
	}
	if checkStates[0].LastResultAt.IsZero() {
		t.Fatal("expected LastResultAt to be set from normalized result timestamp")
	}
	if !checkStates[0].ExpiresAt.After(checkStates[0].LastResultAt) {
		t.Fatalf("ExpiresAt = %v, want future expiry after %v", checkStates[0].ExpiresAt, checkStates[0].LastResultAt)
	}
	if s.persistedWrites[0].IncidentCheckID != "" {
		t.Fatalf("IncidentCheckID = %q, want empty", s.persistedWrites[0].IncidentCheckID)
	}

	quorum, err := runtime.QuorumSnapshot(checkID)
	if err != nil {
		t.Fatalf("QuorumSnapshot() error = %v", err)
	}
	if quorum.State != monitoring.QuorumStatePending {
		t.Fatalf("quorum state = %q, want %q", quorum.State, monitoring.QuorumStatePending)
	}
}

func TestProbeProcessorProcessBatchRejectsEmptyBatch(t *testing.T) {
	p := NewProbeProcessor(&fakeProbeStore{}, monitoring.NewRuntime(nil, []string{"probe-1"}))

	err := p.ProcessBatch(&store.Probe{ProbeID: "probe-1"}, nil)
	var badReq *badRequestError
	if !errors.As(err, &badReq) {
		t.Fatalf("ProcessBatch() error = %v, want badRequestError", err)
	}
	if badReq.Error() != "results is required" {
		t.Fatalf("bad request message = %q, want results is required", badReq.Error())
	}
}

func TestProbeProcessorProcessBatchUsesSingleStoreBatch(t *testing.T) {
	s := &fakeProbeStore{
		getCheckByIDFn: func(checkID string) (*checks.Check, error) {
			if checkID == "not-a-uuid" {
				return nil, store.ErrInvalidCheckID
			}
			check := testProbeCheck(checkID, "site", "http", "https://example.com", "", 30)
			return &check, nil
		},
	}

	runtime := monitoring.NewRuntime(nil, []string{"probe-1", "probe-2"})
	p := NewProbeProcessor(s, runtime)

	err := p.ProcessBatch(&store.Probe{ProbeID: "probe-1"}, []proto.CheckResult{
		{CheckID: "00000000-0000-0000-0000-000000000302", CheckName: "site-a", Up: true},
		{CheckID: "not-a-uuid", CheckName: "stale", Up: false, Error: "timeout"},
		{CheckID: "00000000-0000-0000-0000-000000000303", CheckName: "site-b", Up: false, Error: "timeout"},
	})
	if err != nil {
		t.Fatalf("ProcessBatch() error = %v", err)
	}
	if len(s.persistedBatches) != 1 {
		t.Fatalf("persisted batches = %d, want 1", len(s.persistedBatches))
	}
	if len(s.persistedBatches[0]) != 2 {
		t.Fatalf("batch writes = %d, want 2", len(s.persistedBatches[0]))
	}
}

// TestProbeProcessorProcessOpensIncidentOnStableUpToDownTransition verifies
// durable incident opening on a stable runtime-owned outage transition.
func TestProbeProcessorProcessOpensIncidentOnStableUpToDownTransition(t *testing.T) {
	const checkID = "00000000-0000-0000-0000-000000000304"
	s := &fakeProbeStore{
		getCheckByIDFn: func(checkID string) (*checks.Check, error) {
			check := testProbeCheck(checkID, "site", "http", "https://example.com", "https://hooks.example.com/wacht", 0)
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
		{probeID: "probe-1", up: false},
	}
	processSequence(t, p, checkID, steps)

	write := s.persistedWrites[len(s.persistedWrites)-1]
	if write.IncidentCheckID != checkID {
		t.Fatalf("IncidentCheckID = %q, want %s", write.IncidentCheckID, checkID)
	}
	if write.ResolveIncident {
		t.Fatal("ResolveIncident = true, want false")
	}
	if write.IncidentNotification == nil {
		t.Fatal("expected durable down notification request")
	}
	if string(write.IncidentNotification.Payload) != `{"check_id":"00000000-0000-0000-0000-000000000304","check_name":"site","target":"https://example.com","status":"down","probes_down":2,"probes_total":2}` {
		t.Fatalf("payload = %s", write.IncidentNotification.Payload)
	}

	quorum, err := runtime.QuorumSnapshot(checkID)
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
	const checkID = "00000000-0000-0000-0000-000000000305"
	s := &fakeProbeStore{
		getCheckByIDFn: func(checkID string) (*checks.Check, error) {
			check := testProbeCheck(checkID, "db", "tcp", "db.example.com:5432", "https://hooks.example.com/wacht", 0)
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
		{probeID: "probe-1", up: false},
		{probeID: "probe-1", up: true},
		{probeID: "probe-2", up: true},
		{probeID: "probe-1", up: true},
		{probeID: "probe-2", up: true},
		{probeID: "probe-1", up: true},
	}
	processSequence(t, p, checkID, steps)

	write := s.persistedWrites[len(s.persistedWrites)-1]
	if write.IncidentCheckID != checkID {
		t.Fatalf("IncidentCheckID = %q, want %s", write.IncidentCheckID, checkID)
	}
	if !write.ResolveIncident {
		t.Fatal("ResolveIncident = false, want true")
	}
	if write.IncidentNotification == nil {
		t.Fatal("expected durable recovery notification request")
	}
	if string(write.IncidentNotification.Payload) != `{"check_id":"00000000-0000-0000-0000-000000000305","check_name":"db","target":"db.example.com:5432","status":"up","probes_down":0,"probes_total":2}` {
		t.Fatalf("payload = %s", write.IncidentNotification.Payload)
	}

	quorum, err := runtime.QuorumSnapshot(checkID)
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

	err := processOne(t, p, "probe-1", proto.CheckResult{
		CheckID:   "00000000-0000-0000-0000-000000000307",
		CheckName: "site",
		ProbeID:   "probe-2",
	})
	var badRequest *badRequestError
	if !errors.As(err, &badRequest) {
		t.Fatalf("Process() error = %v, want badRequestError", err)
	}
	if badRequest.Error() != "probe_id does not match authenticated probe" {
		t.Fatalf("bad request = %q", badRequest.Error())
	}
}

func TestProbeProcessorProcessIgnoresMalformedCheckID(t *testing.T) {
	s := &fakeProbeStore{
		getCheckByIDFn: func(checkID string) (*checks.Check, error) {
			return nil, store.ErrInvalidCheckID
		},
	}
	p := NewProbeProcessor(s, monitoring.NewRuntime(nil, []string{"probe-1"}))

	err := processOne(t, p, "probe-1", proto.CheckResult{
		CheckID:   "missing",
		CheckName: "site",
	})
	if err != nil {
		t.Fatalf("Process() error = %v, want nil", err)
	}
	if len(s.persistedWrites) != 0 {
		t.Fatalf("persisted writes = %d, want 0", len(s.persistedWrites))
	}
}

// TestProbeProcessorProcessIgnoresUnknownCheckID verifies that stale probe
// results for deleted checks are dropped instead of poisoning later batches.
func TestProbeProcessorProcessIgnoresUnknownCheckID(t *testing.T) {
	p := NewProbeProcessor(&fakeProbeStore{}, monitoring.NewRuntime(nil, []string{"probe-1"}))

	err := processOne(t, p, "probe-1", proto.CheckResult{
		CheckID:   "00000000-0000-0000-0000-000000999999",
		CheckName: "site",
	})
	if err != nil {
		t.Fatalf("Process() error = %v, want nil", err)
	}
}

func TestProbeProcessorProcessIgnoresMissingCheckID(t *testing.T) {
	s := &fakeProbeStore{}
	p := NewProbeProcessor(s, monitoring.NewRuntime(nil, []string{"probe-1"}))

	err := processOne(t, p, "probe-1", proto.CheckResult{
		CheckName: "site",
	})
	if err != nil {
		t.Fatalf("Process() error = %v, want nil", err)
	}
	if len(s.persistedWrites) != 0 {
		t.Fatalf("persisted writes = %d, want 0", len(s.persistedWrites))
	}
}

// TestProbeProcessorProcessPropagatesPersistError verifies that runtime
// persistence failures still surface to the caller.
func TestProbeProcessorProcessPropagatesPersistError(t *testing.T) {
	persistErr := errors.New("write failed")
	const checkID = "00000000-0000-0000-0000-000000000306"
	s := &fakeProbeStore{
		getCheckByIDFn: func(checkID string) (*checks.Check, error) {
			check := testProbeCheck(checkID, "site", "http", "https://example.com", "", 0)
			return &check, nil
		},
		persistMonitoringBatchFn: func(writes []store.MonitoringWrite) ([]store.MonitoringWrite, error) {
			return nil, persistErr
		},
	}

	runtime := monitoring.NewRuntime(nil, []string{"probe-1"})
	p := NewProbeProcessor(s, runtime)
	err := processOne(t, p, "probe-1", proto.CheckResult{
		CheckID:   checkID,
		CheckName: "site",
		Up:        true,
	})
	if !errors.Is(err, persistErr) {
		t.Fatalf("Process() error = %v, want %v", err, persistErr)
	}
	if len(s.persistedWrites) != 1 {
		t.Fatalf("persisted writes = %d, want 1 attempted persist", len(s.persistedWrites))
	}

	if _, qErr := runtime.QuorumSnapshot(checkID); !errors.Is(qErr, monitoring.ErrUnknownCheck) {
		t.Fatalf("QuorumSnapshot() error = %v, want %v", qErr, monitoring.ErrUnknownCheck)
	}
}
