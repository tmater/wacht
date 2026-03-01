package store

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// User represents a registered user.
type User struct {
	ID           int64
	Email        string
	PasswordHash string
	IsAdmin      bool
	CreatedAt    time.Time
}

// CreateUser hashes the password and inserts a new user. Returns the created user.
func (s *Store) CreateUser(email, password string, isAdmin bool) (*User, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	var id int64
	err = s.db.QueryRow(
		`INSERT INTO users (email, password_hash, is_admin, created_at) VALUES ($1, $2, $3, $4) RETURNING id`,
		email, string(hash), isAdmin, now,
	).Scan(&id)
	if err != nil {
		return nil, err
	}
	return &User{ID: id, Email: email, PasswordHash: string(hash), IsAdmin: isAdmin, CreatedAt: now}, nil
}

// CreateAdminUser creates a user with is_admin=true. Used for the seed user.
func (s *Store) CreateAdminUser(email, password string) (*User, error) {
	return s.CreateUser(email, password, true)
}

// AuthenticateUser verifies email+password and returns the user on success.
func (s *Store) AuthenticateUser(email, password string) (*User, error) {
	var u User
	err := s.db.QueryRow(`SELECT id, email, password_hash, is_admin, created_at FROM users WHERE email=$1`, email).
		Scan(&u.ID, &u.Email, &u.PasswordHash, &u.IsAdmin, &u.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)); err != nil {
		return nil, nil // wrong password
	}
	return &u, nil
}

// UserExists reports whether any user exists in the database.
func (s *Store) UserExists() (bool, error) {
	var count int
	err := s.db.QueryRow(`SELECT COUNT(1) FROM users`).Scan(&count)
	return count > 0, err
}

// CreateSession generates a random token, stores it, and returns it.
// Sessions expire after 30 days.
func (s *Store) CreateSession(userID int64) (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	token := hex.EncodeToString(b)
	now := time.Now().UTC()
	_, err := s.db.Exec(`INSERT INTO sessions (token, user_id, created_at, expires_at) VALUES ($1, $2, $3, $4)`,
		token, userID, now, now.Add(30*24*time.Hour))
	if err != nil {
		return "", err
	}
	return token, nil
}

// GetSessionUser returns the user for a valid, non-expired session token.
// Returns nil if the token is missing or expired.
func (s *Store) GetSessionUser(token string) (*User, error) {
	var u User
	err := s.db.QueryRow(`
		SELECT u.id, u.email, u.password_hash, u.is_admin, u.created_at
		FROM sessions s
		JOIN users u ON u.id = s.user_id
		WHERE s.token = $1 AND s.expires_at > $2
	`, token, time.Now().UTC()).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.IsAdmin, &u.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// DeleteSession removes a session token (logout).
func (s *Store) DeleteSession(token string) error {
	_, err := s.db.Exec(`DELETE FROM sessions WHERE token=$1`, token)
	return err
}

// UpdateUserPassword verifies the current password and replaces it with a new one.
// Returns false if the current password is wrong.
func (s *Store) UpdateUserPassword(userID int64, currentPassword, newPassword string) (bool, error) {
	var hash string
	err := s.db.QueryRow(`SELECT password_hash FROM users WHERE id=$1`, userID).Scan(&hash)
	if err != nil {
		return false, err
	}
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(currentPassword)); err != nil {
		return false, nil // wrong current password
	}
	newHash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return false, err
	}
	_, err = s.db.Exec(`UPDATE users SET password_hash=$1 WHERE id=$2`, string(newHash), userID)
	return err == nil, err
}

// SignupRequest represents a pending user signup request.
type SignupRequest struct {
	ID          int64
	Email       string
	RequestedAt time.Time
	Status      string
}

// CreateSignupRequest inserts a new signup request in pending state.
// Silently ignores duplicate emails to avoid enumeration.
func (s *Store) CreateSignupRequest(email string) error {
	_, err := s.db.Exec(`
		INSERT INTO signup_requests (email, requested_at, status)
		VALUES ($1, $2, 'pending')
		ON CONFLICT (email) DO NOTHING
	`, email, time.Now().UTC())
	return err
}

// ListPendingSignupRequests returns all requests with status='pending', oldest first.
func (s *Store) ListPendingSignupRequests() ([]SignupRequest, error) {
	rows, err := s.db.Query(`
		SELECT id, email, requested_at, status
		FROM signup_requests
		WHERE status = 'pending'
		ORDER BY requested_at ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var reqs []SignupRequest
	for rows.Next() {
		var r SignupRequest
		if err := rows.Scan(&r.ID, &r.Email, &r.RequestedAt, &r.Status); err != nil {
			return nil, err
		}
		reqs = append(reqs, r)
	}
	return reqs, rows.Err()
}

// ApproveSignupRequest creates a user for the given request and marks it approved.
// Returns the email and a generated temporary password.
// Returns ("", "", nil) if the request does not exist or is not pending.
func (s *Store) ApproveSignupRequest(id int64) (email, tempPassword string, err error) {
	var sr SignupRequest
	err = s.db.QueryRow(`
		SELECT id, email, status FROM signup_requests WHERE id = $1
	`, id).Scan(&sr.ID, &sr.Email, &sr.Status)
	if err == sql.ErrNoRows {
		return "", "", nil
	}
	if err != nil {
		return "", "", err
	}
	if sr.Status != "pending" {
		return "", "", nil
	}

	b := make([]byte, 16)
	if _, err = rand.Read(b); err != nil {
		return "", "", err
	}
	tempPassword = hex.EncodeToString(b)

	if _, err = s.CreateUser(sr.Email, tempPassword, false); err != nil {
		return "", "", err
	}

	_, err = s.db.Exec(`UPDATE signup_requests SET status='approved' WHERE id=$1`, id)
	if err != nil {
		return "", "", err
	}

	return sr.Email, tempPassword, nil
}

// DeleteSignupRequest removes a signup request by id. Idempotent.
func (s *Store) DeleteSignupRequest(id int64) error {
	_, err := s.db.Exec(`DELETE FROM signup_requests WHERE id=$1`, id)
	return err
}
