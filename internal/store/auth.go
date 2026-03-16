package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const (
	signupStatusPending   = "pending"
	signupStatusApproved  = "approved"
	signupStatusRejected  = "rejected"
	signupStatusCompleted = "completed"

	signupRetryWindow = 24 * time.Hour
	setupTokenTTL     = 24 * time.Hour
)

type sqlQuerier interface {
	Exec(query string, args ...any) (sql.Result, error)
	QueryRow(query string, args ...any) *sql.Row
}

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
	hash, err := hashPassword(password)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	return createUserWithPasswordHash(s.db, normalizeEmail(email), hash, isAdmin, now)
}

// CreateAdminUser creates a user with is_admin=true. Used for the seed user.
func (s *Store) CreateAdminUser(email, password string) (*User, error) {
	return s.CreateUser(email, password, true)
}

// AuthenticateUser verifies email+password and returns the user on success.
func (s *Store) AuthenticateUser(email, password string) (*User, error) {
	var u User
	err := s.db.QueryRow(`SELECT id, email, password_hash, is_admin, created_at FROM users WHERE email=$1`, normalizeEmail(email)).
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
	return createSessionInQuerier(s.db, userID, time.Now().UTC())
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
	err = setUserPassword(s.db, userID, newPassword)
	return err == nil, err
}

// SignupRequest represents a user signup request and its onboarding state.
type SignupRequest struct {
	ID                  int64
	UserID              *int64
	Email               string
	RequestedAt         time.Time
	ApprovedAt          *time.Time
	RejectedAt          *time.Time
	CompletedAt         *time.Time
	SetupTokenExpiresAt *time.Time
	SetupTokenUsedAt    *time.Time
	Status              string
}

type SignupApproval struct {
	Email      string
	SetupToken string
	ExpiresAt  time.Time
}

type SetupPasswordOutcome struct {
	Email        string
	SessionToken string
}

// CreateSignupRequest inserts a new signup request in pending state or reopens
// the existing row when the retry rules allow it.
func (s *Store) CreateSignupRequest(email string) error {
	email = normalizeEmail(email)
	if email == "" {
		return nil
	}

	now := time.Now().UTC()
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	request, err := getSignupRequestByEmailForUpdate(tx, email)
	if err != nil {
		return err
	}
	if request == nil {
		userExists, err := userExistsByEmail(tx, email)
		if err != nil {
			return err
		}
		if userExists {
			return tx.Commit()
		}
		if _, err := tx.Exec(`
			INSERT INTO signup_requests (email, requested_at, status)
			VALUES ($1, $2, $3)
			ON CONFLICT (email) DO NOTHING
		`, email, now, signupStatusPending); err != nil {
			return err
		}
		return tx.Commit()
	}

	switch request.Status {
	case signupStatusPending, signupStatusCompleted:
		return tx.Commit()
	case signupStatusRejected:
		if request.RejectedAt == nil || request.RejectedAt.Add(signupRetryWindow).After(now) {
			return tx.Commit()
		}
	case signupStatusApproved:
		if request.SetupTokenUsedAt != nil || request.SetupTokenExpiresAt == nil || request.SetupTokenExpiresAt.After(now) {
			return tx.Commit()
		}
	default:
		return tx.Commit()
	}

	if _, err := tx.Exec(`
		UPDATE signup_requests
		SET status = $2,
		    requested_at = $3,
		    approved_at = NULL,
		    rejected_at = NULL,
		    completed_at = NULL,
		    setup_token_hash = NULL,
		    setup_token_expires_at = NULL,
		    setup_token_used_at = NULL
		WHERE id = $1
	`, request.ID, signupStatusPending, now); err != nil {
		return err
	}

	return tx.Commit()
}

// ListPendingSignupRequests returns all requests with status='pending', oldest first.
func (s *Store) ListPendingSignupRequests() ([]SignupRequest, error) {
	rows, err := s.db.Query(`
		SELECT id, user_id, email, requested_at, approved_at, rejected_at, completed_at,
		       setup_token_expires_at, setup_token_used_at, status
		FROM signup_requests
		WHERE status = $1
		ORDER BY requested_at ASC
	`, signupStatusPending)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var reqs []SignupRequest
	for rows.Next() {
		r, err := scanSignupRequest(rows)
		if err != nil {
			return nil, err
		}
		reqs = append(reqs, *r)
	}
	return reqs, rows.Err()
}

