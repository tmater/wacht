package server

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/tmater/wacht/internal/alert"
	"github.com/tmater/wacht/internal/proto"
	"github.com/tmater/wacht/internal/store"
)

type fakeProbeResultProcessor struct {
	processFn func(probe *store.Probe, incoming proto.CheckResult) (ProbeResultOutcome, error)
}

func (f fakeProbeResultProcessor) Process(probe *store.Probe, incoming proto.CheckResult) (ProbeResultOutcome, error) {
	return f.processFn(probe, incoming)
}

func TestHandleResultMapsBadRequestError(t *testing.T) {
	h := &Handler{
		webhooks: alert.NewSender(),
		resultProcessor: fakeProbeResultProcessor{
			processFn: func(probe *store.Probe, incoming proto.CheckResult) (ProbeResultOutcome, error) {
				return ProbeResultOutcome{}, &badRequestError{message: "unknown check_id"}
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

func TestHandleResultMapsInternalProcessorError(t *testing.T) {
	h := &Handler{
		webhooks: alert.NewSender(),
		resultProcessor: fakeProbeResultProcessor{
			processFn: func(probe *store.Probe, incoming proto.CheckResult) (ProbeResultOutcome, error) {
				return ProbeResultOutcome{}, errors.New("boom")
			},
		},
	}
	defer h.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/results", bytes.NewBufferString(`{"check_id":"site"}`))
	req = req.WithContext(context.WithValue(req.Context(), contextKeyProbe, &store.Probe{ProbeID: "probe-1"}))
	rec := httptest.NewRecorder()

	h.handleResult(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if body := rec.Body.String(); body != "internal error\n" {
		t.Fatalf("body = %q, want internal error", body)
	}
}

func TestHandleResultReturnsNoContentOnProcessorSuccess(t *testing.T) {
	h := &Handler{
		webhooks: alert.NewSender(),
		resultProcessor: fakeProbeResultProcessor{
			processFn: func(probe *store.Probe, incoming proto.CheckResult) (ProbeResultOutcome, error) {
				return ProbeResultOutcome{}, nil
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
