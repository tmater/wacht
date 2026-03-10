package server

import (
	"errors"
	"testing"
	"time"

	"github.com/tmater/wacht/internal/checks"
	"github.com/tmater/wacht/internal/proto"
	"github.com/tmater/wacht/internal/store"
)

type fakeResultStore struct {
	getCheckFn              func(id string) (*checks.Check, error)
	saveResultFn            func(r proto.CheckResult) error
	recentResultsPerProbeFn func(checkID string) ([]proto.CheckResult, error)
	recentResultsByProbeFn  func(checkID, probeID string, n int) ([]proto.CheckResult, error)
	openIncidentFn          func(checkID string) (bool, error)
	resolveIncidentFn       func(checkID string) (bool, error)
	savedResults            []proto.CheckResult
	openIncidentCalls       int
	resolveIncidentCalls    int
}

func (f *fakeResultStore) GetCheck(id string) (*checks.Check, error) {
	if f.getCheckFn != nil {
		return f.getCheckFn(id)
	}
	return nil, nil
}

func (f *fakeResultStore) SaveResult(r proto.CheckResult) error {
	f.savedResults = append(f.savedResults, r)
	if f.saveResultFn != nil {
		return f.saveResultFn(r)
	}
	return nil
}

func (f *fakeResultStore) RecentResultsPerProbe(checkID string) ([]proto.CheckResult, error) {
	if f.recentResultsPerProbeFn != nil {
		return f.recentResultsPerProbeFn(checkID)
	}
	return nil, nil
}

func (f *fakeResultStore) RecentResultsByProbe(checkID, probeID string, n int) ([]proto.CheckResult, error) {
	if f.recentResultsByProbeFn != nil {
		return f.recentResultsByProbeFn(checkID, probeID, n)
	}
	return nil, nil
}

func (f *fakeResultStore) OpenIncident(checkID string) (bool, error) {
	f.openIncidentCalls++
	if f.openIncidentFn != nil {
		return f.openIncidentFn(checkID)
	}
	return false, nil
}

func (f *fakeResultStore) ResolveIncident(checkID string) (bool, error) {
	f.resolveIncidentCalls++
	if f.resolveIncidentFn != nil {
		return f.resolveIncidentFn(checkID)
	}
	return false, nil
}

