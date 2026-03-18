package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"strings"
	"time"

	probeapi "github.com/tmater/wacht/internal/api/probe"
	"github.com/tmater/wacht/internal/store"
)

const (
	requestIDHeader                 = "X-Request-ID"
	contextKeyLogger     contextKey = "logger"
	contextKeyRequestLog contextKey = "request_log"
)

type requestLogScope struct {
	requestID string
	userID    int64
	hasUser   bool
	probeID   string
}

type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	n, err := r.ResponseWriter.Write(b)
	r.bytes += n
	return n, err
}

func withRequestLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := normalizeRequestID(r.Header.Get(requestIDHeader))
		if requestID == "" {
			requestID = newRequestID()
		}

		scope := &requestLogScope{requestID: requestID}
		logger := slog.Default().With("request_id", requestID)
		ctx := context.WithValue(r.Context(), contextKeyRequestLog, scope)
		ctx = context.WithValue(ctx, contextKeyLogger, logger)
		r = r.WithContext(ctx)

		w.Header().Set(requestIDHeader, requestID)

		rec := &statusRecorder{ResponseWriter: w}
		start := time.Now()
		next.ServeHTTP(rec, r)

		if rec.status == 0 {
			rec.status = http.StatusOK
		}

		args := []any{
			"component", "http",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"duration_ms", time.Since(start).Milliseconds(),
			"bytes", rec.bytes,
		}
		if scope.hasUser {
			args = append(args, "user_id", scope.userID)
		}
		if scope.probeID != "" {
			args = append(args, "probe_id", scope.probeID)
		}

		logger.Log(r.Context(), requestLogLevel(r.URL.Path, rec.status), "request completed", args...)
	})
}

func requestLogger(r *http.Request) *slog.Logger {
	if r == nil {
		return slog.Default()
	}
	logger, _ := r.Context().Value(contextKeyLogger).(*slog.Logger)
	if logger == nil {
		return slog.Default()
	}
	return logger
}

func attachAuthenticatedUser(r *http.Request, user *store.User) *http.Request {
	if r == nil || user == nil {
		return r
	}
	if scope := requestLogFromContext(r.Context()); scope != nil {
		scope.userID = user.ID
		scope.hasUser = true
	}
	return withRequestLogger(r, requestLogger(r).With("user_id", user.ID))
}

func attachAuthenticatedProbe(r *http.Request, probe *store.Probe) *http.Request {
	if r == nil || probe == nil {
		return r
	}
	if scope := requestLogFromContext(r.Context()); scope != nil {
		scope.probeID = probe.ProbeID
	}
	return withRequestLogger(r, requestLogger(r).With("probe_id", probe.ProbeID))
}

func withRequestLogger(r *http.Request, logger *slog.Logger) *http.Request {
	if r == nil || logger == nil {
		return r
	}
	ctx := context.WithValue(r.Context(), contextKeyLogger, logger)
	return r.WithContext(ctx)
}

func requestLogFromContext(ctx context.Context) *requestLogScope {
	scope, _ := ctx.Value(contextKeyRequestLog).(*requestLogScope)
	return scope
}

func requestLogLevel(path string, status int) slog.Level {
	switch {
	case status >= http.StatusInternalServerError:
		return slog.LevelError
	case status >= http.StatusBadRequest:
		return slog.LevelWarn
	case path == probeapi.PathResults || strings.HasPrefix(path, "/api/probes/"):
		return slog.LevelDebug
	default:
		return slog.LevelInfo
	}
}

func normalizeRequestID(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > 64 {
		return ""
	}
	for _, r := range raw {
		if r >= 'a' && r <= 'z' {
			continue
		}
		if r >= 'A' && r <= 'Z' {
			continue
		}
		if r >= '0' && r <= '9' {
			continue
		}
		switch r {
		case '-', '_', '.':
			continue
		default:
			return ""
		}
	}
	return raw
}

func newRequestID() string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "req-fallback"
	}
	return "req-" + hex.EncodeToString(b[:])
}