// ApproveSignupRequest creates or reuses the user for the given request and
// stores a fresh one-time setup token. Returns a zero-value outcome when the
// request does not exist or is not pending.
func (s *Store) ApproveSignupRequest(id int64) (SignupApproval, error) {
	now := time.Now().UTC()
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return SignupApproval{}, err
	}
	defer tx.Rollback()

	request, err := getSignupRequestByIDForUpdate(tx, id)
	if err != nil {
		return SignupApproval{}, err
	}
	if request == nil || request.Status != signupStatusPending {
		return SignupApproval{}, nil
	}

	userID, err := lookupUserIDByEmail(tx, request.Email)
	if err != nil {
		return SignupApproval{}, err
	}
	if userID == nil {
		user, err := createUserWithPlaceholderPassword(tx, request.Email, false, now)
		if err != nil {
			return SignupApproval{}, err
		}
		userID = &user.ID
	}

	setupToken, err := randomHexToken(32)
	if err != nil {
		return SignupApproval{}, err
	}
	expiresAt := now.Add(setupTokenTTL)
	if _, err := tx.Exec(`
		UPDATE signup_requests
		SET status = $2,
		    user_id = $3,
		    approved_at = $4,
		    rejected_at = NULL,
		    completed_at = NULL,
		    setup_token_hash = $5,
		    setup_token_expires_at = $6,
		    setup_token_used_at = NULL
		WHERE id = $1
	`, request.ID, signupStatusApproved, *userID, now, hashSetupToken(setupToken), expiresAt); err != nil {
		return SignupApproval{}, err
	}

	if err := tx.Commit(); err != nil {
		return SignupApproval{}, err
	}
	return SignupApproval{Email: request.Email, SetupToken: setupToken, ExpiresAt: expiresAt}, nil
}

