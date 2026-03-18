package server

import (
	"bytes"
	"io"
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

func TestStatusRecorderPreservesFlushViaResponseController(t *testing.T) {
	base := &flushRecorder{ResponseRecorder: httptest.NewRecorder()}
	rec := &statusRecorder{ResponseWriter: base}

	if err := http.NewResponseController(rec).Flush(); err != nil {
		t.Fatalf("Flush() error = %v", err)
	}
	if !base.flushed {
		t.Fatal("expected underlying writer to be flushed")
	}
	if rec.Unwrap() != base {
		t.Fatal("expected Unwrap to return the underlying writer")
	}
}

func TestWithRequestLogPreservesConnectionCloseOnTooLargeBody(t *testing.T) {
	handler := withRequestLog(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		err := decodeJSONBody(w, r, &payload, 8, false)
		if err == nil {
			http.Error(w, "expected oversized body to fail decoding", http.StatusInternalServerError)
			return
		}
		if !writeProcessorError(w, err) {
			http.Error(w, "expected writeProcessorError to handle oversized body", http.StatusInternalServerError)
			return
		}
	}))

	server := httptest.NewServer(handler)
	defer server.Close()

	res, err := http.Post(server.URL, "application/json", strings.NewReader(`{"payload":"way too large"}`))
	if err != nil {
		t.Fatalf("Post() error = %v", err)
	}
	defer res.Body.Close()
	io.Copy(io.Discard, res.Body)

	if res.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", res.StatusCode)
	}
	if !res.Close {
		t.Fatalf("res.Close = false, want true after oversized body")
	}
}

type flushRecorder struct {
	*httptest.ResponseRecorder
	flushed bool
}

func (r *flushRecorder) Flush() {
	r.flushed = true
	r.ResponseRecorder.Flush()
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
