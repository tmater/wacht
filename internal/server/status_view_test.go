package server

import (
	"testing"
	"time"

	"github.com/tmater/wacht/internal/monitoring"
	"github.com/tmater/wacht/internal/store"
)

type fakeStatusViewStore struct {
	statusViews    []store.StatusCheckView
	statusErr      error
	publicViews    []store.PublicStatusCheckView
	publicFound    bool
	publicErr      error
	lastStatusUser int64
	lastPublicSlug string
}

func (f *fakeStatusViewStore) StatusCheckViews(userID int64) ([]store.StatusCheckView, error) {
	f.lastStatusUser = userID
	return append([]store.StatusCheckView(nil), f.statusViews...), f.statusErr
}

func (f *fakeStatusViewStore) PublicStatusCheckViews(slug string) ([]store.PublicStatusCheckView, bool, error) {
	f.lastPublicSlug = slug
	return append([]store.PublicStatusCheckView(nil), f.publicViews...), f.publicFound, f.publicErr
}

func TestBuildAuthenticatedStatusResponseUsesRuntimeState(t *testing.T) {
	const (
		pendingCheckID = "00000000-0000-0000-0000-000000000501"
		downCheckID    = "00000000-0000-0000-0000-000000000502"
		errorCheckID   = "00000000-0000-0000-0000-000000000503"
	)
	runtime := monitoring.NewRuntime([]string{downCheckID, errorCheckID}, []string{"probe-e", "probe-c", "probe-a", "probe-d", "probe-b"})
	at := time.Date(2026, time.April, 8, 12, 0, 0, 0, time.UTC)
	expiresAt := at.Add(30 * time.Second)
	incidentSince := at.Add(10 * time.Minute)

	for _, probeID := range []string{"probe-a", "probe-b", "probe-c", "probe-d"} {
		if _, err := runtime.ReceiveHeartbeat(probeID, at); err != nil {
			t.Fatalf("ReceiveHeartbeat %s: %v", probeID, err)
		}
	}
	if _, err := runtime.ExpireHeartbeat("probe-d"); err != nil {
		t.Fatalf("ExpireHeartbeat probe-d: %v", err)
	}
	if _, err := runtime.MarkProbeError("probe-e", "dial failed"); err != nil {
		t.Fatalf("MarkProbeError probe-e: %v", err)
	}

	for _, probeID := range []string{"probe-a", "probe-b", "probe-c"} {
		if _, err := runtime.ObserveCheckUp(downCheckID, probeID, at, &expiresAt); err != nil {
			t.Fatalf("ObserveCheckUp down-check %s: %v", probeID, err)
		}
	}
	downAt := at.Add(5 * time.Second)
	downExpiry := downAt.Add(30 * time.Second)
	for _, probeID := range []string{"probe-a", "probe-b", "probe-c"} {
		if _, err := runtime.ObserveCheckDown(downCheckID, probeID, downAt, &downExpiry, "timeout"); err != nil {
			t.Fatalf("ObserveCheckDown down-check first %s: %v", probeID, err)
		}
	}
	secondDownAt := downAt.Add(time.Second)
	secondDownExpiry := secondDownAt.Add(30 * time.Second)
	for _, probeID := range []string{"probe-a", "probe-b", "probe-c"} {
		if _, err := runtime.ObserveCheckDown(downCheckID, probeID, secondDownAt, &secondDownExpiry, "timeout"); err != nil {
			t.Fatalf("ObserveCheckDown down-check second %s: %v", probeID, err)
		}
	}

	for _, probeID := range []string{"probe-a", "probe-b", "probe-c"} {
		if _, err := runtime.ObserveCheckUp(errorCheckID, probeID, at, &expiresAt); err != nil {
			t.Fatalf("ObserveCheckUp error-check %s: %v", probeID, err)
		}
	}
	secondAt := at.Add(time.Second)
	secondExpiry := secondAt.Add(30 * time.Second)
	if _, err := runtime.ObserveCheckUp(errorCheckID, "probe-a", secondAt, &secondExpiry); err != nil {
		t.Fatalf("ObserveCheckUp error-check probe-a second: %v", err)
	}
	if _, err := runtime.LoseCheckEvidence(errorCheckID, "probe-c"); err != nil {
		t.Fatalf("LoseCheckEvidence error-check probe-c: %v", err)
	}

	st := &fakeStatusViewStore{
		statusViews: []store.StatusCheckView{
			{CheckID: pendingCheckID, CheckName: "pending-check", Target: "https://pending.example.com"},
			{CheckID: downCheckID, CheckName: "down-check", Target: "https://down.example.com", IncidentSince: &incidentSince},
			{CheckID: errorCheckID, CheckName: "error-check", Target: "https://error.example.com"},
		},
	}

	checks, probes, err := buildAuthenticatedStatusResponse(runtime, st, 42)
	if err != nil {
		t.Fatalf("buildAuthenticatedStatusResponse() error = %v", err)
	}
	if st.lastStatusUser != 42 {
		t.Fatalf("status user = %d, want 42", st.lastStatusUser)
	}

	if len(checks) != 3 {
		t.Fatalf("len(checks) = %d, want 3", len(checks))
	}
	if got := checks[0].Status; got != "pending" {
		t.Fatalf("pending-check status = %q, want pending", got)
	}
	if got := checks[1].Status; got != "down" {
		t.Fatalf("down-check status = %q, want down", got)
	}
	if checks[1].IncidentSince == nil || *checks[1].IncidentSince != incidentSince.Format(time.RFC3339) {
		t.Fatalf("down-check incident_since = %v, want %s", checks[1].IncidentSince, incidentSince.Format(time.RFC3339))
	}
	if got := checks[2].Status; got != "error" {
		t.Fatalf("error-check status = %q, want error", got)
	}

	if len(probes) != 5 {
		t.Fatalf("len(probes) = %d, want 5", len(probes))
	}
	if probes[0].ProbeID != "probe-a" || probes[0].Status != "online" || !probes[0].Online {
		t.Fatalf("probe-a = %#v, want online probe-a", probes[0])
	}
	if probes[3].ProbeID != "probe-d" || probes[3].Status != "offline" || probes[3].Online {
		t.Fatalf("probe-d = %#v, want offline probe-d", probes[3])
	}
	if probes[3].LastSeenAt == nil {
		t.Fatal("expected probe-d to keep last_seen_at after going offline")
	}
	if probes[4].ProbeID != "probe-e" || probes[4].Status != "error" || probes[4].LastError != "dial failed" {
		t.Fatalf("probe-e = %#v, want error probe-e with last_error", probes[4])
	}
}