// RejectSignupRequest marks a pending signup request rejected. Returns false
// when the request does not exist or is not pending.
func (s *Store) RejectSignupRequest(id int64) (bool, error) {
	res, err := s.db.Exec(`
		UPDATE signup_requests
		SET status = $2,
		    rejected_at = $3,
		    approved_at = NULL,
		    completed_at = NULL,
		    setup_token_hash = NULL,
		    setup_token_expires_at = NULL,
		    setup_token_used_at = NULL
		WHERE id = $1 AND status = $4
	`, id, signupStatusRejected, time.Now().UTC(), signupStatusPending)
	if err != nil {
		return false, err
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return rows > 0, nil
}

// SetupPassword consumes a one-time setup token, sets the user's password, and
// creates a normal session. Returns a zero-value outcome when the token is
// invalid, expired, or already used.
func (s *Store) SetupPassword(token, newPassword string) (SetupPasswordOutcome, error) {
	token = strings.TrimSpace(token)
	if token == "" || newPassword == "" {
		return SetupPasswordOutcome{}, nil
	}

	now := time.Now().UTC()
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return SetupPasswordOutcome{}, err
	}
	defer tx.Rollback()

	request, err := getSignupRequestBySetupTokenHashForUpdate(tx, hashSetupToken(token))
	if err != nil {
		return SetupPasswordOutcome{}, err
	}
	if request == nil || request.Status != signupStatusApproved || request.UserID == nil {
		return SetupPasswordOutcome{}, nil
	}
	if request.SetupTokenUsedAt != nil || request.SetupTokenExpiresAt == nil || !request.SetupTokenExpiresAt.After(now) {
		return SetupPasswordOutcome{}, nil
	}

	if err := setUserPassword(tx, *request.UserID, newPassword); err != nil {
		return SetupPasswordOutcome{}, err
	}
	sessionToken, err := createSessionInQuerier(tx, *request.UserID, now)
	if err != nil {
		return SetupPasswordOutcome{}, err
	}
	if _, err := tx.Exec(`
		UPDATE signup_requests
		SET status = $2,
		    completed_at = $3,
		    setup_token_hash = NULL,
		    setup_token_expires_at = NULL,
		    setup_token_used_at = $3
		WHERE id = $1
	`, request.ID, signupStatusCompleted, now); err != nil {
		return SetupPasswordOutcome{}, err
	}

	if err := tx.Commit(); err != nil {
		return SetupPasswordOutcome{}, err
	}
	return SetupPasswordOutcome{Email: request.Email, SessionToken: sessionToken}, nil
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func hashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

func createUserWithPasswordHash(q sqlQuerier, email, passwordHash string, isAdmin bool, now time.Time) (*User, error) {
	var id int64
	err := q.QueryRow(
		`INSERT INTO users (email, password_hash, is_admin, created_at) VALUES ($1, $2, $3, $4) RETURNING id`,
		email, passwordHash, isAdmin, now,
	).Scan(&id)
	if err != nil {
		return nil, err
	}
	return &User{ID: id, Email: email, PasswordHash: passwordHash, IsAdmin: isAdmin, CreatedAt: now}, nil
}

func createUserWithPlaceholderPassword(q sqlQuerier, email string, isAdmin bool, now time.Time) (*User, error) {
	placeholder, err := randomHexToken(32)
	if err != nil {
		return nil, err
	}
	hash, err := hashPassword(placeholder)
	if err != nil {
		return nil, err
	}
	return createUserWithPasswordHash(q, email, hash, isAdmin, now)
}

func userExistsByEmail(q sqlQuerier, email string) (bool, error) {
	var exists bool
	if err := q.QueryRow(`SELECT EXISTS (SELECT 1 FROM users WHERE email = $1)`, email).Scan(&exists); err != nil {
		return false, err
	}
	return exists, nil
}

func lookupUserIDByEmail(q sqlQuerier, email string) (*int64, error) {
	var id int64
	err := q.QueryRow(`SELECT id FROM users WHERE email = $1`, email).Scan(&id)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &id, nil
}

func createSessionInQuerier(q sqlQuerier, userID int64, now time.Time) (string, error) {
	token, err := randomHexToken(32)
	if err != nil {
		return "", err
	}
	_, err = q.Exec(`INSERT INTO sessions (token, user_id, created_at, expires_at) VALUES ($1, $2, $3, $4)`,
		token, userID, now, now.Add(30*24*time.Hour))
	if err != nil {
		return "", err
	}
	return token, nil
}

func setUserPassword(q sqlQuerier, userID int64, newPassword string) error {
	hash, err := hashPassword(newPassword)
	if err != nil {
		return err
	}
	_, err = q.Exec(`UPDATE users SET password_hash=$1 WHERE id=$2`, hash, userID)
	return err
}

func randomHexToken(size int) (string, error) {
	b := make([]byte, size)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func hashSetupToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func getSignupRequestByEmailForUpdate(tx *sql.Tx, email string) (*SignupRequest, error) {
	row := tx.QueryRow(`
		SELECT id, user_id, email, requested_at, approved_at, rejected_at, completed_at,
		       setup_token_expires_at, setup_token_used_at, status
		FROM signup_requests
		WHERE email = $1
		FOR UPDATE
	`, email)
	request, err := scanSignupRequest(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return request, nil
}

func getSignupRequestByIDForUpdate(tx *sql.Tx, id int64) (*SignupRequest, error) {
	row := tx.QueryRow(`
		SELECT id, user_id, email, requested_at, approved_at, rejected_at, completed_at,
		       setup_token_expires_at, setup_token_used_at, status
		FROM signup_requests
		WHERE id = $1
		FOR UPDATE
	`, id)
	request, err := scanSignupRequest(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return request, nil
}

func getSignupRequestBySetupTokenHashForUpdate(tx *sql.Tx, tokenHash string) (*SignupRequest, error) {
	row := tx.QueryRow(`
		SELECT id, user_id, email, requested_at, approved_at, rejected_at, completed_at,
		       setup_token_expires_at, setup_token_used_at, status
		FROM signup_requests
		WHERE setup_token_hash = $1
		FOR UPDATE
	`, tokenHash)
	request, err := scanSignupRequest(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return request, nil
}

type signupRequestScanner interface {
	Scan(dest ...any) error
}

func scanSignupRequest(scanner signupRequestScanner) (*SignupRequest, error) {
	var request SignupRequest
	var userID sql.NullInt64
	var approvedAt sql.NullTime
	var rejectedAt sql.NullTime
	var completedAt sql.NullTime
	var setupTokenExpiresAt sql.NullTime
	var setupTokenUsedAt sql.NullTime
	if err := scanner.Scan(
		&request.ID,
		&userID,
		&request.Email,
		&request.RequestedAt,
		&approvedAt,
		&rejectedAt,
		&completedAt,
		&setupTokenExpiresAt,
		&setupTokenUsedAt,
		&request.Status,
	); err != nil {
		return nil, err
	}
	request.UserID = nullInt64Pointer(userID)
	request.ApprovedAt = nullTimePointer(approvedAt)
	request.RejectedAt = nullTimePointer(rejectedAt)
	request.CompletedAt = nullTimePointer(completedAt)
	request.SetupTokenExpiresAt = nullTimePointer(setupTokenExpiresAt)
	request.SetupTokenUsedAt = nullTimePointer(setupTokenUsedAt)
	return &request, nil
}

func nullInt64Pointer(v sql.NullInt64) *int64 {
	if !v.Valid {
		return nil
	}
	value := v.Int64
	return &value
}

func nullTimePointer(v sql.NullTime) *time.Time {
	if !v.Valid {
		return nil
	}
	value := v.Time
	return &value
}
