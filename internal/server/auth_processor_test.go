package server

import (
	"errors"
	"testing"
	"time"

	"github.com/tmater/wacht/internal/store"
)

type fakeAuthStore struct {
	authenticateUserFn          func(email, password string) (*store.User, error)
	createSessionFn             func(userID int64) (string, error)
	updateUserPasswordFn        func(userID int64, currentPassword, newPassword string) (bool, error)
	createSignupRequestFn       func(email string) error
	listPendingSignupRequestsFn func() ([]store.SignupRequest, error)
	approveSignupRequestFn      func(id int64) (store.SignupApproval, error)
	rejectSignupRequestFn       func(id int64) (bool, error)
	setupPasswordFn             func(token, newPassword string) (store.SetupPasswordOutcome, error)
	authEmail                   string
	authPassword                string
	sessionUserID               int64
	passwordUserID              int64
	currentPassword             string
	newPassword                 string
	signupEmail                 string
	signupRequestID             int64
	setupToken                  string
}

func (f *fakeAuthStore) AuthenticateUser(email, password string) (*store.User, error) {
	f.authEmail = email
	f.authPassword = password
	if f.authenticateUserFn != nil {
		return f.authenticateUserFn(email, password)
	}
	return nil, nil
}

func (f *fakeAuthStore) CreateSession(userID int64) (string, error) {
	f.sessionUserID = userID
	if f.createSessionFn != nil {
		return f.createSessionFn(userID)
	}
	return "", nil
}

func (f *fakeAuthStore) UpdateUserPassword(userID int64, currentPassword, newPassword string) (bool, error) {
	f.passwordUserID = userID
	f.currentPassword = currentPassword
	f.newPassword = newPassword
	if f.updateUserPasswordFn != nil {
		return f.updateUserPasswordFn(userID, currentPassword, newPassword)
	}
	return false, nil
}

func (f *fakeAuthStore) CreateSignupRequest(email string) error {
	f.signupEmail = email
	if f.createSignupRequestFn != nil {
		return f.createSignupRequestFn(email)
	}
	return nil
}

func (f *fakeAuthStore) ListPendingSignupRequests() ([]store.SignupRequest, error) {
	if f.listPendingSignupRequestsFn != nil {
		return f.listPendingSignupRequestsFn()
	}
	return nil, nil
}

func (f *fakeAuthStore) ApproveSignupRequest(id int64) (store.SignupApproval, error) {
	f.signupRequestID = id
	if f.approveSignupRequestFn != nil {
		return f.approveSignupRequestFn(id)
	}
	return store.SignupApproval{}, nil
}

func (f *fakeAuthStore) RejectSignupRequest(id int64) (bool, error) {
	f.signupRequestID = id
	if f.rejectSignupRequestFn != nil {
		return f.rejectSignupRequestFn(id)
	}
	return false, nil
}

func (f *fakeAuthStore) SetupPassword(token, newPassword string) (store.SetupPasswordOutcome, error) {
	f.setupToken = token
	f.newPassword = newPassword
	if f.setupPasswordFn != nil {
		return f.setupPasswordFn(token, newPassword)
	}
	return store.SetupPasswordOutcome{}, nil
}

func TestAuthProcessorLoginCreatesSession(t *testing.T) {
	s := &fakeAuthStore{
		authenticateUserFn: func(email, password string) (*store.User, error) {
			return &store.User{ID: 7, Email: "alice@example.com"}, nil
		},
		createSessionFn: func(userID int64) (string, error) {
			return "token-123", nil
		},
	}

	p := NewAuthProcessor(s)
	outcome, err := p.Login(LoginRequest{
		Email:    "alice@example.com",
		Password: "secret",
	})
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	if outcome.Token != "token-123" {
		t.Fatalf("Token = %q, want token-123", outcome.Token)
	}
	if outcome.Email != "alice@example.com" {
		t.Fatalf("Email = %q, want alice@example.com", outcome.Email)
	}
	if s.authEmail != "alice@example.com" || s.authPassword != "secret" {
		t.Fatalf("AuthenticateUser called with %q/%q", s.authEmail, s.authPassword)
	}
	if s.sessionUserID != 7 {
		t.Fatalf("CreateSession userID = %d, want 7", s.sessionUserID)
	}
}

func TestAuthProcessorLoginRejectsInvalidCredentials(t *testing.T) {
	p := NewAuthProcessor(&fakeAuthStore{})

	_, err := p.Login(LoginRequest{
		Email:    "alice@example.com",
		Password: "wrong",
	})
	var unauthorized *unauthorizedError
	if !errors.As(err, &unauthorized) {
		t.Fatalf("Login() error = %v, want unauthorizedError", err)
	}
	if unauthorized.Error() != "invalid credentials" {
		t.Fatalf("unauthorized = %q", unauthorized.Error())
	}
}

func TestAuthProcessorChangePasswordRejectsMissingFields(t *testing.T) {
	p := NewAuthProcessor(&fakeAuthStore{})

	err := p.ChangePassword(&store.User{ID: 9}, ChangePasswordRequest{})
	var badRequest *badRequestError
	if !errors.As(err, &badRequest) {
		t.Fatalf("ChangePassword() error = %v, want badRequestError", err)
	}
	if badRequest.Error() != "current_password and new_password are required" {
		t.Fatalf("bad request = %q", badRequest.Error())
	}
}

