package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/netip"
	"time"

	"github.com/tmater/wacht/internal/alert"
	probeapi "github.com/tmater/wacht/internal/api/probe"
	"github.com/tmater/wacht/internal/checks"
	"github.com/tmater/wacht/internal/config"
	"github.com/tmater/wacht/internal/monitoring"
	"github.com/tmater/wacht/internal/network"
	"github.com/tmater/wacht/internal/proto"
	"github.com/tmater/wacht/internal/store"
)

// Handler holds the dependencies for HTTP handlers.
type Handler struct {
	store          *store.Store
	monitoring     *monitoring.Runtime
	config         *config.ServerConfig
	webhooks       *alert.Sender
	authProcessor  authProcessor
	probeProcessor probeProcessor
	loginLimiter   *rateLimiter
	signupLimiter  *rateLimiter
	publicLimiter  *rateLimiter
	trustedProxies []netip.Prefix
}

// notificationJSON is the API response shape for one durable incident
// notification summary.
type notificationJSON struct {
	State         string  `json:"state"`
	Attempts      int     `json:"attempts"`
	LastError     string  `json:"last_error,omitempty"`
	LastAttemptAt *string `json:"last_attempt_at,omitempty"`
	NextAttemptAt *string `json:"next_attempt_at,omitempty"`
	DeliveredAt   *string `json:"delivered_at,omitempty"`
}

// New creates a new Handler.
func New(store *store.Store, monitoringRuntime *monitoring.Runtime, cfg *config.ServerConfig) *Handler {
	authRateLimit := cfg.AuthRateLimit
	return &Handler{
		store:          store,
		monitoring:     monitoringRuntime,
		config:         cfg,
		webhooks:       alert.NewSender(store, network.Policy{AllowPrivateTargets: cfg.AllowPrivateTargets}),
		authProcessor:  NewAuthProcessor(store),
		probeProcessor: NewProbeProcessor(store, monitoringRuntime),
		loginLimiter:   newRateLimiter(authRateLimit.Requests, authRateLimit.Window),
		signupLimiter:  newRateLimiter(authRateLimit.Requests, authRateLimit.Window),
		publicLimiter:  newRateLimiter(60, time.Minute),
		trustedProxies: append([]netip.Prefix(nil), cfg.TrustedProxyCIDRs...),
	}
}

// Close stops background workers owned by the handler.
func (h *Handler) Close() {
	if h == nil {
		return
	}
	h.webhooks.Close()
}

// Routes registers all HTTP routes.
func (h *Handler) Routes() http.Handler {
	mux := http.NewServeMux()

	// Public routes — no auth required.
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("GET /api/public/status/{slug}", h.rateLimited(h.publicLimiter, h.handlePublicStatus))
	mux.HandleFunc("POST /api/auth/login", h.rateLimited(h.loginLimiter, h.handleLogin))
	mux.HandleFunc("POST /api/auth/logout", h.handleLogout)
	mux.HandleFunc("POST /api/auth/setup-password", h.rateLimited(h.signupLimiter, h.handleSetupPassword))
	mux.HandleFunc("POST /api/auth/request-access", h.rateLimited(h.signupLimiter, h.handleRequestAccess))

	// Probe routes — per-probe auth.
	probe := http.NewServeMux()
	probe.HandleFunc(http.MethodPost+" "+probeapi.PathRegister, h.handleProbeRegister)
	probe.HandleFunc(http.MethodGet+" "+probeapi.PathChecks, h.handleProbeChecks)
	probe.HandleFunc(http.MethodPost+" "+probeapi.PathHeartbeat, h.handleHeartbeat)
	probe.HandleFunc(http.MethodPost+" "+probeapi.PathResults, h.handleResult)
	mux.Handle("/api/probes/", h.requireProbeAuth(probe))
	mux.Handle(probeapi.PathResults, h.requireProbeAuth(probe))

	// Admin routes — session auth, is_admin required.
	mux.HandleFunc("GET /api/admin/signup-requests", h.requireAdmin(h.handleListSignupRequests))
	mux.HandleFunc("POST /api/admin/signup-requests/{id}/approve", h.requireAdmin(h.handleApproveSignupRequest))
	mux.HandleFunc("POST /api/admin/signup-requests/{id}/reject", h.requireAdmin(h.handleRejectSignupRequest))

	// Dashboard routes — session auth.
	mux.HandleFunc("GET /status", h.requireSession(h.handleStatus))
	mux.HandleFunc("GET /api/checks", h.requireSession(h.handleListChecks))
	mux.HandleFunc("POST /api/checks", h.requireSession(h.handleCreateCheck))
	mux.HandleFunc("PUT /api/checks/{id}", h.requireSession(h.handleUpdateCheck))
	mux.HandleFunc("DELETE /api/checks/{id}", h.requireSession(h.handleDeleteCheck))
	mux.HandleFunc("GET /api/auth/me", h.requireSession(h.handleMe))
	mux.HandleFunc("PUT /api/auth/change-password", h.requireSession(h.handleChangePassword))
	mux.HandleFunc("GET /api/incidents", h.requireSession(h.handleListIncidents))

	return withRequestLog(withCORS(mux))
}

