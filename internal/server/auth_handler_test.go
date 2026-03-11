package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/tmater/wacht/internal/store"
)

type fakeAuthProcessor struct {
	loginFn                     func(req LoginRequest) (LoginOutcome, error)
	changePasswordFn            func(user *store.User, req ChangePasswordRequest) error
	requestAccessFn             func(req RequestAccessRequest) error
	listPendingSignupRequestsFn func() ([]store.SignupRequest, error)
	approveSignupRequestFn      func(id int64) (SignupApprovalOutcome, error)
	deleteSignupRequestFn       func(id int64) error
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

func (f fakeAuthProcessor) DeleteSignupRequest(id int64) error {
	return f.deleteSignupRequestFn(id)
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
			deleteSignupRequestFn:       func(id int64) error { return nil },
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
			deleteSignupRequestFn:       func(id int64) error { return nil },
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
			deleteSignupRequestFn:       func(id int64) error { return nil },
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
			deleteSignupRequestFn:       func(id int64) error { return nil },
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

func TestHandleRequestAccessMapsBadRequestError(t *testing.T) {
	h := &Handler{
		authProcessor: fakeAuthProcessor{
			loginFn:          func(req LoginRequest) (LoginOutcome, error) { return LoginOutcome{}, nil },
			changePasswordFn: func(user *store.User, req ChangePasswordRequest) error { return nil },
			requestAccessFn: func(req RequestAccessRequest) error {
				return &badRequestError{message: "email is required"}
			},
			listPendingSignupRequestsFn: func() ([]store.SignupRequest, error) { return nil, nil },
			approveSignupRequestFn:      func(id int64) (SignupApprovalOutcome, error) { return SignupApprovalOutcome{}, nil },
			deleteSignupRequestFn:       func(id int64) error { return nil },
		},
	}

	req := httptest.NewRequest(http.MethodPost, "/api/auth/request-access", bytes.NewBufferString(`{"email":""}`))
	rec := httptest.NewRecorder()

	h.handleRequestAccess(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if body := rec.Body.String(); body != "email is required\n" {
		t.Fatalf("body = %q, want email is required", body)
	}
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
			deleteSignupRequestFn: func(id int64) error { return nil },
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