func TestBuildAuthenticatedStatusResponseOmitsProbesWithoutChecks(t *testing.T) {
	runtime := monitoring.NewRuntime(nil, []string{"probe-a"})
	st := &fakeStatusViewStore{}

	checks, probes, err := buildAuthenticatedStatusResponse(runtime, st, 7)
	if err != nil {
		t.Fatalf("buildAuthenticatedStatusResponse() error = %v", err)
	}
	if len(checks) != 0 {
		t.Fatalf("len(checks) = %d, want 0", len(checks))
	}
	if len(probes) != 0 {
		t.Fatalf("len(probes) = %d, want 0", len(probes))
	}
}

func TestBuildPublicStatusResponseUsesRuntimeState(t *testing.T) {
	const (
		downCheckID    = "00000000-0000-0000-0000-000000000601"
		pendingCheckID = "00000000-0000-0000-0000-000000000602"
	)
	runtime := monitoring.NewRuntime([]string{downCheckID}, []string{"probe-a", "probe-b", "probe-c"})
	at := time.Date(2026, time.April, 8, 12, 0, 0, 0, time.UTC)
	expiresAt := at.Add(30 * time.Second)
	incidentSince := at.Add(2 * time.Minute)

	for _, probeID := range []string{"probe-a", "probe-b"} {
		if _, err := runtime.ObserveCheckUp(downCheckID, probeID, at, &expiresAt); err != nil {
			t.Fatalf("ObserveCheckUp %s: %v", probeID, err)
		}
	}
	downAt := at.Add(5 * time.Second)
	downExpiry := downAt.Add(30 * time.Second)
	for _, probeID := range []string{"probe-a", "probe-b"} {
		if _, err := runtime.ObserveCheckDown(downCheckID, probeID, downAt, &downExpiry, "timeout"); err != nil {
			t.Fatalf("ObserveCheckDown first %s: %v", probeID, err)
		}
	}
	secondDownAt := downAt.Add(time.Second)
	secondDownExpiry := secondDownAt.Add(30 * time.Second)
	for _, probeID := range []string{"probe-a", "probe-b"} {
		if _, err := runtime.ObserveCheckDown(downCheckID, probeID, secondDownAt, &secondDownExpiry, "timeout"); err != nil {
			t.Fatalf("ObserveCheckDown second %s: %v", probeID, err)
		}
	}

	st := &fakeStatusViewStore{
		publicViews: []store.PublicStatusCheckView{
			{CheckID: downCheckID, CheckName: "down-check", IncidentSince: &incidentSince},
			{CheckID: pendingCheckID, CheckName: "pending-check"},
		},
		publicFound: true,
	}

	checks, found, err := buildPublicStatusResponse(runtime, st, "demo")
	if err != nil {
		t.Fatalf("buildPublicStatusResponse() error = %v", err)
	}
	if !found {
		t.Fatal("found = false, want true")
	}
	if st.lastPublicSlug != "demo" {
		t.Fatalf("public slug = %q, want demo", st.lastPublicSlug)
	}

	if len(checks) != 2 {
		t.Fatalf("len(checks) = %d, want 2", len(checks))
	}
	if got := checks[0].Status; got != "down" {
		t.Fatalf("down-check status = %q, want down", got)
	}
	if checks[0].IncidentSince == nil || *checks[0].IncidentSince != incidentSince.Format(time.RFC3339) {
		t.Fatalf("down-check incident_since = %v, want %s", checks[0].IncidentSince, incidentSince.Format(time.RFC3339))
	}
	if got := checks[1].Status; got != "pending" {
		t.Fatalf("pending-check status = %q, want pending", got)
	}
}

func TestBuildPublicStatusResponsePropagatesMissingSlug(t *testing.T) {
	runtime := monitoring.NewRuntime(nil, nil)
	st := &fakeStatusViewStore{publicFound: false}

	_, found, err := buildPublicStatusResponse(runtime, st, "missing")
	if err != nil {
		t.Fatalf("buildPublicStatusResponse() error = %v", err)
	}
	if found {
		t.Fatal("found = true, want false")
	}
}
