package store

import (
	"testing"
)

func TestCreateUser_HashesPassword(t *testing.T) {
	s := newTestStore(t)

	user, err := s.CreateUser("alice@example.com", "secret")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if user.ID == 0 {
		t.Error("expected non-zero ID")
	}
	if user.Email != "alice@example.com" {
		t.Errorf("unexpected email %q", user.Email)
	}
	// Password must be stored hashed, not as plaintext.
	if user.PasswordHash == "secret" {
		t.Error("password stored as plaintext")
	}
	if user.PasswordHash == "" {
		t.Error("password hash is empty")
	}
}

func TestCreateUser_DuplicateEmail(t *testing.T) {
	s := newTestStore(t)

	if _, err := s.CreateUser("bob@example.com", "pass1"); err != nil {
		t.Fatalf("first CreateUser: %v", err)
	}
	_, err := s.CreateUser("bob@example.com", "pass2")
	if err == nil {
		t.Fatal("expected error on duplicate email, got nil")
	}
}

func TestAuthenticateUser_CorrectPassword(t *testing.T) {
	s := newTestStore(t)

	if _, err := s.CreateUser("carol@example.com", "correcthorsebattery"); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	user, err := s.AuthenticateUser("carol@example.com", "correcthorsebattery")
	if err != nil {
		t.Fatalf("AuthenticateUser: %v", err)
	}
	if user == nil {
		t.Fatal("expected user, got nil")
	}
	if user.Email != "carol@example.com" {
		t.Errorf("unexpected email %q", user.Email)
	}
}

func TestAuthenticateUser_WrongPassword(t *testing.T) {
	s := newTestStore(t)

	if _, err := s.CreateUser("dave@example.com", "rightpassword"); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	user, err := s.AuthenticateUser("dave@example.com", "wrongpassword")
	if err != nil {
		t.Fatalf("AuthenticateUser returned unexpected error: %v", err)
	}
	if user != nil {
		t.Fatal("expected nil for wrong password, got user")
	}
}

func TestAuthenticateUser_UnknownEmail(t *testing.T) {
	s := newTestStore(t)

	user, err := s.AuthenticateUser("nobody@example.com", "anything")
	if err != nil {
		t.Fatalf("AuthenticateUser returned unexpected error: %v", err)
	}
	if user != nil {
		t.Fatal("expected nil for unknown email, got user")
	}
}

func TestUserExists(t *testing.T) {
	s := newTestStore(t)

	exists, err := s.UserExists()
	if err != nil {
		t.Fatalf("UserExists (empty): %v", err)
	}
	if exists {
		t.Fatal("expected false on empty store")
	}

	if _, err := s.CreateUser("eve@example.com", "pass"); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	exists, err = s.UserExists()
	if err != nil {
		t.Fatalf("UserExists (after create): %v", err)
	}
	if !exists {
		t.Fatal("expected true after creating a user")
	}
}

func TestSession_CreateAndLookup(t *testing.T) {
	s := newTestStore(t)

	user, err := s.CreateUser("frank@example.com", "pass")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	token, err := s.CreateSession(user.ID)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if token == "" {
		t.Fatal("expected non-empty token")
	}

	got, err := s.GetSessionUser(token)
	if err != nil {
		t.Fatalf("GetSessionUser: %v", err)
	}
	if got == nil {
		t.Fatal("expected user, got nil")
	}
	if got.ID != user.ID {
		t.Errorf("expected user ID %d, got %d", user.ID, got.ID)
	}
}

func TestSession_InvalidToken(t *testing.T) {
	s := newTestStore(t)

	got, err := s.GetSessionUser("doesnotexist")
	if err != nil {
		t.Fatalf("GetSessionUser: %v", err)
	}
	if got != nil {
		t.Fatal("expected nil for unknown token, got user")
	}
}

func TestSession_Delete(t *testing.T) {
	s := newTestStore(t)

	user, err := s.CreateUser("grace@example.com", "pass")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	token, err := s.CreateSession(user.ID)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	if err := s.DeleteSession(token); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}

	got, err := s.GetSessionUser(token)
	if err != nil {
		t.Fatalf("GetSessionUser after delete: %v", err)
	}
	if got != nil {
		t.Fatal("expected nil after delete, got user")
	}
}
