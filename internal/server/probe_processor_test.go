package server

import (
	"errors"
	"testing"
	"time"

	probeapi "github.com/tmater/wacht/internal/api/probe"
	"github.com/tmater/wacht/internal/checks"
	"github.com/tmater/wacht/internal/proto"
	"github.com/tmater/wacht/internal/store"
)

type fakeProbeStore struct {
	registerProbeFn         func(probeID, version string) error
	updateProbeHeartbeatFn  func(probeID string) error
	getCheckFn              func(id string) (*checks.Check, error)
	saveResultFn            func(r proto.CheckResult) error
	recentResultsPerProbeFn func(checkID string) ([]proto.CheckResult, error)
	recentResultsByProbeFn  func(checkID, probeID string, n int) ([]proto.CheckResult, error)
	openIncidentFn          func(checkID string, request *store.NotificationRequest) (bool, error)
	resolveIncidentFn       func(checkID string, request *store.NotificationRequest) (bool, error)
	registerProbeID         string
	registerVersion         string
	heartbeatProbeID        string
	savedResults            []proto.CheckResult
	openIncidentCalls       int
	resolveIncidentCalls    int
	lastOpenNotification    *store.NotificationRequest
	lastResolveNotification *store.NotificationRequest
}

func (f *fakeProbeStore) RegisterProbe(probeID, version string) error {
	f.registerProbeID = probeID
	f.registerVersion = version
	if f.registerProbeFn != nil {
		return f.registerProbeFn(probeID, version)
	}
	return nil
}

func (f *fakeProbeStore) UpdateProbeHeartbeat(probeID string) error {
	f.heartbeatProbeID = probeID
	if f.updateProbeHeartbeatFn != nil {
		return f.updateProbeHeartbeatFn(probeID)
	}
	return nil
}

func (f *fakeProbeStore) GetCheck(id string) (*checks.Check, error) {
	if f.getCheckFn != nil {
		return f.getCheckFn(id)
	}
	return nil, nil
}

func (f *fakeProbeStore) SaveResult(r proto.CheckResult) error {
	f.savedResults = append(f.savedResults, r)
	if f.saveResultFn != nil {
		return f.saveResultFn(r)
	}
	return nil
}

func (f *fakeProbeStore) RecentResultsPerProbe(checkID string) ([]proto.CheckResult, error) {
	if f.recentResultsPerProbeFn != nil {
		return f.recentResultsPerProbeFn(checkID)
	}
	return nil, nil
}

func (f *fakeProbeStore) RecentResultsByProbe(checkID, probeID string, n int) ([]proto.CheckResult, error) {
	if f.recentResultsByProbeFn != nil {
		return f.recentResultsByProbeFn(checkID, probeID, n)
	}
	return nil, nil
}

func (f *fakeProbeStore) OpenIncidentWithNotification(checkID string, request *store.NotificationRequest) (bool, error) {
	f.openIncidentCalls++
	f.lastOpenNotification = request
	if f.openIncidentFn != nil {
		return f.openIncidentFn(checkID, request)
	}
	return false, nil
}

func (f *fakeProbeStore) ResolveIncidentWithNotification(checkID string, request *store.NotificationRequest) (bool, error) {
	f.resolveIncidentCalls++
	f.lastResolveNotification = request
	if f.resolveIncidentFn != nil {
		return f.resolveIncidentFn(checkID, request)
	}
	return false, nil
}

func TestProbeProcessorHeartbeatUpdatesAuthenticatedProbe(t *testing.T) {
	s := &fakeProbeStore{}
	p := NewProbeProcessor(s)

	err := p.Heartbeat(&store.Probe{ProbeID: "probe-1"}, probeapi.HeartbeatRequest{})
	if err != nil {
		t.Fatalf("Heartbeat() error = %v", err)
	}
	if s.heartbeatProbeID != "probe-1" {
		t.Fatalf("UpdateProbeHeartbeat probeID = %q, want probe-1", s.heartbeatProbeID)
	}
}

