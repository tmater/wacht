package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/tmater/wacht/internal/store"
)

type fakeAuthProcessor struct {
	loginFn                     func(req LoginRequest) (LoginOutcome, error)
	changePasswordFn            func(user *store.User, req ChangePasswordRequest) error
	requestAccessFn             func(req RequestAccessRequest) error
	listPendingSignupRequestsFn func() ([]store.SignupRequest, error)
	approveSignupRequestFn      func(id int64) (SignupApprovalOutcome, error)
	rejectSignupRequestFn       func(id int64) error
	setupPasswordFn             func(req SetupPasswordRequest) (SetupPasswordOutcome, error)
}

func (f fakeAuthProcessor) Login(req LoginRequest) (LoginOutcome, error) {
	return f.loginFn(req)
}

func (f fakeAuthProcessor) ChangePassword(user *store.User, req ChangePasswordRequest) error {
	return f.changePasswordFn(user, req)
}

func (f fakeAuthProcessor) RequestAccess(req RequestAccessRequest) error {
	return f.requestAccessFn(req)
}

func (f fakeAuthProcessor) ListPendingSignupRequests() ([]store.SignupRequest, error) {
	return f.listPendingSignupRequestsFn()
}

func (f fakeAuthProcessor) ApproveSignupRequest(id int64) (SignupApprovalOutcome, error) {
	return f.approveSignupRequestFn(id)
}

func (f fakeAuthProcessor) RejectSignupRequest(id int64) error {
	return f.rejectSignupRequestFn(id)
}

func (f fakeAuthProcessor) SetupPassword(req SetupPasswordRequest) (SetupPasswordOutcome, error) {
	return f.setupPasswordFn(req)
}

func TestHandleLoginMapsUnauthorizedError(t *testing.T) {
	h := &Handler{
		authProcessor: fakeAuthProcessor{
			loginFn: func(req LoginRequest) (LoginOutcome, error) {
				return LoginOutcome{}, &unauthorizedError{message: "invalid credentials"}
			},
			changePasswordFn:            func(user *store.User, req ChangePasswordRequest) error { return nil },
			requestAccessFn:             func(req RequestAccessRequest) error { return nil },
			listPendingSignupRequestsFn: func() ([]store.SignupRequest, error) { return nil, nil },
			approveSignupRequestFn:      func(id int64) (SignupApprovalOutcome, error) { return SignupApprovalOutcome{}, nil },
			rejectSignupRequestFn:       func(id int64) error { return nil },
			setupPasswordFn:             func(req SetupPasswordRequest) (SetupPasswordOutcome, error) { return SetupPasswordOutcome{}, nil },
		},
	}

	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewBufferString(`{"email":"alice@example.com","password":"wrong"}`))
	rec := httptest.NewRecorder()

	h.handleLogin(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if body := rec.Body.String(); body != "invalid credentials\n" {
		t.Fatalf("body = %q, want invalid credentials", body)
	}
}

func TestHandleLoginReturnsJSONOnSuccess(t *testing.T) {
	h := &Handler{
		authProcessor: fakeAuthProcessor{
			loginFn: func(req LoginRequest) (LoginOutcome, error) {
				return LoginOutcome{Token: "token-123", Email: "alice@example.com"}, nil
			},
			changePasswordFn:            func(user *store.User, req ChangePasswordRequest) error { return nil },
			requestAccessFn:             func(req RequestAccessRequest) error { return nil },
			listPendingSignupRequestsFn: func() ([]store.SignupRequest, error) { return nil, nil },
			approveSignupRequestFn:      func(id int64) (SignupApprovalOutcome, error) { return SignupApprovalOutcome{}, nil },
			rejectSignupRequestFn:       func(id int64) error { return nil },
			setupPasswordFn:             func(req SetupPasswordRequest) (SetupPasswordOutcome, error) { return SetupPasswordOutcome{}, nil },
		},
	}

	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewBufferString(`{"email":"alice@example.com","password":"secret"}`))
	rec := httptest.NewRecorder()

	h.handleLogin(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if body["token"] != "token-123" || body["email"] != "alice@example.com" {
		t.Fatalf("body = %#v, want token/email payload", body)
	}
}

