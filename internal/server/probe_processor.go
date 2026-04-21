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
	GetCheck(id string) (*checks.Check, error)
	PersistMonitoringWrite(write store.MonitoringWrite) (store.MonitoringWrite, error)
	PersistMonitoringBatch(writes []store.MonitoringWrite) ([]store.MonitoringWrite, error)
}

type probeProcessor interface {
	Heartbeat(probe *store.Probe, req probeapi.HeartbeatRequest) error
	Register(probe *store.Probe, req probeapi.RegisterRequest) error
	Process(probe *store.Probe, incoming proto.CheckResult) error
	ProcessBatch(probe *store.Probe, incoming []proto.CheckResult) error
}

type ProbeProcessor struct {
	store   probeStore
	runtime *monitoring.Runtime
}

// NewProbeProcessor builds the probe ingress adapter around store and runtime
// dependencies.
func NewProbeProcessor(store probeStore, runtime *monitoring.Runtime) *ProbeProcessor {
	return &ProbeProcessor{store: store, runtime: runtime}
}

// Heartbeat validates the authenticated probe heartbeat request and delegates
// the liveness update to the monitoring runtime.
func (p *ProbeProcessor) Heartbeat(probe *store.Probe, req probeapi.HeartbeatRequest) error {
	if probe == nil {
		return fmt.Errorf("probe is required")
	}
	if req.ProbeID != "" && req.ProbeID != probe.ProbeID {
		return &badRequestError{message: "probe_id does not match authenticated probe"}
	}
	if err := monitoring.ApplyHeartbeat(p.runtime, p.store, probe.ProbeID, time.Now().UTC()); err != nil {
		return fmt.Errorf("apply heartbeat: %w", err)
	}
	return nil
}

// Register records authenticated probe startup metadata.
func (p *ProbeProcessor) Register(probe *store.Probe, req probeapi.RegisterRequest) error {
	if probe == nil {
		return fmt.Errorf("probe is required")
	}
	if req.ProbeID != "" && req.ProbeID != probe.ProbeID {
		return &badRequestError{message: "probe_id does not match authenticated probe"}
	}
	return p.store.RegisterProbe(probe.ProbeID, req.Version)
}

// Process validates and normalizes one probe result before handing it off to
// runtime-owned monitoring logic.
func (p *ProbeProcessor) Process(probe *store.Probe, incoming proto.CheckResult) error {
	return p.ProcessBatch(probe, []proto.CheckResult{incoming})
}

// ProcessBatch validates and normalizes one flushed probe result batch before
// handing the accepted results off to runtime-owned monitoring logic.
func (p *ProbeProcessor) ProcessBatch(probe *store.Probe, incoming []proto.CheckResult) error {
	if probe == nil {
		return fmt.Errorf("probe is required")
	}
	if len(incoming) == 0 {
		return &badRequestError{message: "results is required"}
	}

	normalized, err := p.normalizeBatch(probe, incoming)
	if err != nil {
		return err
	}

	for _, item := range normalized {
		slog.Default().Debug("probe result received", "component", "probe", "check_id", item.result.CheckID, "probe_id", item.result.ProbeID, "up", item.result.Up)
	}

	observed := make([]monitoring.ObservedResult, 0, len(normalized))
	for _, item := range normalized {
		observed = append(observed, monitoring.ObservedResult{
			Check:  *item.check,
			Result: item.result,
		})
	}
	if err := monitoring.ApplyResultBatch(p.runtime, p.store, observed); err != nil {
		return fmt.Errorf("apply result batch: %w", err)
	}
	return nil
}

type normalizedResult struct {
	check  *checks.Check
	result proto.CheckResult
}

// normalize resolves check metadata and stamps the accepted probe result with
// server-owned fields.
func (p *ProbeProcessor) normalize(probe *store.Probe, incoming proto.CheckResult) (*checks.Check, proto.CheckResult, error) {
	check, result, skip, err := p.normalizeWithCache(probe, make(map[string]*checks.Check), incoming, time.Now().UTC())
	if err != nil {
		return nil, proto.CheckResult{}, err
	}
	if skip {
		return nil, proto.CheckResult{}, nil
	}
	return check, result, nil
}

func (p *ProbeProcessor) normalizeBatch(probe *store.Probe, incoming []proto.CheckResult) ([]normalizedResult, error) {
	cache := make(map[string]*checks.Check, len(incoming))
	acceptedAt := time.Now().UTC()
	out := make([]normalizedResult, 0, len(incoming))

	for _, result := range incoming {
		check, normalized, skip, err := p.normalizeWithCache(probe, cache, result, acceptedAt)
		if err != nil {
			return nil, err
		}
		if skip {
			slog.Default().Debug("dropping stale result for unknown check", "component", "probe", "check_id", result.CheckID, "probe_id", probe.ProbeID)
			continue
		}
		out = append(out, normalizedResult{check: check, result: normalized})
	}
	return out, nil
}

func (p *ProbeProcessor) normalizeWithCache(probe *store.Probe, cache map[string]*checks.Check, incoming proto.CheckResult, acceptedAt time.Time) (*checks.Check, proto.CheckResult, bool, error) {
	if incoming.ProbeID != "" && incoming.ProbeID != probe.ProbeID {
		return nil, proto.CheckResult{}, false, &badRequestError{message: "probe_id does not match authenticated probe"}
	}

	check, ok := cache[incoming.CheckID]
	if !ok {
		loaded, err := p.store.GetCheck(incoming.CheckID)
		if err != nil {
			return nil, proto.CheckResult{}, false, fmt.Errorf("look up check %q: %w", incoming.CheckID, err)
		}
		if loaded == nil {
			return nil, incoming, true, nil
		}
		cache[incoming.CheckID] = loaded
		check = loaded
	}

	result := incoming
	result.ProbeID = probe.ProbeID
	result.Type = string(check.Type)
	result.Target = check.Target
	result.Timestamp = acceptedAt
	return check, result, false, nil
}
