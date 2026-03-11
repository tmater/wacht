package server

import (
	"fmt"

	"github.com/tmater/wacht/internal/store"
)

type authStore interface {
	AuthenticateUser(email, password string) (*store.User, error)
	CreateSession(userID int64) (string, error)
	UpdateUserPassword(userID int64, currentPassword, newPassword string) (bool, error)
	CreateSignupRequest(email string) error
	ListPendingSignupRequests() ([]store.SignupRequest, error)
	ApproveSignupRequest(id int64) (email, tempPassword string, err error)
	DeleteSignupRequest(id int64) error
}

type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type LoginOutcome struct {
	Token string
	Email string
}

type ChangePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

type RequestAccessRequest struct {
	Email string `json:"email"`
}

type SignupApprovalOutcome struct {
	Email        string
	TempPassword string
}

type authProcessor interface {
	Login(req LoginRequest) (LoginOutcome, error)
	ChangePassword(user *store.User, req ChangePasswordRequest) error
	RequestAccess(req RequestAccessRequest) error
	ListPendingSignupRequests() ([]store.SignupRequest, error)
	ApproveSignupRequest(id int64) (SignupApprovalOutcome, error)
	DeleteSignupRequest(id int64) error
}

type AuthProcessor struct {
	store authStore
}

func NewAuthProcessor(store authStore) *AuthProcessor {
	return &AuthProcessor{store: store}
}

func (p *AuthProcessor) Login(req LoginRequest) (LoginOutcome, error) {
	user, err := p.store.AuthenticateUser(req.Email, req.Password)
	if err != nil {
		return LoginOutcome{}, fmt.Errorf("authenticate user: %w", err)
	}
	if user == nil {
		return LoginOutcome{}, &unauthorizedError{message: "invalid credentials"}
	}

	token, err := p.store.CreateSession(user.ID)
	if err != nil {
		return LoginOutcome{}, fmt.Errorf("create session: %w", err)
	}

	return LoginOutcome{
		Token: token,
		Email: user.Email,
	}, nil
}

func (p *AuthProcessor) ChangePassword(user *store.User, req ChangePasswordRequest) error {
	if user == nil {
		return fmt.Errorf("user is required")
	}
	if req.CurrentPassword == "" || req.NewPassword == "" {
		return &badRequestError{message: "current_password and new_password are required"}
	}

	ok, err := p.store.UpdateUserPassword(user.ID, req.CurrentPassword, req.NewPassword)
	if err != nil {
		return fmt.Errorf("update password: %w", err)
	}
	if !ok {
		return &unauthorizedError{message: "current password is incorrect"}
	}
	return nil
}

func (p *AuthProcessor) RequestAccess(req RequestAccessRequest) error {
	if req.Email == "" {
		return &badRequestError{message: "email is required"}
	}
	if err := p.store.CreateSignupRequest(req.Email); err != nil {
		return fmt.Errorf("create signup request: %w", err)
	}
	return nil
}

func (p *AuthProcessor) ListPendingSignupRequests() ([]store.SignupRequest, error) {
	reqs, err := p.store.ListPendingSignupRequests()
	if err != nil {
		return nil, fmt.Errorf("list pending signup requests: %w", err)
	}
	return reqs, nil
}

func (p *AuthProcessor) ApproveSignupRequest(id int64) (SignupApprovalOutcome, error) {
	email, tempPassword, err := p.store.ApproveSignupRequest(id)
	if err != nil {
		return SignupApprovalOutcome{}, fmt.Errorf("approve signup request: %w", err)
	}
	if email == "" {
		return SignupApprovalOutcome{}, &notFoundError{message: "request not found or already processed"}
	}
	return SignupApprovalOutcome{
		Email:        email,
		TempPassword: tempPassword,
	}, nil
}

func (p *AuthProcessor) DeleteSignupRequest(id int64) error {
	if err := p.store.DeleteSignupRequest(id); err != nil {
		return fmt.Errorf("delete signup request: %w", err)
	}
	return nil
}