func TestHandleChangePasswordMapsBadRequestError(t *testing.T) {
	h := &Handler{
		authProcessor: fakeAuthProcessor{
			loginFn: func(req LoginRequest) (LoginOutcome, error) { return LoginOutcome{}, nil },
			changePasswordFn: func(user *store.User, req ChangePasswordRequest) error {
				return &badRequestError{message: "current_password and new_password are required"}
			},
			requestAccessFn:             func(req RequestAccessRequest) error { return nil },
			listPendingSignupRequestsFn: func() ([]store.SignupRequest, error) { return nil, nil },
			approveSignupRequestFn:      func(id int64) (SignupApprovalOutcome, error) { return SignupApprovalOutcome{}, nil },
			rejectSignupRequestFn:       func(id int64) error { return nil },
			setupPasswordFn:             func(req SetupPasswordRequest) (SetupPasswordOutcome, error) { return SetupPasswordOutcome{}, nil },
		},
	}

	req := httptest.NewRequest(http.MethodPut, "/api/auth/change-password", bytes.NewBufferString(`{"current_password":"","new_password":""}`))
	req = req.WithContext(context.WithValue(req.Context(), contextKeyUser, &store.User{ID: 4}))
	rec := httptest.NewRecorder()

	h.handleChangePassword(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if body := rec.Body.String(); body != "current_password and new_password are required\n" {
		t.Fatalf("body = %q, want bad request message", body)
	}
}

func TestHandleChangePasswordMapsInternalError(t *testing.T) {
	h := &Handler{
		authProcessor: fakeAuthProcessor{
			loginFn: func(req LoginRequest) (LoginOutcome, error) { return LoginOutcome{}, nil },
			changePasswordFn: func(user *store.User, req ChangePasswordRequest) error {
				return errors.New("boom")
			},
			requestAccessFn:             func(req RequestAccessRequest) error { return nil },
			listPendingSignupRequestsFn: func() ([]store.SignupRequest, error) { return nil, nil },
			approveSignupRequestFn:      func(id int64) (SignupApprovalOutcome, error) { return SignupApprovalOutcome{}, nil },
			rejectSignupRequestFn:       func(id int64) error { return nil },
			setupPasswordFn:             func(req SetupPasswordRequest) (SetupPasswordOutcome, error) { return SetupPasswordOutcome{}, nil },
		},
	}

	req := httptest.NewRequest(http.MethodPut, "/api/auth/change-password", bytes.NewBufferString(`{"current_password":"old","new_password":"new"}`))
	req = req.WithContext(context.WithValue(req.Context(), contextKeyUser, &store.User{ID: 4}))
	rec := httptest.NewRecorder()

	h.handleChangePassword(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if body := rec.Body.String(); body != "internal error\n" {
		t.Fatalf("body = %q, want internal error", body)
	}
}

func TestHandleRequestAccessAlwaysReturnsOK(t *testing.T) {
	h := &Handler{
		authProcessor: fakeAuthProcessor{
			loginFn:                     func(req LoginRequest) (LoginOutcome, error) { return LoginOutcome{}, nil },
			changePasswordFn:            func(user *store.User, req ChangePasswordRequest) error { return nil },
			requestAccessFn:             func(req RequestAccessRequest) error { return errors.New("boom") },
			listPendingSignupRequestsFn: func() ([]store.SignupRequest, error) { return nil, nil },
			approveSignupRequestFn:      func(id int64) (SignupApprovalOutcome, error) { return SignupApprovalOutcome{}, nil },
			rejectSignupRequestFn:       func(id int64) error { return nil },
			setupPasswordFn:             func(req SetupPasswordRequest) (SetupPasswordOutcome, error) { return SetupPasswordOutcome{}, nil },
		},
	}

	req := httptest.NewRequest(http.MethodPost, "/api/auth/request-access", bytes.NewBufferString(`{"email":""}`))
	rec := httptest.NewRecorder()

	h.handleRequestAccess(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestHandleSetupPasswordReturnsJSONOnSuccess(t *testing.T) {
	h := &Handler{
		authProcessor: fakeAuthProcessor{
			loginFn:                     func(req LoginRequest) (LoginOutcome, error) { return LoginOutcome{}, nil },
			changePasswordFn:            func(user *store.User, req ChangePasswordRequest) error { return nil },
			requestAccessFn:             func(req RequestAccessRequest) error { return nil },
			listPendingSignupRequestsFn: func() ([]store.SignupRequest, error) { return nil, nil },
			approveSignupRequestFn:      func(id int64) (SignupApprovalOutcome, error) { return SignupApprovalOutcome{}, nil },
			rejectSignupRequestFn:       func(id int64) error { return nil },
			setupPasswordFn: func(req SetupPasswordRequest) (SetupPasswordOutcome, error) {
				return SetupPasswordOutcome{Token: "session-123", Email: "alice@example.com"}, nil
			},
		},
	}

	req := httptest.NewRequest(http.MethodPost, "/api/auth/setup-password", bytes.NewBufferString(`{"token":"setup","new_password":"secret"}`))
	rec := httptest.NewRecorder()

	h.handleSetupPassword(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if body["token"] != "session-123" || body["email"] != "alice@example.com" {
		t.Fatalf("body = %#v, want setup-password token/email payload", body)
	}
}

func TestRateLimitedUsesDistinctDirectClientIPs(t *testing.T) {
	h := &Handler{loginLimiter: newRateLimiter(1, time.Minute)}
	limited := h.rateLimited(h.loginLimiter, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	req1 := httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
	req1.RemoteAddr = "198.51.100.10:1234"
	rec1 := httptest.NewRecorder()
	limited(rec1, req1)
	if rec1.Code != http.StatusNoContent {
		t.Fatalf("first status = %d, want 204", rec1.Code)
	}

	req2 := httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
	req2.RemoteAddr = "198.51.100.11:1234"
	rec2 := httptest.NewRecorder()
	limited(rec2, req2)
	if rec2.Code != http.StatusNoContent {
		t.Fatalf("second status = %d, want 204", rec2.Code)
	}

	req3 := httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
	req3.RemoteAddr = "198.51.100.10:9999"
	rec3 := httptest.NewRecorder()
	limited(rec3, req3)
	if rec3.Code != http.StatusTooManyRequests {
		t.Fatalf("third status = %d, want 429", rec3.Code)
	}
}

func TestRateLimitedUsesForwardedClientIPFromTrustedProxy(t *testing.T) {
	h := &Handler{
		loginLimiter:   newRateLimiter(1, time.Minute),
		trustedProxies: mustPrefixes(t, "172.16.0.0/12"),
	}
	limited := h.rateLimited(h.loginLimiter, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	req1 := httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
	req1.RemoteAddr = "172.18.0.2:1234"
	req1.Header.Set("X-Forwarded-For", "198.51.100.10")
	rec1 := httptest.NewRecorder()
	limited(rec1, req1)
	if rec1.Code != http.StatusNoContent {
		t.Fatalf("first status = %d, want 204", rec1.Code)
	}

	req2 := httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
	req2.RemoteAddr = "172.18.0.2:1234"
	req2.Header.Set("X-Forwarded-For", "198.51.100.11")
	rec2 := httptest.NewRecorder()
	limited(rec2, req2)
	if rec2.Code != http.StatusNoContent {
		t.Fatalf("second status = %d, want 204", rec2.Code)
	}

	req3 := httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
	req3.RemoteAddr = "172.18.0.2:9876"
	req3.Header.Set("X-Forwarded-For", "198.51.100.10")
	rec3 := httptest.NewRecorder()
	limited(rec3, req3)
	if rec3.Code != http.StatusTooManyRequests {
		t.Fatalf("third status = %d, want 429", rec3.Code)
	}
}

func TestRateLimitedUsesRightmostUntrustedForwardedIP(t *testing.T) {
	h := &Handler{
		loginLimiter:   newRateLimiter(1, time.Minute),
		trustedProxies: mustPrefixes(t, "172.16.0.0/12"),
	}
	limited := h.rateLimited(h.loginLimiter, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	req1 := httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
	req1.RemoteAddr = "172.18.0.2:1234"
	req1.Header.Set("X-Forwarded-For", "203.0.113.9, 198.51.100.10")
	rec1 := httptest.NewRecorder()
	limited(rec1, req1)
	if rec1.Code != http.StatusNoContent {
		t.Fatalf("first status = %d, want 204", rec1.Code)
	}

	req2 := httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
	req2.RemoteAddr = "172.18.0.2:4321"
	req2.Header.Set("X-Forwarded-For", "198.51.100.10")
	rec2 := httptest.NewRecorder()
	limited(rec2, req2)
	if rec2.Code != http.StatusTooManyRequests {
		t.Fatalf("second status = %d, want 429", rec2.Code)
	}
}

func TestRateLimitedIgnoresForwardedHeadersFromUntrustedPeer(t *testing.T) {
	h := &Handler{
		loginLimiter:   newRateLimiter(1, time.Minute),
		trustedProxies: mustPrefixes(t, "172.16.0.0/12"),
	}
	limited := h.rateLimited(h.loginLimiter, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	req1 := httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
	req1.RemoteAddr = "198.51.100.77:1234"
	req1.Header.Set("X-Forwarded-For", "203.0.113.10")
	rec1 := httptest.NewRecorder()
	limited(rec1, req1)
	if rec1.Code != http.StatusNoContent {
		t.Fatalf("first status = %d, want 204", rec1.Code)
	}

	req2 := httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
	req2.RemoteAddr = "198.51.100.77:9876"
	req2.Header.Set("X-Forwarded-For", "203.0.113.11")
	rec2 := httptest.NewRecorder()
	limited(rec2, req2)
	if rec2.Code != http.StatusTooManyRequests {
		t.Fatalf("second status = %d, want 429", rec2.Code)
	}
}

func mustPrefixes(t *testing.T, cidrs ...string) []netip.Prefix {
	t.Helper()
	prefixes := make([]netip.Prefix, 0, len(cidrs))
	for _, cidr := range cidrs {
		prefix, err := netip.ParsePrefix(cidr)
		if err != nil {
			t.Fatalf("ParsePrefix(%q): %v", cidr, err)
		}
		prefixes = append(prefixes, prefix.Masked())
	}
	return prefixes
}

func TestHandleApproveSignupRequestMapsNotFoundError(t *testing.T) {
	h := &Handler{
		authProcessor: fakeAuthProcessor{
			loginFn:                     func(req LoginRequest) (LoginOutcome, error) { return LoginOutcome{}, nil },
			changePasswordFn:            func(user *store.User, req ChangePasswordRequest) error { return nil },
			requestAccessFn:             func(req RequestAccessRequest) error { return nil },
			listPendingSignupRequestsFn: func() ([]store.SignupRequest, error) { return nil, nil },
			approveSignupRequestFn: func(id int64) (SignupApprovalOutcome, error) {
				return SignupApprovalOutcome{}, &notFoundError{message: "request not found or already processed"}
			},
			rejectSignupRequestFn: func(id int64) error { return nil },
			setupPasswordFn:       func(req SetupPasswordRequest) (SetupPasswordOutcome, error) { return SetupPasswordOutcome{}, nil },
		},
	}

	req := httptest.NewRequest(http.MethodPost, "/api/admin/signup-requests/42/approve", nil)
	req.SetPathValue("id", "42")
	rec := httptest.NewRecorder()

	h.handleApproveSignupRequest(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if body := rec.Body.String(); body != "request not found or already processed\n" {
		t.Fatalf("body = %q, want not found message", body)
	}
}

func TestHandleApproveSignupRequestReturnsSetupToken(t *testing.T) {
	h := &Handler{
		authProcessor: fakeAuthProcessor{
			loginFn:                     func(req LoginRequest) (LoginOutcome, error) { return LoginOutcome{}, nil },
			changePasswordFn:            func(user *store.User, req ChangePasswordRequest) error { return nil },
			requestAccessFn:             func(req RequestAccessRequest) error { return nil },
			listPendingSignupRequestsFn: func() ([]store.SignupRequest, error) { return nil, nil },
			approveSignupRequestFn: func(id int64) (SignupApprovalOutcome, error) {
				return SignupApprovalOutcome{
					Email:      "alice@example.com",
					SetupToken: "setup-token",
					ExpiresAt:  time.Date(2026, time.March, 16, 12, 0, 0, 0, time.UTC),
				}, nil
			},
			rejectSignupRequestFn: func(id int64) error { return nil },
			setupPasswordFn:       func(req SetupPasswordRequest) (SetupPasswordOutcome, error) { return SetupPasswordOutcome{}, nil },
		},
	}

	req := httptest.NewRequest(http.MethodPost, "/api/admin/signup-requests/42/approve", nil)
	req.SetPathValue("id", "42")
	rec := httptest.NewRecorder()

	h.handleApproveSignupRequest(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if body["setup_token"] != "setup-token" || body["email"] != "alice@example.com" {
		t.Fatalf("body = %#v, want setup token/email payload", body)
	}
	if body["expires_at"] != "2026-03-16T12:00:00Z" {
		t.Fatalf("expires_at = %q, want 2026-03-16T12:00:00Z", body["expires_at"])
	}
}

func TestHandleLoginRejectsTooLargeBody(t *testing.T) {
	h := &Handler{
		authProcessor: fakeAuthProcessor{
			loginFn: func(req LoginRequest) (LoginOutcome, error) {
				t.Fatal("Login should not be called for an oversized body")
				return LoginOutcome{}, nil
			},
			changePasswordFn:            func(user *store.User, req ChangePasswordRequest) error { return nil },
			requestAccessFn:             func(req RequestAccessRequest) error { return nil },
			listPendingSignupRequestsFn: func() ([]store.SignupRequest, error) { return nil, nil },
			approveSignupRequestFn:      func(id int64) (SignupApprovalOutcome, error) { return SignupApprovalOutcome{}, nil },
			rejectSignupRequestFn:       func(id int64) error { return nil },
			setupPasswordFn:             func(req SetupPasswordRequest) (SetupPasswordOutcome, error) { return SetupPasswordOutcome{}, nil },
		},
	}

	body := `{"email":"alice@example.com","password":"` + strings.Repeat("x", int(maxJSONRequestBodyBytes)) + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewBufferString(body))
	rec := httptest.NewRecorder()

	h.handleLogin(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", rec.Code)
	}
	if got := rec.Body.String(); got != "request body too large\n" {
		t.Fatalf("body = %q, want request body too large", got)
	}
}
