package server

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tmater/wacht/internal/alert"
	probeapi "github.com/tmater/wacht/internal/api/probe"
	"github.com/tmater/wacht/internal/network"
	"github.com/tmater/wacht/internal/proto"
	"github.com/tmater/wacht/internal/store"
)

type fakeProbeProcessor struct {
	heartbeatFn func(probe *store.Probe, req probeapi.HeartbeatRequest) error
	registerFn  func(probe *store.Probe, req probeapi.RegisterRequest) error
	processFn   func(probe *store.Probe, incoming proto.CheckResult) error
}

func (f fakeProbeProcessor) Heartbeat(probe *store.Probe, req probeapi.HeartbeatRequest) error {
	return f.heartbeatFn(probe, req)
}

func (f fakeProbeProcessor) Register(probe *store.Probe, req probeapi.RegisterRequest) error {
	return f.registerFn(probe, req)
}

func (f fakeProbeProcessor) Process(probe *store.Probe, incoming proto.CheckResult) error {
	return f.processFn(probe, incoming)
}

func TestHandleHeartbeatMapsBadRequestError(t *testing.T) {
	h := &Handler{
		probeProcessor: fakeProbeProcessor{
			heartbeatFn: func(probe *store.Probe, req probeapi.HeartbeatRequest) error {
				return &badRequestError{message: "probe_id does not match authenticated probe"}
			},
			registerFn: func(probe *store.Probe, req probeapi.RegisterRequest) error { return nil },
			processFn: func(probe *store.Probe, incoming proto.CheckResult) error {
				return nil
			},
		},
	}

	req := httptest.NewRequest(http.MethodPost, "/api/probes/heartbeat", bytes.NewBufferString(`{"probe_id":"probe-2"}`))
	req = req.WithContext(context.WithValue(req.Context(), contextKeyProbe, &store.Probe{ProbeID: "probe-1"}))
	rec := httptest.NewRecorder()

	h.handleHeartbeat(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if body := rec.Body.String(); body != "probe_id does not match authenticated probe\n" {
		t.Fatalf("body = %q, want bad request message", body)
	}
}

func TestHandleProbeRegisterMapsInternalError(t *testing.T) {
	h := &Handler{
		probeProcessor: fakeProbeProcessor{
			heartbeatFn: func(probe *store.Probe, req probeapi.HeartbeatRequest) error { return nil },
			registerFn: func(probe *store.Probe, req probeapi.RegisterRequest) error {
				return errors.New("boom")
			},
			processFn: func(probe *store.Probe, incoming proto.CheckResult) error {
				return nil
			},
		},
	}

	req := httptest.NewRequest(http.MethodPost, "/api/probes/register", bytes.NewBufferString(`{"probe_id":"probe-1","version":"v1.0.0"}`))
	req = req.WithContext(context.WithValue(req.Context(), contextKeyProbe, &store.Probe{ProbeID: "probe-1"}))
	rec := httptest.NewRecorder()

	h.handleProbeRegister(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if body := rec.Body.String(); body != "internal error\n" {
		t.Fatalf("body = %q, want internal error", body)
	}
}

func TestHandleResultMapsBadRequestError(t *testing.T) {
	h := &Handler{
		webhooks: alert.NewSender(nil, network.Policy{}),
		probeProcessor: fakeProbeProcessor{
			heartbeatFn: func(probe *store.Probe, req probeapi.HeartbeatRequest) error { return nil },
			registerFn:  func(probe *store.Probe, req probeapi.RegisterRequest) error { return nil },
			processFn: func(probe *store.Probe, incoming proto.CheckResult) error {
				return &badRequestError{message: "unknown check_id"}
			},
		},
	}
	defer h.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/results", bytes.NewBufferString(`{"check_id":"missing"}`))
	req = req.WithContext(context.WithValue(req.Context(), contextKeyProbe, &store.Probe{ProbeID: "probe-1"}))
	rec := httptest.NewRecorder()

	h.handleResult(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if body := rec.Body.String(); body != "unknown check_id\n" {
		t.Fatalf("body = %q, want bad request message", body)
	}
}

func TestHandleResultReturnsNoContentOnProcessorSuccess(t *testing.T) {
	h := &Handler{
		webhooks: alert.NewSender(nil, network.Policy{}),
		probeProcessor: fakeProbeProcessor{
			heartbeatFn: func(probe *store.Probe, req probeapi.HeartbeatRequest) error { return nil },
			registerFn:  func(probe *store.Probe, req probeapi.RegisterRequest) error { return nil },
			processFn: func(probe *store.Probe, incoming proto.CheckResult) error {
				return nil
			},
		},
	}
	defer h.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/results", bytes.NewBufferString(`{"check_id":"site","up":true}`))
	req = req.WithContext(context.WithValue(req.Context(), contextKeyProbe, &store.Probe{ProbeID: "probe-1"}))
	rec := httptest.NewRecorder()

	h.handleResult(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
}

func TestHandleProbeRegisterRejectsTooLargeBody(t *testing.T) {
	h := &Handler{
		probeProcessor: fakeProbeProcessor{
			heartbeatFn: func(probe *store.Probe, req probeapi.HeartbeatRequest) error { return nil },
			registerFn: func(probe *store.Probe, req probeapi.RegisterRequest) error {
				t.Fatal("Register should not be called for an oversized body")
				return nil
			},
			processFn: func(probe *store.Probe, incoming proto.CheckResult) error {
				return nil
			},
		},
	}

	body := `{"version":"` + strings.Repeat("x", int(maxProbeJSONRequestBodyBytes)) + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/probes/register", bytes.NewBufferString(body))
	req = req.WithContext(context.WithValue(req.Context(), contextKeyProbe, &store.Probe{ProbeID: "probe-1"}))
	rec := httptest.NewRecorder()

	h.handleProbeRegister(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", rec.Code)
	}
	if body := rec.Body.String(); body != "request body too large\n" {
		t.Fatalf("body = %q, want request body too large", body)
	}
}
