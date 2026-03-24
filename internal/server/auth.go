package server

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"time"

	probeapi "github.com/tmater/wacht/internal/api/probe"
	"github.com/tmater/wacht/internal/config"
	"github.com/tmater/wacht/internal/logx"
	"github.com/tmater/wacht/internal/store"
)

type contextKey string

const (
	contextKeyUser  contextKey = "user"
	contextKeyProbe contextKey = "probe"
)

// requireProbeAuth authenticates an individual probe using its provisioned
// probe_id + secret and injects that probe into the request context.
func (h *Handler) requireProbeAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		logger := requestLogger(r)
		probeID := strings.TrimSpace(r.Header.Get(probeapi.HeaderProbeID))
		secret := strings.TrimSpace(r.Header.Get(probeapi.HeaderProbeSecret))
		if probeID == "" || secret == "" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		probe, err := h.store.AuthenticateProbe(probeID, secret)
		if err != nil {
			logger.Error("probe lookup failed", "component", "auth", "probe_id", probeID, "err", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		if probe == nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		ctx := context.WithValue(r.Context(), contextKeyProbe, probe)
		next.ServeHTTP(w, attachAuthenticatedProbe(r.WithContext(ctx), probe))
	})
}

// requireSession validates the Bearer token and injects the user into context.
func (h *Handler) requireSession(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		logger := requestLogger(r)
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if token == "" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		user, err := h.store.GetSessionUser(token)
		if err != nil {
			logger.Error("session lookup failed", "component", "auth", "err", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		if user == nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		ctx := context.WithValue(r.Context(), contextKeyUser, user)
		next(w, attachAuthenticatedUser(r.WithContext(ctx), user))
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

func (h *Handler) rateLimited(rl *rateLimiter, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !rl.allow(h.clientIP(r)) {
			http.Error(w, "too many requests", http.StatusTooManyRequests)
			return
		}
		next(w, r)
	}
}

func (h *Handler) clientIP(r *http.Request) string {
	peer, ok := parseRequestIP(r.RemoteAddr)
	if !ok {
		return r.RemoteAddr
	}
	if h == nil || !ipInPrefixes(peer, h.trustedProxies) {
		return peer.String()
	}
	if client, ok := forwardedClientIP(r, peer, h.trustedProxies); ok {
		return client.String()
	}
	return peer.String()
}

func forwardedClientIP(r *http.Request, peer netip.Addr, trusted []netip.Prefix) (netip.Addr, bool) {
	chain := parseForwardedFor(r.Header.Get("X-Forwarded-For"))
	if len(chain) == 0 {
		if realIP, ok := parseIP(r.Header.Get("X-Real-IP")); ok {
			chain = append(chain, realIP)
		}
	}
	chain = append(chain, peer)
	for i := len(chain) - 1; i >= 0; i-- {
		if !ipInPrefixes(chain[i], trusted) {
			return chain[i], true
		}
	}
	if len(chain) == 0 {
		return netip.Addr{}, false
	}
	return chain[0], true
}

func parseForwardedFor(header string) []netip.Addr {
	if header == "" {
		return nil
	}
	parts := strings.Split(header, ",")
	ips := make([]netip.Addr, 0, len(parts))
	for _, part := range parts {
		if ip, ok := parseIP(part); ok {
			ips = append(ips, ip)
		}
	}
	return ips
}

func parseRequestIP(remoteAddr string) (netip.Addr, bool) {
	host := remoteAddr
	if h, _, err := net.SplitHostPort(remoteAddr); err == nil {
		host = h
	}
	return parseIP(host)
}

func parseIP(raw string) (netip.Addr, bool) {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "[")
	raw = strings.TrimSuffix(raw, "]")
	if raw == "" {
		return netip.Addr{}, false
	}
	ip, err := netip.ParseAddr(raw)
	if err != nil {
		return netip.Addr{}, false
	}
	return ip.Unmap(), true
}

func ipInPrefixes(ip netip.Addr, prefixes []netip.Prefix) bool {
	ip = ip.Unmap()
	for _, prefix := range prefixes {
		if prefix.Contains(ip) {
			return true
		}
	}
	return false
}

// handleLogin authenticates a user and returns a session token.
func (h *Handler) handleLogin(w http.ResponseWriter, r *http.Request) {
	logger := requestLogger(r)
	var req LoginRequest
	if err := decodeJSONBody(w, r, &req, maxJSONRequestBodyBytes, false); err != nil {
		if writeProcessorError(w, err) {
			return
		}
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	outcome, err := h.authProcessor.Login(req)
	if err != nil {
		if writeProcessorError(w, err) {
			return
		}
		logger.Error("login failed", "component", "auth", "email_hash", logx.EmailHash(req.Email), "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"token": outcome.Token, "email": outcome.Email})
}

// handleLogout deletes the session token.
func (h *Handler) handleLogout(w http.ResponseWriter, r *http.Request) {
	logger := requestLogger(r)
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if token == "" {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err := h.store.DeleteSession(token); err != nil {
		logger.Error("delete session failed", "component", "auth", "err", err)
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleChangePassword verifies the current password and sets a new one.
func (h *Handler) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	user := sessionUser(r)
	logger := requestLogger(r)
	var req ChangePasswordRequest
	if err := decodeJSONBody(w, r, &req, maxJSONRequestBodyBytes, false); err != nil {
		if writeProcessorError(w, err) {
			return
		}
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if err := h.authProcessor.ChangePassword(user, req); err != nil {
		if writeProcessorError(w, err) {
			return
		}
		logger.Error("change password failed", "component", "auth", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleSetupPassword lets a user choose a password with a one-time setup token.
func (h *Handler) handleSetupPassword(w http.ResponseWriter, r *http.Request) {
	logger := requestLogger(r)
	var req SetupPasswordRequest
	if err := decodeJSONBody(w, r, &req, maxJSONRequestBodyBytes, false); err != nil {
		if writeProcessorError(w, err) {
			return
		}
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	outcome, err := h.authProcessor.SetupPassword(req)
	if err != nil {
		if writeProcessorError(w, err) {
			return
		}
		logger.Error("setup password failed", "component", "auth", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"token": outcome.Token, "email": outcome.Email})
}

// handleRequestAccess accepts a public email submission for signup.
// Always returns 200 OK to prevent email enumeration.
func (h *Handler) handleRequestAccess(w http.ResponseWriter, r *http.Request) {
	logger := requestLogger(r)
	var req RequestAccessRequest
	if err := decodeJSONBody(w, r, &req, maxJSONRequestBodyBytes, false); err != nil {
		if writeProcessorError(w, err) {
			return
		}
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if err := h.authProcessor.RequestAccess(req); err != nil {
		if writeProcessorError(w, err) {
			return
		}
		logger.Error("create signup request failed", "component", "signup", "email_hash", logx.EmailHash(req.Email), "err", err)
	} else {
		logger.Info("signup request received", "component", "signup", "email_hash", logx.EmailHash(req.Email))
	}
	w.WriteHeader(http.StatusOK)
}

// handleListSignupRequests returns all pending signup requests. Protected by requireAdmin.
func (h *Handler) handleListSignupRequests(w http.ResponseWriter, r *http.Request) {
	logger := requestLogger(r)
	reqs, err := h.authProcessor.ListPendingSignupRequests()
	if err != nil {
		if writeProcessorError(w, err) {
			return
		}
		logger.Error("list signup requests failed", "component", "admin", "err", err)
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
		logger.Warn("encode signup requests failed", "component", "admin", "err", err)
	}
}

// handleApproveSignupRequest approves a pending request and returns the generated
// one-time setup token. Protected by requireAdmin.
func (h *Handler) handleApproveSignupRequest(w http.ResponseWriter, r *http.Request) {
	logger := requestLogger(r)
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
		logger.Error("approve signup request failed", "component", "admin", "signup_request_id", id, "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	logger.Info("signup request approved", "component", "admin", "signup_request_id", id, "email_hash", logx.EmailHash(outcome.Email))
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]string{
		"email":       outcome.Email,
		"setup_token": outcome.SetupToken,
		"expires_at":  outcome.ExpiresAt.UTC().Format(time.RFC3339),
	}); err != nil {
		logger.Warn("encode approved signup request failed", "component", "admin", "signup_request_id", id, "err", err)
	}
}

// handleRejectSignupRequest marks a pending signup request rejected.
// Protected by requireAdmin.
func (h *Handler) handleRejectSignupRequest(w http.ResponseWriter, r *http.Request) {
	logger := requestLogger(r)
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	if err := h.authProcessor.RejectSignupRequest(id); err != nil {
		if writeProcessorError(w, err) {
			return
		}
		logger.Error("reject signup request failed", "component", "admin", "signup_request_id", id, "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	logger.Info("signup request rejected", "component", "admin", "signup_request_id", id)
	w.WriteHeader(http.StatusNoContent)
}