func TestProbeResultProcessorProcessOpensIncidentAndReturnsAlert(t *testing.T) {
	s := &fakeResultStore{
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

	p := NewProbeResultProcessor(s)
	outcome, err := p.Process(&store.Probe{ProbeID: "probe-1"}, proto.CheckResult{
		CheckID: "site",
		Up:      false,
	})
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	if outcome.Alert == nil {
		t.Fatalf("Process() returned no alert")
	}
	if outcome.WebhookURL != "https://hooks.example.com/wacht" {
		t.Fatalf("WebhookURL = %q, want webhook URL", outcome.WebhookURL)
	}
	if outcome.Alert.Status != "down" {
		t.Fatalf("alert status = %q, want down", outcome.Alert.Status)
	}
	if outcome.Alert.ProbesDown != 2 || outcome.Alert.ProbesTotal != 3 {
		t.Fatalf("alert counts = %d/%d, want 2/3", outcome.Alert.ProbesDown, outcome.Alert.ProbesTotal)
	}
	if len(s.savedResults) != 1 {
		t.Fatalf("saved results = %d, want 1", len(s.savedResults))
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

func TestProbeResultProcessorProcessResolvesIncidentAndReturnsAlert(t *testing.T) {
	s := &fakeResultStore{
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
		resolveIncidentFn: func(checkID string) (bool, error) {
			return true, nil
		},
	}

	p := NewProbeResultProcessor(s)
	outcome, err := p.Process(&store.Probe{ProbeID: "probe-2"}, proto.CheckResult{
		CheckID: "db",
		Up:      true,
	})
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	if outcome.Alert == nil {
		t.Fatalf("Process() returned no recovery alert")
	}
	if outcome.Alert.Status != "up" {
		t.Fatalf("alert status = %q, want up", outcome.Alert.Status)
	}
	if outcome.Alert.ProbesDown != 1 || outcome.Alert.ProbesTotal != 3 {
		t.Fatalf("alert counts = %d/%d, want 1/3", outcome.Alert.ProbesDown, outcome.Alert.ProbesTotal)
	}
}

func TestProbeResultProcessorProcessReturnsNoAlertWhenIncidentAlreadyOpen(t *testing.T) {
	s := &fakeResultStore{
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
		openIncidentFn: func(checkID string) (bool, error) {
			return true, nil
		},
	}

	p := NewProbeResultProcessor(s)
	outcome, err := p.Process(&store.Probe{ProbeID: "probe-1"}, proto.CheckResult{
		CheckID: "site",
		Up:      false,
	})
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	if outcome.Alert != nil {
		t.Fatalf("Process() alert = %#v, want nil", outcome.Alert)
	}
	if s.openIncidentCalls != 1 {
		t.Fatalf("OpenIncident calls = %d, want 1", s.openIncidentCalls)
	}
}

func TestProbeResultProcessorProcessIgnoresQuorumQueryErrorAfterSave(t *testing.T) {
	s := &fakeResultStore{
		getCheckFn: func(id string) (*checks.Check, error) {
			check := checks.NewCheck(id, "dns", "example.com", "", 0)
			return &check, nil
		},
		recentResultsPerProbeFn: func(checkID string) ([]proto.CheckResult, error) {
			return nil, errors.New("db unavailable")
		},
	}

	p := NewProbeResultProcessor(s)
	outcome, err := p.Process(&store.Probe{ProbeID: "probe-3"}, proto.CheckResult{
		CheckID: "dns-check",
		Up:      true,
	})
	if err != nil {
		t.Fatalf("Process() error = %v, want nil", err)
	}
	if outcome.Alert != nil {
		t.Fatalf("Process() alert = %#v, want nil", outcome.Alert)
	}
	if len(s.savedResults) != 1 {
		t.Fatalf("saved results = %d, want 1", len(s.savedResults))
	}
}

func TestProbeResultProcessorProcessRejectsInvalidProbeID(t *testing.T) {
	p := NewProbeResultProcessor(&fakeResultStore{})

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

func TestProbeResultProcessorProcessRejectsUnknownCheckID(t *testing.T) {
	p := NewProbeResultProcessor(&fakeResultStore{})

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

func TestProbeResultProcessorProcessPropagatesSaveError(t *testing.T) {
	s := &fakeResultStore{
		getCheckFn: func(id string) (*checks.Check, error) {
			check := checks.NewCheck(id, "http", "https://example.com", "", 0)
			return &check, nil
		},
		saveResultFn: func(r proto.CheckResult) error {
			return errors.New("write failed")
		},
	}

	p := NewProbeResultProcessor(s)
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

func TestProbeResultProcessorProcessDoesNotOpenIncidentWithoutConsecutiveFailures(t *testing.T) {
	s := &fakeResultStore{
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

	p := NewProbeResultProcessor(s)
	outcome, err := p.Process(&store.Probe{ProbeID: "probe-1"}, proto.CheckResult{
		CheckID: "site",
		Up:      false,
	})
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	if outcome.Alert != nil {
		t.Fatalf("Process() alert = %#v, want nil", outcome.Alert)
	}
	if s.openIncidentCalls != 0 {
		t.Fatalf("OpenIncident calls = %d, want 0", s.openIncidentCalls)
	}
}

func TestProbeResultProcessorProcessOpensIncidentWithoutWebhookAlert(t *testing.T) {
	s := &fakeResultStore{
		getCheckFn: func(id string) (*checks.Check, error) {
			check := checks.NewCheck(id, "http", "https://example.com", "", 0)
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

	p := NewProbeResultProcessor(s)
	outcome, err := p.Process(&store.Probe{ProbeID: "probe-1"}, proto.CheckResult{
		CheckID: "site",
		Up:      false,
	})
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	if outcome.Alert != nil {
		t.Fatalf("Process() alert = %#v, want nil", outcome.Alert)
	}
	if s.openIncidentCalls != 1 {
		t.Fatalf("OpenIncident calls = %d, want 1", s.openIncidentCalls)
	}
}