func TestProbeProcessorRegisterRecordsVersion(t *testing.T) {
	s := &fakeProbeStore{}
	p := NewProbeProcessor(s)

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

func TestProbeProcessorProcessOpensIncidentAndCreatesNotification(t *testing.T) {
	s := &fakeProbeStore{
		getCheckFn: func(id string) (*checks.Check, error) {
			check := checks.NewCheck(id, "http", "https://example.com", "https://hooks.example.com/wacht", 0)
			return &check, nil
		},
		recentResultsPerProbeFn: func(checkID string) ([]proto.CheckResult, error) {
			return []proto.CheckResult{
				{ProbeID: "probe-1", Up: false},
				{ProbeID: "probe-2", Up: false},
				{ProbeID: "probe-3", Up: true},
			}, nil
		},
		recentResultsByProbeFn: func(checkID, probeID string, n int) ([]proto.CheckResult, error) {
			return []proto.CheckResult{
				{ProbeID: probeID, Up: false},
				{ProbeID: probeID, Up: false},
			}, nil
		},
	}

	p := NewProbeProcessor(s)
	outcome, err := p.Process(&store.Probe{ProbeID: "probe-1"}, proto.CheckResult{
		CheckID: "site",
		Up:      false,
	})
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	if outcome != (ProbeResultOutcome{}) {
		t.Fatalf("Process() outcome = %#v, want empty outcome", outcome)
	}
	if len(s.savedResults) != 1 {
		t.Fatalf("saved results = %d, want 1", len(s.savedResults))
	}
	if s.lastOpenNotification == nil {
		t.Fatal("expected durable down notification request")
	}
	if s.lastOpenNotification.WebhookURL != "https://hooks.example.com/wacht" {
		t.Fatalf("WebhookURL = %q, want webhook URL", s.lastOpenNotification.WebhookURL)
	}
	if string(s.lastOpenNotification.Payload) != `{"check_id":"site","target":"https://example.com","status":"down","probes_down":2,"probes_total":3}` {
		t.Fatalf("payload = %s", s.lastOpenNotification.Payload)
	}

	saved := s.savedResults[0]
	if saved.ProbeID != "probe-1" {
		t.Fatalf("saved ProbeID = %q, want probe-1", saved.ProbeID)
	}
	if saved.Type != string(checks.CheckHTTP) {
		t.Fatalf("saved Type = %q, want %q", saved.Type, checks.CheckHTTP)
	}
	if saved.Target != "https://example.com" {
		t.Fatalf("saved Target = %q, want normalized target", saved.Target)
	}
	if saved.Timestamp.IsZero() {
		t.Fatalf("saved Timestamp should be set")
	}
	if time.Since(saved.Timestamp) > time.Minute {
		t.Fatalf("saved Timestamp = %s, want recent timestamp", saved.Timestamp)
	}
}

func TestProbeProcessorProcessResolvesIncidentAndCreatesNotification(t *testing.T) {
	s := &fakeProbeStore{
		getCheckFn: func(id string) (*checks.Check, error) {
			check := checks.NewCheck(id, "tcp", "db.example.com:5432", "https://hooks.example.com/wacht", 0)
			return &check, nil
		},
		recentResultsPerProbeFn: func(checkID string) ([]proto.CheckResult, error) {
			return []proto.CheckResult{
				{ProbeID: "probe-1", Up: true},
				{ProbeID: "probe-2", Up: true},
				{ProbeID: "probe-3", Up: false},
			}, nil
		},
		recentResultsByProbeFn: func(checkID, probeID string, n int) ([]proto.CheckResult, error) {
			if probeID == "probe-3" {
				t.Fatalf("RecentResultsByProbe should not be called for down probe %s", probeID)
			}
			return []proto.CheckResult{
				{ProbeID: probeID, Up: true},
				{ProbeID: probeID, Up: true},
			}, nil
		},
		resolveIncidentFn: func(checkID string, request *store.NotificationRequest) (bool, error) {
			return true, nil
		},
	}

	p := NewProbeProcessor(s)
	outcome, err := p.Process(&store.Probe{ProbeID: "probe-2"}, proto.CheckResult{
		CheckID: "db",
		Up:      true,
	})
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	if outcome != (ProbeResultOutcome{}) {
		t.Fatalf("Process() outcome = %#v, want empty outcome", outcome)
	}
	if s.lastResolveNotification == nil {
		t.Fatal("expected durable recovery notification request")
	}
	if string(s.lastResolveNotification.Payload) != `{"check_id":"db","target":"db.example.com:5432","status":"up","probes_down":1,"probes_total":3}` {
		t.Fatalf("payload = %s", s.lastResolveNotification.Payload)
	}
}

func TestProbeProcessorProcessDoesNotResolveIncidentWithoutConsecutiveSuccesses(t *testing.T) {
	s := &fakeProbeStore{
		getCheckFn: func(id string) (*checks.Check, error) {
			check := checks.NewCheck(id, "tcp", "db.example.com:5432", "https://hooks.example.com/wacht", 0)
			return &check, nil
		},
		recentResultsPerProbeFn: func(checkID string) ([]proto.CheckResult, error) {
			return []proto.CheckResult{
				{ProbeID: "probe-1", Up: true},
				{ProbeID: "probe-2", Up: true},
				{ProbeID: "probe-3", Up: false},
			}, nil
		},
		recentResultsByProbeFn: func(checkID, probeID string, n int) ([]proto.CheckResult, error) {
			return []proto.CheckResult{
				{ProbeID: probeID, Up: true},
			}, nil
		},
	}

	p := NewProbeProcessor(s)
	outcome, err := p.Process(&store.Probe{ProbeID: "probe-2"}, proto.CheckResult{
		CheckID: "db",
		Up:      true,
	})
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	if outcome != (ProbeResultOutcome{}) {
		t.Fatalf("Process() returned outcome = %#v, want empty outcome", outcome)
	}
	if s.resolveIncidentCalls != 0 {
		t.Fatalf("ResolveIncident calls = %d, want 0", s.resolveIncidentCalls)
	}
}

func TestProbeProcessorProcessRejectsInvalidProbeID(t *testing.T) {
	p := NewProbeProcessor(&fakeProbeStore{})

	_, err := p.Process(&store.Probe{ProbeID: "probe-1"}, proto.CheckResult{
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

func TestProbeProcessorProcessRejectsUnknownCheckID(t *testing.T) {
	p := NewProbeProcessor(&fakeProbeStore{})

	_, err := p.Process(&store.Probe{ProbeID: "probe-1"}, proto.CheckResult{
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

func TestProbeProcessorProcessPropagatesSaveError(t *testing.T) {
	s := &fakeProbeStore{
		getCheckFn: func(id string) (*checks.Check, error) {
			check := checks.NewCheck(id, "http", "https://example.com", "", 0)
			return &check, nil
		},
		saveResultFn: func(r proto.CheckResult) error {
			return errors.New("write failed")
		},
	}

	p := NewProbeProcessor(s)
	_, err := p.Process(&store.Probe{ProbeID: "probe-1"}, proto.CheckResult{
		CheckID: "site",
		Up:      true,
	})
	if err == nil {
		t.Fatalf("Process() error = nil, want save error")
	}
	if len(s.savedResults) != 1 {
		t.Fatalf("saved results = %d, want 1 attempted save", len(s.savedResults))
	}
}

func TestProbeProcessorProcessDoesNotOpenIncidentWithoutConsecutiveFailures(t *testing.T) {
	s := &fakeProbeStore{
		getCheckFn: func(id string) (*checks.Check, error) {
			check := checks.NewCheck(id, "http", "https://example.com", "https://hooks.example.com/wacht", 0)
			return &check, nil
		},
		recentResultsPerProbeFn: func(checkID string) ([]proto.CheckResult, error) {
			return []proto.CheckResult{
				{ProbeID: "probe-1", Up: false},
				{ProbeID: "probe-2", Up: false},
				{ProbeID: "probe-3", Up: true},
			}, nil
		},
		recentResultsByProbeFn: func(checkID, probeID string, n int) ([]proto.CheckResult, error) {
			return []proto.CheckResult{
				{ProbeID: probeID, Up: false},
			}, nil
		},
	}

	p := NewProbeProcessor(s)
	outcome, err := p.Process(&store.Probe{ProbeID: "probe-1"}, proto.CheckResult{
		CheckID: "site",
		Up:      false,
	})
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	if outcome != (ProbeResultOutcome{}) {
		t.Fatalf("Process() outcome = %#v, want empty outcome", outcome)
	}
	if s.openIncidentCalls != 0 {
		t.Fatalf("OpenIncident calls = %d, want 0", s.openIncidentCalls)
	}
}