func TestAuthProcessorChangePasswordRejectsWrongCurrentPassword(t *testing.T) {
	s := &fakeAuthStore{
		updateUserPasswordFn: func(userID int64, currentPassword, newPassword string) (bool, error) {
			return false, nil
		},
	}

	p := NewAuthProcessor(s)
	err := p.ChangePassword(&store.User{ID: 9}, ChangePasswordRequest{
		CurrentPassword: "old",
		NewPassword:     "new",
	})
	var unauthorized *unauthorizedError
	if !errors.As(err, &unauthorized) {
		t.Fatalf("ChangePassword() error = %v, want unauthorizedError", err)
	}
	if unauthorized.Error() != "current password is incorrect" {
		t.Fatalf("unauthorized = %q", unauthorized.Error())
	}
}

func TestAuthProcessorChangePasswordUpdatesPassword(t *testing.T) {
	s := &fakeAuthStore{
		updateUserPasswordFn: func(userID int64, currentPassword, newPassword string) (bool, error) {
			return true, nil
		},
	}

	p := NewAuthProcessor(s)
	err := p.ChangePassword(&store.User{ID: 9}, ChangePasswordRequest{
		CurrentPassword: "old",
		NewPassword:     "new",
	})
	if err != nil {
		t.Fatalf("ChangePassword() error = %v", err)
	}
	if s.passwordUserID != 9 {
		t.Fatalf("UpdateUserPassword userID = %d, want 9", s.passwordUserID)
	}
	if s.currentPassword != "old" || s.newPassword != "new" {
		t.Fatalf("UpdateUserPassword called with %q/%q", s.currentPassword, s.newPassword)
	}
}

func TestAuthProcessorRequestAccessCreatesSignupRequest(t *testing.T) {
	s := &fakeAuthStore{}
	p := NewAuthProcessor(s)

	err := p.RequestAccess(RequestAccessRequest{Email: "alice@example.com"})
	if err != nil {
		t.Fatalf("RequestAccess() error = %v", err)
	}
	if s.signupEmail != "alice@example.com" {
		t.Fatalf("CreateSignupRequest email = %q, want alice@example.com", s.signupEmail)
	}
}

func TestAuthProcessorApproveSignupRequestReturnsNotFound(t *testing.T) {
	p := NewAuthProcessor(&fakeAuthStore{})

	_, err := p.ApproveSignupRequest(42)
	var notFound *notFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("ApproveSignupRequest() error = %v, want notFoundError", err)
	}
	if notFound.Error() != "request not found or already processed" {
		t.Fatalf("not found = %q", notFound.Error())
	}
}

func TestAuthProcessorApproveSignupRequestReturnsSetupToken(t *testing.T) {
	s := &fakeAuthStore{
		approveSignupRequestFn: func(id int64) (store.SignupApproval, error) {
			return store.SignupApproval{
				Email:      "alice@example.com",
				SetupToken: "setup-token",
				ExpiresAt:  time.Date(2026, time.March, 16, 10, 0, 0, 0, time.UTC),
			}, nil
		},
	}

	p := NewAuthProcessor(s)
	outcome, err := p.ApproveSignupRequest(7)
	if err != nil {
		t.Fatalf("ApproveSignupRequest() error = %v", err)
	}
	if outcome.Email != "alice@example.com" || outcome.SetupToken != "setup-token" {
		t.Fatalf("unexpected approval outcome: %#v", outcome)
	}
}

func TestAuthProcessorRejectSignupRequestReturnsNotFound(t *testing.T) {
	p := NewAuthProcessor(&fakeAuthStore{})

	err := p.RejectSignupRequest(42)
	var notFound *notFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("RejectSignupRequest() error = %v, want notFoundError", err)
	}
}

func TestAuthProcessorSetupPasswordRejectsMissingFields(t *testing.T) {
	p := NewAuthProcessor(&fakeAuthStore{})

	_, err := p.SetupPassword(SetupPasswordRequest{})
	var badRequest *badRequestError
	if !errors.As(err, &badRequest) {
		t.Fatalf("SetupPassword() error = %v, want badRequestError", err)
	}
	if badRequest.Error() != "token and new_password are required" {
		t.Fatalf("bad request = %q", badRequest.Error())
	}
}

func TestAuthProcessorSetupPasswordRejectsInvalidToken(t *testing.T) {
	p := NewAuthProcessor(&fakeAuthStore{})

	_, err := p.SetupPassword(SetupPasswordRequest{Token: "bad", NewPassword: "secret"})
	var unauthorized *unauthorizedError
	if !errors.As(err, &unauthorized) {
		t.Fatalf("SetupPassword() error = %v, want unauthorizedError", err)
	}
	if unauthorized.Error() != "invalid or expired setup token" {
		t.Fatalf("unauthorized = %q", unauthorized.Error())
	}
}

func TestAuthProcessorSetupPasswordReturnsSession(t *testing.T) {
	s := &fakeAuthStore{
		setupPasswordFn: func(token, newPassword string) (store.SetupPasswordOutcome, error) {
			return store.SetupPasswordOutcome{
				Email:        "alice@example.com",
				SessionToken: "session-123",
			}, nil
		},
	}

	p := NewAuthProcessor(s)
	outcome, err := p.SetupPassword(SetupPasswordRequest{Token: "setup-token", NewPassword: "secret"})
	if err != nil {
		t.Fatalf("SetupPassword() error = %v", err)
	}
	if outcome.Email != "alice@example.com" || outcome.Token != "session-123" {
		t.Fatalf("unexpected setup password outcome: %#v", outcome)
	}
	if s.setupToken != "setup-token" || s.newPassword != "secret" {
		t.Fatalf("SetupPassword store called with %q/%q", s.setupToken, s.newPassword)
	}
}
