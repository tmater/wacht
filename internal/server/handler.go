package server

import (
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/tmater/wacht/internal/alert"
	"github.com/tmater/wacht/internal/config"
	"github.com/tmater/wacht/internal/proto"
	"github.com/tmater/wacht/internal/quorum"
	"github.com/tmater/wacht/internal/store"
)

// Handler holds the dependencies for HTTP handlers.
type Handler struct {
	store         *store.Store
	config        *config.ServerConfig
	loginLimiter  *rateLimiter
	signupLimiter *rateLimiter
}

// New creates a new Handler.
func New(store *store.Store, cfg *config.ServerConfig) *Handler {
	return &Handler{
		store:         store,
		config:        cfg,
		loginLimiter:  newRateLimiter(),
		signupLimiter: newRateLimiter(),
	}
}

// Routes registers all HTTP routes.
func (h *Handler) Routes() http.Handler {
	mux := http.NewServeMux()

	// Public routes — no auth required.
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("POST /api/auth/login", h.loginLimiter.middleware(h.handleLogin))
	mux.HandleFunc("POST /api/auth/logout", h.handleLogout)
	mux.HandleFunc("POST /api/auth/request-access", h.signupLimiter.middleware(h.handleRequestAccess))

	// Probe routes — shared secret auth (internal, not customer-facing).
	probe := http.NewServeMux()
	probe.HandleFunc("POST /api/probes/register", h.handleProbeRegister)
	probe.HandleFunc("GET /api/probes/checks", h.handleProbeChecks)
	probe.HandleFunc("POST /api/probes/heartbeat", h.handleHeartbeat)
	probe.HandleFunc("POST /api/results", h.handleResult)
	mux.Handle("/api/probes/", h.requireSecret(probe))
	mux.Handle("/api/results", h.requireSecret(probe))

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
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Wacht-Secret")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// handleStatus serves the public status page as JSON.
func (h *Handler) handleStatus(w http.ResponseWriter, r *http.Request) {
	statuses, err := h.store.CheckStatuses()
	if err != nil {
		log.Printf("status: failed to query check statuses: %s", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	probeStatuses, err := h.store.AllProbeStatuses()
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
		ProbeID    string `json:"probe_id"`
		Online     bool   `json:"online"`
		LastSeenAt string `json:"last_seen_at"`
	}

	checks := make([]checkJSON, 0, len(statuses))
	for _, cs := range statuses {
		cj := checkJSON{
			CheckID: cs.CheckID,
			Target:  cs.Target,
			Status:  "up",
		}
		if !cs.Up || cs.IncidentSince != nil {
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
		probes = append(probes, probeJSON{
			ProbeID:    ps.ProbeID,
			Online:     time.Since(ps.LastSeenAt) < 90*time.Second,
			LastSeenAt: ps.LastSeenAt.UTC().Format(time.RFC3339),
		})
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
	if err := h.store.UpdateCheck(c, user.ID); err != nil {
		log.Printf("handler: failed to update check id=%s: %s", id, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
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
	var req struct {
		ProbeID string `json:"probe_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if req.ProbeID == "" {
		http.Error(w, "missing probe_id", http.StatusBadRequest)
		return
	}
	if err := h.store.UpdateProbeHeartbeat(req.ProbeID); err != nil {
		log.Printf("handler: failed to update heartbeat probe_id=%s: %s", req.ProbeID, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleProbeRegister registers a probe on startup.
func (h *Handler) handleProbeRegister(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ProbeID string `json:"probe_id"`
		Version string `json:"version"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if req.ProbeID == "" {
		http.Error(w, "missing probe_id", http.StatusBadRequest)
		return
	}
	if err := h.store.RegisterProbe(req.ProbeID, req.Version); err != nil {
		log.Printf("handler: failed to register probe_id=%s: %s", req.ProbeID, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	log.Printf("handler: registered probe_id=%s version=%s", req.ProbeID, req.Version)
	w.WriteHeader(http.StatusNoContent)
}

// handleResult receives a check result from a probe and saves it.
func (h *Handler) handleResult(w http.ResponseWriter, r *http.Request) {
	var result proto.CheckResult
	if err := json.NewDecoder(r.Body).Decode(&result); err != nil {
		log.Printf("handler: failed to decode result: %s", err)
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	registered, err := h.store.IsProbeRegistered(result.ProbeID)
	if err != nil {
		log.Printf("handler: failed to check registration probe_id=%s: %s", result.ProbeID, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if !registered {
		log.Printf("handler: rejected result from unregistered probe_id=%s", result.ProbeID)
		http.Error(w, "probe not registered", http.StatusForbidden)
		return
	}

	log.Printf("handler: received result check_id=%s probe_id=%s up=%v", result.CheckID, result.ProbeID, result.Up)

	if err := h.store.SaveResult(result); err != nil {
		log.Printf("handler: failed to save result: %s", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	recent, err := h.store.RecentResultsPerProbe(result.CheckID)
	if err != nil {
		log.Printf("quorum: failed to query recent results for check_id=%s: %s", result.CheckID, err)
	} else if quorum.MajorityDown(recent) {
		allConsecutive := true
		for _, r := range recent {
			if r.Up {
				continue
			}
			history, err := h.store.RecentResultsByProbe(result.CheckID, r.ProbeID, 2)
			if err != nil {
				log.Printf("quorum: failed to query history probe_id=%s check_id=%s: %s", r.ProbeID, result.CheckID, err)
				allConsecutive = false
				break
			}
			if !quorum.AllConsecutivelyDown(history) {
				allConsecutive = false
				break
			}
		}
		if allConsecutive {
			log.Printf("quorum: ALERT check_id=%s down on %d/%d probes (consecutive)", result.CheckID, countDown(recent), len(recent))
			alreadyOpen, err := h.store.OpenIncident(result.CheckID)
			if err != nil {
				log.Printf("alert: failed to open incident check_id=%s: %s", result.CheckID, err)
			} else if !alreadyOpen {
				if check := h.checkByID(result.CheckID); check != nil && check.Webhook != "" {
					payload := alert.AlertPayload{
						CheckID:     result.CheckID,
						Target:      check.Target,
						Status:      "down",
						ProbesDown:  countDown(recent),
						ProbesTotal: len(recent),
					}
					if err := alert.Fire(check.Webhook, payload); err != nil {
						log.Printf("alert: webhook failed check_id=%s: %s", result.CheckID, err)
					} else {
						log.Printf("alert: webhook fired check_id=%s url=%s", result.CheckID, check.Webhook)
					}
				}
			}
		}
	} else {
		if err := h.store.ResolveIncident(result.CheckID); err != nil {
			log.Printf("alert: failed to resolve incident check_id=%s: %s", result.CheckID, err)
		}
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleListIncidents returns the most recent 50 incidents, newest first.
func (h *Handler) handleListIncidents(w http.ResponseWriter, r *http.Request) {
	incidents, err := h.store.ListIncidents(50)
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

func (h *Handler) checkByID(id string) *store.Check {
	c, err := h.store.GetCheck(id)
	if err != nil {
		log.Printf("handler: failed to look up check id=%s: %s", id, err)
		return nil
	}
	return c
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
