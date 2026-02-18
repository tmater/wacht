package server

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/tmater/wacht/internal/alert"
	"github.com/tmater/wacht/internal/config"
	"github.com/tmater/wacht/internal/proto"
	"github.com/tmater/wacht/internal/quorum"
	"github.com/tmater/wacht/internal/store"
)

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
	mux.HandleFunc("POST /api/probes/register", h.handleRegister)
	mux.HandleFunc("GET /api/probes/checks", h.handleChecks)
	mux.HandleFunc("POST /api/probes/heartbeat", h.handleHeartbeat)
	mux.HandleFunc("POST /api/results", h.handleResult)
	return h.requireSecret(mux)
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

// handleChecks returns the list of checks the probe should run.
func (h *Handler) handleChecks(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(h.config.Checks); err != nil {
		log.Printf("handler: failed to encode checks: %s", err)
	}
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

// handleRegister registers a probe on startup.
func (h *Handler) handleRegister(w http.ResponseWriter, r *http.Request) {
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
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) checkByID(id string) *config.Check {
	for i := range h.config.Checks {
		if h.config.Checks[i].ID == id {
			return &h.config.Checks[i]
		}
	}
	return nil
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
