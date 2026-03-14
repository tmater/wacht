package server

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/tmater/wacht/internal/config"
	"github.com/tmater/wacht/internal/store"
)

type contextKey string

const (
	contextKeyUser  contextKey = "user"
	contextKeyProbe contextKey = "probe"
)

const (
	probeIDHeader     = "X-Wacht-Probe-ID"
	probeSecretHeader = "X-Wacht-Probe-Secret"
)

// requireProbeAuth authenticates an individual probe using its provisioned
// probe_id + secret and injects that probe into the request context.
func (h *Handler) requireProbeAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		probeID := strings.TrimSpace(r.Header.Get(probeIDHeader))
		secret := strings.TrimSpace(r.Header.Get(probeSecretHeader))
		if probeID == "" || secret == "" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		probe, err := h.store.AuthenticateProbe(probeID, secret)
		if err != nil {
			log.Printf("auth: probe lookup error probe_id=%s: %s", probeID, err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		if probe == nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		ctx := context.WithValue(r.Context(), contextKeyProbe, probe)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// requireSession validates the Bearer token and injects the user into context.
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

func authenticatedProbe(r *http.Request) *store.Probe {
	p, _ := r.Context().Value(contextKeyProbe).(*store.Probe)
	return p
}

// requireAdmin validates the session and additionally requires is_admin=true.
func (h *Handler) requireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return h.requireSession(func(w http.ResponseWriter, r *http.Request) {
		if !sessionUser(r).IsAdmin {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		next(w, r)
	})
}

// handleMe returns the current user's email and admin status.
func (h *Handler) handleMe(w http.ResponseWriter, r *http.Request) {
	u := sessionUser(r)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"email": u.Email, "is_admin": u.IsAdmin})
}

// rateLimiter is a simple per-IP token bucket rate limiter.
type rateLimiter struct {
	mu     sync.Mutex
	tokens map[string]*tokenBucket
	limit  int
	window time.Duration
}

type tokenBucket struct {
	count   int
	resetAt time.Time
}

func newRateLimiter(limit int, window time.Duration) *rateLimiter {
	if limit <= 0 {
		limit = config.DefaultAuthRateLimitRequests
	}
	if window <= 0 {
		window = config.DefaultAuthRateLimitWindow
	}
	return &rateLimiter{
		tokens: make(map[string]*tokenBucket),
		limit:  limit,
		window: window,
	}
}

func (rl *rateLimiter) allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	b, ok := rl.tokens[ip]
	if !ok || time.Now().After(b.resetAt) {
		rl.tokens[ip] = &tokenBucket{count: 1, resetAt: time.Now().Add(rl.window)}
		return true
	}
	if b.count >= rl.limit {
		return false
	}
	b.count++
	return true
}

func (rl *rateLimiter) middleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ip := r.RemoteAddr
		if i := strings.LastIndex(ip, ":"); i != -1 {
			ip = ip[:i]
		}
		if !rl.allow(ip) {
			http.Error(w, "too many requests", http.StatusTooManyRequests)
			return
		}
		next(w, r)
	}
}

// handleLogin authenticates a user and returns a session token.
func (h *Handler) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	outcome, err := h.authProcessor.Login(req)
	if err != nil {
		if writeProcessorError(w, err) {
			return
		}
		log.Printf("auth: login failed email=%s: %s", req.Email, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"token": outcome.Token, "email": outcome.Email})
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

// handleChangePassword verifies the current password and sets a new one.
func (h *Handler) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	user := sessionUser(r)
	var req ChangePasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if err := h.authProcessor.ChangePassword(user, req); err != nil {
		if writeProcessorError(w, err) {
			return
		}
		log.Printf("auth: failed to update password user_id=%d: %s", user.ID, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleRequestAccess accepts a public email submission for signup.
// Always returns 200 OK to prevent email enumeration.
func (h *Handler) handleRequestAccess(w http.ResponseWriter, r *http.Request) {
	var req RequestAccessRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if err := h.authProcessor.RequestAccess(req); err != nil {
		if writeProcessorError(w, err) {
			return
		}
		log.Printf("signup: failed to create request email=%s: %s", req.Email, err)
	} else {
		log.Printf("signup: request received email=%s", req.Email)
	}
	w.WriteHeader(http.StatusOK)
}

// handleListSignupRequests returns all pending signup requests. Protected by requireAdmin.
func (h *Handler) handleListSignupRequests(w http.ResponseWriter, r *http.Request) {
	reqs, err := h.authProcessor.ListPendingSignupRequests()
	if err != nil {
		if writeProcessorError(w, err) {
			return
		}
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
// temporary password. Protected by requireAdmin.
func (h *Handler) handleApproveSignupRequest(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	outcome, err := h.authProcessor.ApproveSignupRequest(id)
	if err != nil {
		if writeProcessorError(w, err) {
			return
		}
		log.Printf("admin: failed to approve signup request id=%d: %s", id, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	log.Printf("admin: approved signup request id=%d email=%s", id, outcome.Email)
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]string{
		"email":         outcome.Email,
		"temp_password": outcome.TempPassword,
	}); err != nil {
		log.Printf("admin: failed to encode approved signup request id=%d: %s", id, err)
	}
}

// handleDeleteSignupRequest rejects and removes a pending signup request.
// Protected by requireAdmin.
func (h *Handler) handleDeleteSignupRequest(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	if err := h.authProcessor.DeleteSignupRequest(id); err != nil {
		if writeProcessorError(w, err) {
			return
		}
		log.Printf("admin: failed to delete signup request id=%d: %s", id, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	log.Printf("admin: rejected signup request id=%d", id)
	w.WriteHeader(http.StatusNoContent)
}
