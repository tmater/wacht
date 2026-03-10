package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/tmater/wacht/internal/alert"
	"github.com/tmater/wacht/internal/config"
	"github.com/tmater/wacht/internal/network"
	"github.com/tmater/wacht/internal/proto"
	"github.com/tmater/wacht/internal/store"
)

// Handler holds the dependencies for HTTP handlers.
type Handler struct {
	store           *store.Store
	config          *config.ServerConfig
	webhooks        *alert.Sender
	resultProcessor probeResultProcessor
	loginLimiter    *rateLimiter
	signupLimiter   *rateLimiter
}

// New creates a new Handler.
func New(store *store.Store, cfg *config.ServerConfig) *Handler {
	return &Handler{
		store:           store,
		config:          cfg,
		webhooks:        alert.NewSender(),
		resultProcessor: NewProbeResultProcessor(store),
		loginLimiter:    newRateLimiter(),
		signupLimiter:   newRateLimiter(),
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
	mux.HandleFunc("POST /api/auth/login", h.loginLimiter.middleware(h.handleLogin))
	mux.HandleFunc("POST /api/auth/logout", h.handleLogout)
	mux.HandleFunc("POST /api/auth/request-access", h.signupLimiter.middleware(h.handleRequestAccess))

	// Probe routes — per-probe auth.
	probe := http.NewServeMux()
	probe.HandleFunc("POST /api/probes/register", h.handleProbeRegister)
	probe.HandleFunc("GET /api/probes/checks", h.handleProbeChecks)
	probe.HandleFunc("POST /api/probes/heartbeat", h.handleHeartbeat)
	probe.HandleFunc("POST /api/results", h.handleResult)
	mux.Handle("/api/probes/", h.requireProbeAuth(probe))
	mux.Handle("/api/results", h.requireProbeAuth(probe))

	// Admin routes — session auth, is_admin required.
	mux.HandleFunc("GET /api/admin/signup-requests", h.requireAdmin(h.handleListSignupRequests))
	mux.HandleFunc("POST /api/admin/signup-requests/{id}/approve", h.requireAdmin(h.handleApproveSignupRequest))
	mux.HandleFunc("DELETE /api/admin/signup-requests/{id}", h.requireAdmin(h.handleDeleteSignupRequest))

	// Dashboard routes — session auth.
	mux.HandleFunc("GET /status", h.requireSession(h.handleStatus))
	mux.HandleFunc("GET /api/checks", h.requireSession(h.handleListChecks))
	mux.HandleFunc("POST /api/checks", h.requireSession(h.handleCreateCheck))
	mux.HandleFunc("PUT /api/checks/{id}", h.requireSession(h.handleUpdateCheck))
	mux.HandleFunc("DELETE /api/checks/{id}", h.requireSession(h.handleDeleteCheck))
	mux.HandleFunc("GET /api/auth/me", h.requireSession(h.handleMe))
	mux.HandleFunc("PUT /api/auth/change-password", h.requireSession(h.handleChangePassword))
	mux.HandleFunc("GET /api/incidents", h.requireSession(h.handleListIncidents))

	return withCORS(mux)
}

// withCORS adds permissive CORS headers so the dashboard can talk to the
// server from a different port during local development.
func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Wacht-Probe-ID, X-Wacht-Probe-Secret")
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

	statuses, err := h.store.CheckStatuses(user.ID)
	if err != nil {
		log.Printf("status: failed to query check statuses: %s", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	probeStatuses, err := h.store.ProbeStatuses(user.ID)
	if err != nil {
		log.Printf("status: failed to query probe statuses: %s", err)
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
		online := false
		if ps.LastSeenAt != nil {
			s := ps.LastSeenAt.UTC().Format(time.RFC3339)
			lastSeenAt = &s
			online = time.Since(*ps.LastSeenAt) < 90*time.Second
		}
		probes = append(probes, probeJSON{ProbeID: ps.ProbeID, Online: online, LastSeenAt: lastSeenAt})
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]any{"checks": checks, "probes": probes}); err != nil {
		log.Printf("status: failed to encode response: %s", err)
	}
}

// handleProbeChecks returns all checks for probes to run (no user scoping).
func (h *Handler) handleProbeChecks(w http.ResponseWriter, r *http.Request) {
	checks, err := h.store.ListAllChecks()
	if err != nil {
		log.Printf("handler: failed to list checks: %s", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(checks); err != nil {
		log.Printf("handler: failed to encode checks: %s", err)
	}
}

// handleListChecks returns checks owned by the authenticated user.
func (h *Handler) handleListChecks(w http.ResponseWriter, r *http.Request) {
	user := sessionUser(r)
	checks, err := h.store.ListChecks(user.ID)
	if err != nil {
		log.Printf("handler: failed to list checks: %s", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(checks); err != nil {
		log.Printf("handler: failed to encode checks: %s", err)
	}
}

// handleCreateCheck creates a new check owned by the authenticated user.
func (h *Handler) handleCreateCheck(w http.ResponseWriter, r *http.Request) {
	user := sessionUser(r)
	var c store.Check
	if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if c.ID == "" || c.Type == "" || c.Target == "" {
		http.Error(w, "id, type, and target are required", http.StatusBadRequest)
		return
	}
	if c.Interval < 0 || c.Interval > 86400 {
		http.Error(w, "interval must be between 0 and 86400 seconds", http.StatusBadRequest)
		return
	}
	if err := alert.ValidateWebhookURL(c.Webhook); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	if err := network.ValidateCheckTarget(ctx, c.Type, c.Target, h.targetPolicy()); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := h.store.CreateCheck(c, user.ID); err != nil {
		log.Printf("handler: failed to create check id=%s: %s", c.ID, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusCreated)
}

// handleUpdateCheck replaces type, target, and webhook for a check owned by the authenticated user.
func (h *Handler) handleUpdateCheck(w http.ResponseWriter, r *http.Request) {
	user := sessionUser(r)
	id := r.PathValue("id")
	var c store.Check
	if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	c.ID = id
	if c.Type == "" || c.Target == "" {
		http.Error(w, "type and target are required", http.StatusBadRequest)
		return
	}
	if c.Interval < 0 || c.Interval > 86400 {
		http.Error(w, "interval must be between 0 and 86400 seconds", http.StatusBadRequest)
		return
	}
	if err := alert.ValidateWebhookURL(c.Webhook); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	if err := network.ValidateCheckTarget(ctx, c.Type, c.Target, h.targetPolicy()); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := h.store.UpdateCheck(c, user.ID); err != nil {
		log.Printf("handler: failed to update check id=%s: %s", id, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) targetPolicy() network.Policy {
	return network.Policy{AllowPrivateTargets: h.config.AllowPrivateTargets}
}

// handleDeleteCheck removes a check owned by the authenticated user.
func (h *Handler) handleDeleteCheck(w http.ResponseWriter, r *http.Request) {
	user := sessionUser(r)
	id := r.PathValue("id")
	if err := h.store.DeleteCheck(id, user.ID); err != nil {
		log.Printf("handler: failed to delete check id=%s: %s", id, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleHeartbeat updates last_seen_at for a registered probe.
func (h *Handler) handleHeartbeat(w http.ResponseWriter, r *http.Request) {
	probe := authenticatedProbe(r)
	var req struct {
		ProbeID string `json:"probe_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err != io.EOF {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if req.ProbeID != "" && req.ProbeID != probe.ProbeID {
		http.Error(w, "probe_id does not match authenticated probe", http.StatusBadRequest)
		return
	}
	if err := h.store.UpdateProbeHeartbeat(probe.ProbeID); err != nil {
		log.Printf("handler: failed to update heartbeat probe_id=%s: %s", probe.ProbeID, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleProbeRegister records an authenticated probe startup.
func (h *Handler) handleProbeRegister(w http.ResponseWriter, r *http.Request) {
	probe := authenticatedProbe(r)
	var req struct {
		ProbeID string `json:"probe_id"`
		Version string `json:"version"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err != io.EOF {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if req.ProbeID != "" && req.ProbeID != probe.ProbeID {
		http.Error(w, "probe_id does not match authenticated probe", http.StatusBadRequest)
		return
	}
	if err := h.store.RegisterProbe(probe.ProbeID, req.Version); err != nil {
		log.Printf("handler: failed to register probe_id=%s: %s", probe.ProbeID, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	log.Printf("handler: registered probe_id=%s version=%s", probe.ProbeID, req.Version)
	w.WriteHeader(http.StatusNoContent)
}

// handleResult receives a check result from a probe and saves it.
func (h *Handler) handleResult(w http.ResponseWriter, r *http.Request) {
	probe := authenticatedProbe(r)
	var result proto.CheckResult
	if err := json.NewDecoder(r.Body).Decode(&result); err != nil {
		log.Printf("handler: failed to decode result: %s", err)
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	outcome, err := h.resultProcessor.Process(probe, result)
	if err != nil {
		var badRequest *badRequestError
		if errors.As(err, &badRequest) {
			http.Error(w, badRequest.Error(), http.StatusBadRequest)
			return
		}
		log.Printf("handler: failed to process result check_id=%s probe_id=%s: %s", result.CheckID, probe.ProbeID, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	if outcome.Alert != nil {
		if ok := h.webhooks.Enqueue(outcome.WebhookURL, *outcome.Alert); !ok {
			log.Printf("alert: webhook queue full, dropping check_id=%s url=%s", outcome.Alert.CheckID, outcome.WebhookURL)
		}
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleListIncidents returns the most recent 50 incidents for the
// authenticated user, newest first.
func (h *Handler) handleListIncidents(w http.ResponseWriter, r *http.Request) {
	user := sessionUser(r)
	incidents, err := h.store.ListIncidents(user.ID, 50)
	if err != nil {
		log.Printf("handler: failed to list incidents: %s", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	type incidentJSON struct {
		ID         int64   `json:"id"`
		CheckID    string  `json:"check_id"`
		StartedAt  string  `json:"started_at"`
		ResolvedAt *string `json:"resolved_at,omitempty"`
		DurationMs *int64  `json:"duration_ms,omitempty"`
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
		out = append(out, ij)
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(out); err != nil {
		log.Printf("handler: failed to encode incidents: %s", err)
	}
}