// withCORS adds permissive CORS headers so the dashboard can talk to the
// server from a different port during local development.
func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, "+probeapi.HeaderProbeID+", "+probeapi.HeaderProbeSecret)
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// handleStatus serves the authenticated status view as JSON.
func (h *Handler) handleStatus(w http.ResponseWriter, r *http.Request) {
	user := sessionUser(r)
	logger := requestLogger(r)

	statuses, err := h.store.CheckStatuses(user.ID)
	if err != nil {
		logger.Error("query check statuses failed", "component", "status", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	probeStatuses, err := h.store.ProbeStatuses(user.ID)
	if err != nil {
		logger.Error("query probe statuses failed", "component", "status", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	type checkJSON struct {
		CheckID       string  `json:"check_id"`
		Target        string  `json:"target"`
		Status        string  `json:"status"`
		IncidentSince *string `json:"incident_since,omitempty"`
	}

	type probeJSON struct {
		ProbeID    string  `json:"probe_id"`
		Online     bool    `json:"online"`
		LastSeenAt *string `json:"last_seen_at,omitempty"`
	}

	checks := make([]checkJSON, 0, len(statuses))
	for _, cs := range statuses {
		cj := checkJSON{
			CheckID: cs.CheckID,
			Target:  cs.Target,
			Status:  "up",
		}
		if !cs.Up {
			cj.Status = "down"
		}
		if cs.IncidentSince != nil {
			s := cs.IncidentSince.UTC().Format(time.RFC3339)
			cj.IncidentSince = &s
		}
		checks = append(checks, cj)
	}

	probes := make([]probeJSON, 0, len(probeStatuses))
	for _, ps := range probeStatuses {
		var lastSeenAt *string
		online := probeOnline(ps.LastSeenAt, h.probeOfflineAfter())
		if ps.LastSeenAt != nil {
			s := ps.LastSeenAt.UTC().Format(time.RFC3339)
			lastSeenAt = &s
		}
		probes = append(probes, probeJSON{ProbeID: ps.ProbeID, Online: online, LastSeenAt: lastSeenAt})
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]any{"checks": checks, "probes": probes}); err != nil {
		logger.Warn("encode status response failed", "component", "status", "err", err)
	}
}

// handlePublicStatus serves the anonymous customer-facing status view as JSON.
func (h *Handler) handlePublicStatus(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	logger := requestLogger(r)

	statuses, found, err := h.store.PublicCheckStatuses(slug)
	if err != nil {
		logger.Error("query public statuses failed", "component", "public_status", "slug", slug, "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if !found {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	type checkJSON struct {
		CheckID       string  `json:"check_id"`
		Status        string  `json:"status"`
		IncidentSince *string `json:"incident_since,omitempty"`
	}

	checks := make([]checkJSON, 0, len(statuses))
	for _, status := range statuses {
		item := checkJSON{
			CheckID: status.CheckID,
			Status:  status.Status,
		}
		if status.IncidentSince != nil {
			ts := status.IncidentSince.UTC().Format(time.RFC3339)
			item.IncidentSince = &ts
		}
		checks = append(checks, item)
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]any{"checks": checks}); err != nil {
		logger.Warn("encode public status response failed", "component", "public_status", "slug", slug, "err", err)
	}
}

func (h *Handler) probeOfflineAfter() time.Duration {
	if h == nil || h.config == nil || h.config.ProbeOfflineAfter <= 0 {
		return config.DefaultProbeOfflineAfter
	}
	return h.config.ProbeOfflineAfter
}

func probeOnline(lastSeenAt *time.Time, offlineAfter time.Duration) bool {
	if lastSeenAt == nil {
		return false
	}
	if offlineAfter <= 0 {
		offlineAfter = config.DefaultProbeOfflineAfter
	}
	return time.Since(*lastSeenAt) < offlineAfter
}

// handleProbeChecks returns the probe-visible check set. This currently stays
// global for all authenticated probes, but strips server-only metadata.
func (h *Handler) handleProbeChecks(w http.ResponseWriter, r *http.Request) {
	logger := requestLogger(r)
	checks, err := h.store.ListAllChecks()
	if err != nil {
		logger.Error("list probe checks failed", "component", "probe", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	payload := make([]proto.ProbeCheck, 0, len(checks))
	for _, check := range checks {
		payload = append(payload, proto.ProbeCheck{
			ID:       check.ID,
			Type:     string(check.Type),
			Target:   check.Target,
			Interval: check.Interval,
		})
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		logger.Warn("encode probe checks failed", "component", "probe", "err", err)
	}
}

// handleListChecks returns checks owned by the authenticated user.
func (h *Handler) handleListChecks(w http.ResponseWriter, r *http.Request) {
	user := sessionUser(r)
	logger := requestLogger(r)
	checks, err := h.store.ListChecks(user.ID)
	if err != nil {
		logger.Error("list checks failed", "component", "checks", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(checks); err != nil {
		logger.Warn("encode checks failed", "component", "checks", "err", err)
	}
}

// handleCreateCheck creates a new check owned by the authenticated user.
func (h *Handler) handleCreateCheck(w http.ResponseWriter, r *http.Request) {
	user := sessionUser(r)
	logger := requestLogger(r)
	check, err := h.decodeCheck(w, r, "")
	if err != nil {
		if writeProcessorError(w, err) {
			return
		}
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if err := h.store.CreateCheck(check, user.ID); err != nil {
		logger.Error("create check failed", "component", "checks", "check_id", check.ID, "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusCreated)
}

// handleUpdateCheck replaces type, target, and webhook for a check owned by the authenticated user.
func (h *Handler) handleUpdateCheck(w http.ResponseWriter, r *http.Request) {
	user := sessionUser(r)
	id := r.PathValue("id")
	logger := requestLogger(r)
	check, err := h.decodeCheck(w, r, id)
	if err != nil {
		if writeProcessorError(w, err) {
			return
		}
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if err := h.store.UpdateCheck(check, user.ID); err != nil {
		logger.Error("update check failed", "component", "checks", "check_id", id, "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) decodeCheck(w http.ResponseWriter, r *http.Request, id string) (checks.Check, error) {
	var check checks.Check
	if err := decodeJSONBody(w, r, &check, maxJSONRequestBodyBytes, false); err != nil {
		return checks.Check{}, err
	}
	if id != "" {
		check.ID = id
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	check, err := check.NormalizeAndValidate(ctx, h.targetPolicy(), true)
	if err != nil {
		return checks.Check{}, &badRequestError{message: err.Error()}
	}
	return check, nil
}

func (h *Handler) targetPolicy() network.Policy {
	return network.Policy{AllowPrivateTargets: h.config.AllowPrivateTargets}
}

// handleDeleteCheck removes a check owned by the authenticated user.
func (h *Handler) handleDeleteCheck(w http.ResponseWriter, r *http.Request) {
	user := sessionUser(r)
	id := r.PathValue("id")
	logger := requestLogger(r)
	if err := h.store.DeleteCheck(id, user.ID); err != nil {
		logger.Error("delete check failed", "component", "checks", "check_id", id, "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleHeartbeat updates last_seen_at for a registered probe.
func (h *Handler) handleHeartbeat(w http.ResponseWriter, r *http.Request) {
	probe := authenticatedProbe(r)
	logger := requestLogger(r)
	var req probeapi.HeartbeatRequest
	if err := decodeJSONBody(w, r, &req, maxProbeJSONRequestBodyBytes, true); err != nil {
		if writeProcessorError(w, err) {
			return
		}
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if err := h.probeProcessor.Heartbeat(probe, req); err != nil {
		if writeProcessorError(w, err) {
			return
		}
		logger.Error("update heartbeat failed", "component", "probe", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleProbeRegister records an authenticated probe startup.
func (h *Handler) handleProbeRegister(w http.ResponseWriter, r *http.Request) {
	probe := authenticatedProbe(r)
	logger := requestLogger(r)
	var req probeapi.RegisterRequest
	if err := decodeJSONBody(w, r, &req, maxProbeJSONRequestBodyBytes, true); err != nil {
		if writeProcessorError(w, err) {
			return
		}
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if err := h.probeProcessor.Register(probe, req); err != nil {
		if writeProcessorError(w, err) {
			return
		}
		logger.Error("register probe failed", "component", "probe", "version", req.Version, "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	logger.Info("probe registered", "component", "probe", "version", req.Version)
	w.WriteHeader(http.StatusNoContent)
}

// handleResult receives a check result from a probe and saves it.
func (h *Handler) handleResult(w http.ResponseWriter, r *http.Request) {
	probe := authenticatedProbe(r)
	logger := requestLogger(r)
	var result proto.CheckResult
	if err := decodeJSONBody(w, r, &result, maxProbeJSONRequestBodyBytes, false); err != nil {
		if writeProcessorError(w, err) {
			return
		}
		logger.Error("decode probe result failed", "component", "probe", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if _, err := h.probeProcessor.Process(probe, result); err != nil {
		if writeProcessorError(w, err) {
			return
		}
		logger.Error("process probe result failed", "component", "probe", "check_id", result.CheckID, "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleListIncidents returns the most recent 50 incidents for the
// authenticated user, newest first.
func (h *Handler) handleListIncidents(w http.ResponseWriter, r *http.Request) {
	user := sessionUser(r)
	logger := requestLogger(r)
	incidents, err := h.store.ListIncidents(user.ID, 50)
	if err != nil {
		logger.Error("list incidents failed", "component", "incidents", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	type incidentJSON struct {
		ID               int64             `json:"id"`
		CheckID          string            `json:"check_id"`
		StartedAt        string            `json:"started_at"`
		ResolvedAt       *string           `json:"resolved_at,omitempty"`
		DurationMs       *int64            `json:"duration_ms,omitempty"`
		DownNotification *notificationJSON `json:"down_notification,omitempty"`
		UpNotification   *notificationJSON `json:"up_notification,omitempty"`
	}

	out := make([]incidentJSON, 0, len(incidents))
	for _, inc := range incidents {
		ij := incidentJSON{
			ID:        inc.ID,
			CheckID:   inc.CheckID,
			StartedAt: inc.StartedAt.UTC().Format(time.RFC3339),
		}
		if inc.ResolvedAt != nil {
			s := inc.ResolvedAt.UTC().Format(time.RFC3339)
			ij.ResolvedAt = &s
			ms := inc.ResolvedAt.Sub(inc.StartedAt).Milliseconds()
			ij.DurationMs = &ms
		}
		ij.DownNotification = notificationToJSON(inc.DownNotification)
		ij.UpNotification = notificationToJSON(inc.UpNotification)
		out = append(out, ij)
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(out); err != nil {
		logger.Warn("encode incidents failed", "component", "incidents", "err", err)
	}
}

func notificationToJSON(n *store.IncidentNotification) *notificationJSON {
	if n == nil {
		return nil
	}

	out := &notificationJSON{
		State:     n.State,
		Attempts:  n.Attempts,
		LastError: n.LastError,
	}
	if n.LastAttemptAt != nil {
		s := n.LastAttemptAt.UTC().Format(time.RFC3339)
		out.LastAttemptAt = &s
	}
	if n.NextAttemptAt != nil {
		s := n.NextAttemptAt.UTC().Format(time.RFC3339)
		out.NextAttemptAt = &s
	}
	if n.DeliveredAt != nil {
		s := n.DeliveredAt.UTC().Format(time.RFC3339)
		out.DeliveredAt = &s
	}
	return out
}
