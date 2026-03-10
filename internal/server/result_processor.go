package server

import (
	"fmt"
	"log"
	"time"

	"github.com/tmater/wacht/internal/alert"
	"github.com/tmater/wacht/internal/proto"
	"github.com/tmater/wacht/internal/quorum"
	"github.com/tmater/wacht/internal/store"
)

type resultStore interface {
	GetCheck(id string) (*store.Check, error)
	SaveResult(r proto.CheckResult) error
	RecentResultsPerProbe(checkID string) ([]proto.CheckResult, error)
	RecentResultsByProbe(checkID, probeID string, n int) ([]proto.CheckResult, error)
	OpenIncident(checkID string) (bool, error)
	ResolveIncident(checkID string) (bool, error)
}

type ProbeResultOutcome struct {
	WebhookURL string
	Alert      *alert.AlertPayload
}

type probeResultProcessor interface {
	Process(probe *store.Probe, incoming proto.CheckResult) (ProbeResultOutcome, error)
}

// ProbeResultProcessor applies a single authenticated probe result to the
// current monitoring state and reports any resulting alert transition.
type ProbeResultProcessor struct {
	store resultStore
}

func NewProbeResultProcessor(store resultStore) *ProbeResultProcessor {
	return &ProbeResultProcessor{store: store}
}

type badRequestError struct {
	message string
}

func (e *badRequestError) Error() string {
	return e.message
}

func (p *ProbeResultProcessor) Process(probe *store.Probe, incoming proto.CheckResult) (ProbeResultOutcome, error) {
	if probe == nil {
		return ProbeResultOutcome{}, fmt.Errorf("probe is required")
	}

	check, result, err := p.normalize(probe, incoming)
	if err != nil {
		return ProbeResultOutcome{}, err
	}

	log.Printf("handler: received result check_id=%s probe_id=%s up=%v", result.CheckID, result.ProbeID, result.Up)

	if err := p.store.SaveResult(result); err != nil {
		return ProbeResultOutcome{}, fmt.Errorf("save result: %w", err)
	}

	recent, err := p.store.RecentResultsPerProbe(result.CheckID)
	if err != nil {
		log.Printf("quorum: failed to query recent results for check_id=%s: %s", result.CheckID, err)
		return ProbeResultOutcome{}, nil
	}

	if quorum.MajorityDown(recent) {
		return p.openIncidentIfNeeded(check, recent)
	}
	return p.resolveIncidentIfNeeded(check, recent)
}

func (p *ProbeResultProcessor) normalize(probe *store.Probe, incoming proto.CheckResult) (*store.Check, proto.CheckResult, error) {
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
	result.Type = proto.CheckType(check.Type)
	result.Target = check.Target
	result.Timestamp = time.Now().UTC()
	return check, result, nil
}

func (p *ProbeResultProcessor) openIncidentIfNeeded(check *store.Check, recent []proto.CheckResult) (ProbeResultOutcome, error) {
	for _, result := range recent {
		if result.Up {
			continue
		}

		history, err := p.store.RecentResultsByProbe(check.ID, result.ProbeID, 2)
		if err != nil {
			log.Printf("quorum: failed to query history probe_id=%s check_id=%s: %s", result.ProbeID, check.ID, err)
			return ProbeResultOutcome{}, nil
		}
		if !quorum.AllConsecutivelyDown(history) {
			return ProbeResultOutcome{}, nil
		}
	}

	probesDown := countDown(recent)
	log.Printf("quorum: ALERT check_id=%s down on %d/%d probes (consecutive)", check.ID, probesDown, len(recent))

	alreadyOpen, err := p.store.OpenIncident(check.ID)
	if err != nil {
		log.Printf("alert: failed to open incident check_id=%s: %s", check.ID, err)
		return ProbeResultOutcome{}, nil
	}
	if alreadyOpen || check.Webhook == "" {
		return ProbeResultOutcome{}, nil
	}

	return ProbeResultOutcome{
		WebhookURL: check.Webhook,
		Alert: &alert.AlertPayload{
			CheckID:     check.ID,
			Target:      check.Target,
			Status:      "down",
			ProbesDown:  probesDown,
			ProbesTotal: len(recent),
		},
	}, nil
}

func (p *ProbeResultProcessor) resolveIncidentIfNeeded(check *store.Check, recent []proto.CheckResult) (ProbeResultOutcome, error) {
	resolved, err := p.store.ResolveIncident(check.ID)
	if err != nil {
		log.Printf("alert: failed to resolve incident check_id=%s: %s", check.ID, err)
		return ProbeResultOutcome{}, nil
	}
	if !resolved || check.Webhook == "" {
		return ProbeResultOutcome{}, nil
	}

	return ProbeResultOutcome{
		WebhookURL: check.Webhook,
		Alert: &alert.AlertPayload{
			CheckID:     check.ID,
			Target:      check.Target,
			Status:      "up",
			ProbesDown:  countDown(recent),
			ProbesTotal: len(recent),
		},
	}, nil
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
