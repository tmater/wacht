package server

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tmater/wacht/internal/logx"
	"github.com/tmater/wacht/internal/store"
)

func TestWithRequestLogUsesIncomingRequestIDAndCapturesUser(t *testing.T) {
	var buf bytes.Buffer
	restoreDefaultLogger(t, logx.New("test-service", &buf, "debug"))

	handler := withRequestLog(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r = attachAuthenticatedUser(r, &store.User{ID: 42})
		w.WriteHeader(http.StatusCreated)
	}))

	req := httptest.NewRequest(http.MethodPost, "/api/checks", nil)
	req.Header.Set(requestIDHeader, "req-user-123")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if got := rec.Header().Get(requestIDHeader); got != "req-user-123" {
		t.Fatalf("X-Request-ID = %q, want req-user-123", got)
	}

	line := lastLogLine(t, buf.String())
	assertLogContains(t, line, `level=INFO`)
	assertLogContains(t, line, `msg="request completed"`)
	assertLogContains(t, line, `request_id=req-user-123`)
	assertLogContains(t, line, `method=POST`)
	assertLogContains(t, line, `path=/api/checks`)
	assertLogContains(t, line, `status=201`)
	assertLogContains(t, line, `user_id=42`)
}

func TestWithRequestLogGeneratesRequestIDWhenHeaderIsInvalid(t *testing.T) {
	var buf bytes.Buffer
	restoreDefaultLogger(t, logx.New("test-service", &buf, "debug"))

	handler := withRequestLog(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r = attachAuthenticatedProbe(r, &store.Probe{ProbeID: "probe-1"})
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodPost, "/api/results", nil)
	req.Header.Set(requestIDHeader, "bad request id")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	requestID := rec.Header().Get(requestIDHeader)
	if !strings.HasPrefix(requestID, "req-") {
		t.Fatalf("X-Request-ID = %q, want generated req-* value", requestID)
	}

	line := lastLogLine(t, buf.String())
	assertLogContains(t, line, `level=DEBUG`)
	assertLogContains(t, line, `probe_id=probe-1`)
	assertLogContains(t, line, `request_id=`+requestID)
}

func restoreDefaultLogger(t *testing.T, logger *slog.Logger) {
	t.Helper()
	previous := slog.Default()
	slog.SetDefault(logger)
	t.Cleanup(func() {
		slog.SetDefault(previous)
	})
}

func lastLogLine(t *testing.T, output string) string {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) == 0 || lines[0] == "" {
		t.Fatal("expected log output")
	}
	return lines[len(lines)-1]
}

func assertLogContains(t *testing.T, line, want string) {
	t.Helper()
	if !strings.Contains(line, want) {
		t.Fatalf("log line %q does not contain %q", line, want)
	}
}
