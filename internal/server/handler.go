package server

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/tmater/wacht/internal/proto"
	"github.com/tmater/wacht/internal/store"
)

// Handler holds the dependencies for HTTP handlers.
type Handler struct {
	store *store.Store
}

// New creates a new Handler.
func New(store *store.Store) *Handler {
	return &Handler{store: store}
}

// Routes registers all HTTP routes.
func (h *Handler) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/results", h.handleResult)
	return mux
}

// handleResult receives a check result from a probe and saves it.
func (h *Handler) handleResult(w http.ResponseWriter, r *http.Request) {
	var result proto.CheckResult
	if err := json.NewDecoder(r.Body).Decode(&result); err != nil {
		log.Printf("handler: failed to decode result: %s", err)
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	log.Printf("handler: received result check_id=%s probe_id=%s up=%v", result.CheckID, result.ProbeID, result.Up)

	if err := h.store.SaveResult(result); err != nil {
		log.Printf("handler: failed to save result: %s", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
