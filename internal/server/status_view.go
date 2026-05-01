package server

import (
	"fmt"
	"time"

	"github.com/tmater/wacht/internal/monitoring"
	"github.com/tmater/wacht/internal/store"
)

type statusViewStore interface {
	StatusCheckViews(userID int64) ([]store.StatusCheckView, error)
	PublicStatusCheckViews(slug string) ([]store.PublicStatusCheckView, bool, error)
}

type statusCheckDTO struct {
	CheckID       string  `json:"check_id"`
	CheckName     string  `json:"check_name"`
	Target        string  `json:"target,omitempty"`
	Status        string  `json:"status"`
	IncidentSince *string `json:"incident_since,omitempty"`
}

type statusProbeDTO struct {
	ProbeID    string  `json:"probe_id"`
	Status     string  `json:"status"`
	Online     bool    `json:"online"`
	LastSeenAt *string `json:"last_seen_at,omitempty"`
	LastError  string  `json:"last_error,omitempty"`
}

func buildAuthenticatedStatusResponse(runtime *monitoring.Runtime, st statusViewStore, userID int64) ([]statusCheckDTO, []statusProbeDTO, error) {
	if runtime == nil {
		return nil, nil, fmt.Errorf("monitoring runtime is required")
	}
	if st == nil {
		return nil, nil, fmt.Errorf("status store is required")
	}

	views, err := st.StatusCheckViews(userID)
	if err != nil {
		return nil, nil, err
	}

	checkIDs := make([]string, 0, len(views))
	for _, view := range views {
		checkIDs = append(checkIDs, view.CheckID)
	}

	quorumByCheckID := make(map[string]monitoring.CheckQuorumState, len(checkIDs))
	for _, quorum := range runtime.QuorumSnapshots(checkIDs) {
		quorumByCheckID[quorum.CheckID] = quorum
	}

	checks := make([]statusCheckDTO, 0, len(views))
	for _, view := range views {
		quorum := quorumByCheckID[view.CheckID]
		checks = append(checks, statusCheckDTO{
			CheckID:       view.CheckID,
			CheckName:     view.CheckName,
			Target:        view.Target,
			Status:        string(quorum.State),
			IncidentSince: formatOptionalTimestamp(view.IncidentSince),
		})
	}

	if len(views) == 0 {
		return checks, nil, nil
	}

	probes := runtime.ProbeSnapshots()
	items := make([]statusProbeDTO, 0, len(probes))
	for _, probe := range probes {
		items = append(items, statusProbeDTO{
			ProbeID:    probe.ProbeID,
			Status:     string(probe.State),
			Online:     probe.State == monitoring.ProbeStateOnline,
			LastSeenAt: formatOptionalTimestamp(probe.LastHeartbeatAt),
			LastError:  probe.LastError,
		})
	}
	return checks, items, nil
}

func buildPublicStatusResponse(runtime *monitoring.Runtime, st statusViewStore, slug string) ([]statusCheckDTO, bool, error) {
	if runtime == nil {
		return nil, false, fmt.Errorf("monitoring runtime is required")
	}
	if st == nil {
		return nil, false, fmt.Errorf("status store is required")
	}

	views, found, err := st.PublicStatusCheckViews(slug)
	if err != nil || !found {
		return nil, found, err
	}

	checkIDs := make([]string, 0, len(views))
	for _, view := range views {
		checkIDs = append(checkIDs, view.CheckID)
	}

	quorumByCheckID := make(map[string]monitoring.CheckQuorumState, len(checkIDs))
	for _, quorum := range runtime.QuorumSnapshots(checkIDs) {
		quorumByCheckID[quorum.CheckID] = quorum
	}

	checks := make([]statusCheckDTO, 0, len(views))
	for _, view := range views {
		quorum := quorumByCheckID[view.CheckID]
		checks = append(checks, statusCheckDTO{
			CheckID:       view.CheckID,
			CheckName:     view.CheckName,
			Status:        string(quorum.State),
			IncidentSince: formatOptionalTimestamp(view.IncidentSince),
		})
	}

	return checks, true, nil
}

func formatOptionalTimestamp(ts *time.Time) *string {
	if ts == nil {
		return nil
	}
	formatted := ts.UTC().Format(time.RFC3339)
	return &formatted
}
