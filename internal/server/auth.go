package server

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/tmater/wacht/internal/store"
)

type contextKey string

const contextKeyUser contextKey = "user"

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
}

type tokenBucket struct {
	count   int
	resetAt time.Time
}

const (
	rateLimitRequests = 10
	rateLimitWindow   = time.Minute
)

func newRateLimiter() *rateLimiter {
	return &rateLimiter{tokens: make(map[string]*tokenBucket)}
}

func (rl *rateLimiter) allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	b, ok := rl.tokens[ip]
	if !ok || time.Now().After(b.resetAt) {
		rl.tokens[ip] = &tokenBucket{count: 1, resetAt: time.Now().Add(rateLimitWindow)}
		return true
	}
	if b.count >= rateLimitRequests {
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

// handleChangePassword verifies the current password and sets a new one.
func (h *Handler) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	user := sessionUser(r)
	var req struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if req.CurrentPassword == "" || req.NewPassword == "" {
		http.Error(w, "current_password and new_password are required", http.StatusBadRequest)
		return
	}
	ok, err := h.store.UpdateUserPassword(user.ID, req.CurrentPassword, req.NewPassword)
	if err != nil {
		log.Printf("auth: failed to update password user_id=%d: %s", user.ID, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if !ok {
		http.Error(w, "current password is incorrect", http.StatusUnauthorized)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
