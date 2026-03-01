package server

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"time"
)

// handleRequestAccess accepts a public email submission for signup.
// Always returns 200 OK to prevent email enumeration.
func (h *Handler) handleRequestAccess(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if req.Email == "" {
		http.Error(w, "email is required", http.StatusBadRequest)
		return
	}
	if err := h.store.CreateSignupRequest(req.Email); err != nil {
		log.Printf("signup: failed to create request email=%s: %s", req.Email, err)
	} else {
		log.Printf("signup: request received email=%s", req.Email)
	}
	w.WriteHeader(http.StatusOK)
}

// handleListSignupRequests returns all pending signup requests. Protected by requireSecret.
func (h *Handler) handleListSignupRequests(w http.ResponseWriter, r *http.Request) {
	reqs, err := h.store.ListPendingSignupRequests()
	if err != nil {
		log.Printf("admin: failed to list signup requests: %s", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	type requestJSON struct {
		ID          int64  `json:"id"`
		Email       string `json:"email"`
		RequestedAt string `json:"requested_at"`
	}

	out := make([]requestJSON, 0, len(reqs))
	for _, sr := range reqs {
		out = append(out, requestJSON{
			ID:          sr.ID,
			Email:       sr.Email,
			RequestedAt: sr.RequestedAt.UTC().Format(time.RFC3339),
		})
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(out); err != nil {
		log.Printf("admin: failed to encode signup requests: %s", err)
	}
}

// handleApproveSignupRequest approves a pending request and returns the generated
// temporary password. Protected by requireSecret.
func (h *Handler) handleApproveSignupRequest(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	email, tempPassword, err := h.store.ApproveSignupRequest(id)
	if err != nil {
		log.Printf("admin: failed to approve signup request id=%d: %s", id, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if email == "" {
		http.Error(w, "request not found or already processed", http.StatusNotFound)
		return
	}

	log.Printf("admin: approved signup request id=%d email=%s", id, email)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"email":         email,
		"temp_password": tempPassword,
	})
}

// handleDeleteSignupRequest rejects and removes a pending signup request.
// Protected by requireSecret.
func (h *Handler) handleDeleteSignupRequest(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	if err := h.store.DeleteSignupRequest(id); err != nil {
		log.Printf("admin: failed to delete signup request id=%d: %s", id, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	log.Printf("admin: rejected signup request id=%d", id)
	w.WriteHeader(http.StatusNoContent)
}
