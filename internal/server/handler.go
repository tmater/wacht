package server

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/tmater/wacht/internal/alert"
	"github.com/tmater/wacht/internal/config"
	"github.com/tmater/wacht/internal/proto"
	"github.com/tmater/wacht/internal/quorum"
	"github.com/tmater/wacht/internal/store"
)

type contextKey string

const contextKeyUser contextKey = "user"

// Handler holds the dependencies for HTTP handlers.
type Handler struct {
	store  *store.Store
	config *config.ServerConfig
}

// New creates a new Handler.
func New(store *store.Store, cfg *config.ServerConfig) *Handler {
	return &Handler{store: store, config: cfg}
}

// Routes registers all HTTP routes.
func (h *Handler) Routes() http.Handler {
	mux := http.NewServeMux()

	// Public routes — no auth required.
	mux.HandleFunc("GET /status", h.handleStatus)
	mux.HandleFunc("POST /api/auth/register", h.handleRegister)
	mux.HandleFunc("POST /api/auth/login", h.handleLogin)
	mux.HandleFunc("POST /api/auth/logout", h.handleLogout)

	// Probe routes — shared secret auth (internal, not customer-facing).
	probe := http.NewServeMux()
	probe.HandleFunc("POST /api/probes/register", h.handleProbeRegister)
	probe.HandleFunc("GET /api/probes/checks", h.handleProbeChecks)
	probe.HandleFunc("POST /api/probes/heartbeat", h.handleHeartbeat)
	probe.HandleFunc("POST /api/results", h.handleResult)
	mux.Handle("/api/probes/", h.requireSecret(probe))
	mux.Handle("/api/results", h.requireSecret(probe))

	// Dashboard routes — session auth.
	mux.HandleFunc("GET /api/checks", h.requireSession(h.handleListChecks))
	mux.HandleFunc("POST /api/checks", h.requireSession(h.handleCreateCheck))
	mux.HandleFunc("PUT /api/checks/{id}", h.requireSession(h.handleUpdateCheck))
	mux.HandleFunc("DELETE /api/checks/{id}", h.requireSession(h.handleDeleteCheck))

	return withCORS(mux)
}

// withCORS adds permissive CORS headers so the dashboard can talk to the
// server from a different port during local development.
func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
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

// requireSecret is middleware that rejects requests missing the correct X-Wacht-Secret header.
func (h *Handler) requireSecret(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Wacht-Secret") != h.config.Secret {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// requireSession validates the Bearer token and injects the user into context.
// In Go, context.WithValue is the standard way to pass request-scoped values
// through middleware — similar to ThreadLocal in Java.
func (h *Handler) requireSession(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if token == "" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		user, err := h.store.GetSessionUser(token)
		if err != nil {
			log.Printf("auth: session lookup error: %s", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		if user == nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		ctx := context.WithValue(r.Context(), contextKeyUser, user)
		next(w, r.WithContext(ctx))
	}
}

// sessionUser extracts the authenticated user from the request context.
func sessionUser(r *http.Request) *store.User {
	u, _ := r.Context().Value(contextKeyUser).(*store.User)
	return u
}

// handleRegister creates a new user account.
func (h *Handler) handleRegister(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if req.Email == "" || req.Password == "" {
		http.Error(w, "email and password are required", http.StatusBadRequest)
		return
	}
	user, err := h.store.CreateUser(req.Email, req.Password)
	if err != nil {
		log.Printf("auth: failed to create user email=%s: %s", req.Email, err)
		http.Error(w, "could not create user", http.StatusInternalServerError)
		return
	}
	token, err := h.store.CreateSession(user.ID)
	if err != nil {
		log.Printf("auth: failed to create session user_id=%d: %s", user.ID, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{"token": token, "email": user.Email})
}

// handleLogin authenticates a user and returns a session token.
func (h *Handler) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	user, err := h.store.AuthenticateUser(req.Email, req.Password)
	if err != nil {
		log.Printf("auth: authenticate error email=%s: %s", req.Email, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if user == nil {
		http.Error(w, "invalid credentials", http.StatusUnauthorized)
		return
	}
	token, err := h.store.CreateSession(user.ID)
	if err != nil {
		log.Printf("auth: failed to create session user_id=%d: %s", user.ID, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"token": token, "email": user.Email})
}

// handleLogout deletes the session token.
func (h *Handler) handleLogout(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if token == "" {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err := h.store.DeleteSession(token); err != nil {
		log.Printf("auth: failed to delete session: %s", err)
	}
	w.WriteHeader(http.StatusNoContent)
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
		// Majority vote passed — verify each down probe has consecutive failures
		// to filter out transient blips before alerting.
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
		// Majority reports up — resolve any open incident.
		if err := h.store.ResolveIncident(result.CheckID); err != nil {
			log.Printf("alert: failed to resolve incident check_id=%s: %s", result.CheckID, err)
		}
	}

	w.WriteHeader(http.StatusNoContent)
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
