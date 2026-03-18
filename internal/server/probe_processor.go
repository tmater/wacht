package server

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/tmater/wacht/internal/alert"
	probeapi "github.com/tmater/wacht/internal/api/probe"
	"github.com/tmater/wacht/internal/checks"
	"github.com/tmater/wacht/internal/proto"
	"github.com/tmater/wacht/internal/quorum"
	"github.com/tmater/wacht/internal/store"
)

type probeStore interface {
	RegisterProbe(probeID, version string) error
	UpdateProbeHeartbeat(probeID string) error
	GetCheck(id string) (*checks.Check, error)
	SaveResult(r proto.CheckResult) error
	RecentResultsPerProbe(checkID string) ([]proto.CheckResult, error)
	RecentResultsByProbe(checkID, probeID string, n int) ([]proto.CheckResult, error)
	OpenIncidentWithNotification(checkID string, request *store.NotificationRequest) (bool, error)
	ResolveIncidentWithNotification(checkID string, request *store.NotificationRequest) (bool, error)
}

type ProbeResultOutcome struct{}

type probeProcessor interface {
	Heartbeat(probe *store.Probe, req probeapi.HeartbeatRequest) error
	Register(probe *store.Probe, req probeapi.RegisterRequest) error
	Process(probe *store.Probe, incoming proto.CheckResult) (ProbeResultOutcome, error)
}

type ProbeProcessor struct {
	store probeStore
}

func NewProbeProcessor(store probeStore) *ProbeProcessor {
	return &ProbeProcessor{store: store}
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

	if err := p.store.SaveResult(result); err != nil {
		return ProbeResultOutcome{}, fmt.Errorf("save result: %w", err)
	}

	recent, err := p.store.RecentResultsPerProbe(result.CheckID)
	if err != nil {
		slog.Default().Error("query recent results failed", "component", "quorum", "check_id", result.CheckID, "err", err)
		return ProbeResultOutcome{}, nil
	}

	if quorum.MajorityDown(recent) {
		return p.openIncidentIfNeeded(check, recent)
	}
	return p.resolveIncidentIfNeeded(check, recent)
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

func (p *ProbeProcessor) openIncidentIfNeeded(check *checks.Check, recent []proto.CheckResult) (ProbeResultOutcome, error) {
	for _, result := range recent {
		if result.Up {
			continue
		}

		history, err := p.store.RecentResultsByProbe(check.ID, result.ProbeID, 2)
		if err != nil {
			slog.Default().Error("query probe failure history failed", "component", "quorum", "check_id", check.ID, "probe_id", result.ProbeID, "err", err)
			return ProbeResultOutcome{}, nil
		}
		if !quorum.AllConsecutivelyDown(history) {
			return ProbeResultOutcome{}, nil
		}
	}

	probesDown := countDown(recent)

	request, err := notificationRequest(check.Webhook, alert.AlertPayload{
		CheckID:     check.ID,
		Target:      check.Target,
		Status:      "down",
		ProbesDown:  probesDown,
		ProbesTotal: len(recent),
	})
	if err != nil {
		slog.Default().Warn("encode down notification failed", "component", "alert", "check_id", check.ID, "err", err)
	}

	alreadyOpen, err := p.store.OpenIncidentWithNotification(check.ID, request)
	if err != nil {
		slog.Default().Error("open incident failed", "component", "alert", "check_id", check.ID, "err", err)
		return ProbeResultOutcome{}, nil
	}
	if alreadyOpen {
		return ProbeResultOutcome{}, nil
	}

	slog.Default().Info("incident opened", "component", "alert", "check_id", check.ID, "probes_down", probesDown, "probes_total", len(recent))
	return ProbeResultOutcome{}, nil
}

func (p *ProbeProcessor) resolveIncidentIfNeeded(check *checks.Check, recent []proto.CheckResult) (ProbeResultOutcome, error) {
	for _, result := range recent {
		if !result.Up {
			continue
		}

		history, err := p.store.RecentResultsByProbe(check.ID, result.ProbeID, 2)
		if err != nil {
			slog.Default().Error("query probe recovery history failed", "component", "quorum", "check_id", check.ID, "probe_id", result.ProbeID, "err", err)
			return ProbeResultOutcome{}, nil
		}
		if !quorum.AllConsecutivelyUp(history) {
			return ProbeResultOutcome{}, nil
		}
	}

	request, err := notificationRequest(check.Webhook, alert.AlertPayload{
		CheckID:     check.ID,
		Target:      check.Target,
		Status:      "up",
		ProbesDown:  countDown(recent),
		ProbesTotal: len(recent),
	})
	if err != nil {
		slog.Default().Warn("encode recovery notification failed", "component", "alert", "check_id", check.ID, "err", err)
	}

	resolved, err := p.store.ResolveIncidentWithNotification(check.ID, request)
	if err != nil {
		slog.Default().Error("resolve incident failed", "component", "alert", "check_id", check.ID, "err", err)
		return ProbeResultOutcome{}, nil
	}
	if !resolved {
		return ProbeResultOutcome{}, nil
	}

	slog.Default().Info("incident resolved", "component", "alert", "check_id", check.ID, "probes_down", countDown(recent), "probes_total", len(recent))
	return ProbeResultOutcome{}, nil
}

func countDown(results []proto.CheckResult) int {
	n := 0
	for _, r := range results {
		if !r.Up {
			n++
		}
	}
	return n
}

func notificationRequest(webhookURL string, payload alert.AlertPayload) (*store.NotificationRequest, error) {
	if webhookURL == "" {
		return nil, nil
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return &store.NotificationRequest{
		WebhookURL: webhookURL,
		Payload:    body,
	}, nil
}
