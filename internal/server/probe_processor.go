package server

import (
	"fmt"
	"log/slog"
	"time"

	probeapi "github.com/tmater/wacht/internal/api/probe"
	"github.com/tmater/wacht/internal/checks"
	"github.com/tmater/wacht/internal/monitoring"
	"github.com/tmater/wacht/internal/proto"
	"github.com/tmater/wacht/internal/store"
)

type probeStore interface {
	RegisterProbe(probeID, version string) error
	UpdateProbeHeartbeat(probeID string) error
	GetCheck(id string) (*checks.Check, error)
	SaveResult(r proto.CheckResult) error
	PersistMonitoringWrite(write store.MonitoringWrite) (store.MonitoringWrite, bool, error)
}

// ProbeResultOutcome is the normalized result of processing one probe result.
type ProbeResultOutcome struct{}

type probeProcessor interface {
	Heartbeat(probe *store.Probe, req probeapi.HeartbeatRequest) error
	Register(probe *store.Probe, req probeapi.RegisterRequest) error
	Process(probe *store.Probe, incoming proto.CheckResult) (ProbeResultOutcome, error)
}

type ProbeProcessor struct {
	store   probeStore
	runtime *monitoring.Runtime
}

func NewProbeProcessor(store probeStore, runtime *monitoring.Runtime) *ProbeProcessor {
	return &ProbeProcessor{store: store, runtime: runtime}
}

func (p *ProbeProcessor) Heartbeat(probe *store.Probe, req probeapi.HeartbeatRequest) error {
	if probe == nil {
		return fmt.Errorf("probe is required")
	}
	if req.ProbeID != "" && req.ProbeID != probe.ProbeID {
		return &badRequestError{message: "probe_id does not match authenticated probe"}
	}
	return p.store.UpdateProbeHeartbeat(probe.ProbeID)
}

func (p *ProbeProcessor) Register(probe *store.Probe, req probeapi.RegisterRequest) error {
	if probe == nil {
		return fmt.Errorf("probe is required")
	}
	if req.ProbeID != "" && req.ProbeID != probe.ProbeID {
		return &badRequestError{message: "probe_id does not match authenticated probe"}
	}
	return p.store.RegisterProbe(probe.ProbeID, req.Version)
}

func (p *ProbeProcessor) Process(probe *store.Probe, incoming proto.CheckResult) (ProbeResultOutcome, error) {
	if probe == nil {
		return ProbeResultOutcome{}, fmt.Errorf("probe is required")
	}

	check, result, err := p.normalize(probe, incoming)
	if err != nil {
		return ProbeResultOutcome{}, err
	}

	slog.Default().Debug("probe result received", "component", "probe", "check_id", result.CheckID, "probe_id", result.ProbeID, "up", result.Up)

	if err := monitoring.ApplyResult(p.runtime, p.store, *check, result); err != nil {
		return ProbeResultOutcome{}, fmt.Errorf("apply result: %w", err)
	}
	return ProbeResultOutcome{}, nil
}

func (p *ProbeProcessor) normalize(probe *store.Probe, incoming proto.CheckResult) (*checks.Check, proto.CheckResult, error) {
	if incoming.ProbeID != "" && incoming.ProbeID != probe.ProbeID {
		return nil, proto.CheckResult{}, &badRequestError{message: "probe_id does not match authenticated probe"}
	}

	check, err := p.store.GetCheck(incoming.CheckID)
	if err != nil {
		return nil, proto.CheckResult{}, fmt.Errorf("look up check %q: %w", incoming.CheckID, err)
	}
	if check == nil {
		return nil, proto.CheckResult{}, &badRequestError{message: "unknown check_id"}
	}

	result := incoming
	result.ProbeID = probe.ProbeID
	result.Type = string(check.Type)
	result.Target = check.Target
	result.Timestamp = time.Now().UTC()
	return check, result, nil
}
