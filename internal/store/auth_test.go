package store

import (
	"database/sql"
	"testing"
	"time"
)

func TestCreateUser_HashesPassword(t *testing.T) {
	s := newTestStore(t)

	user, err := s.CreateUser("alice@example.com", "secret", false)
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

	if _, err := s.CreateUser("bob@example.com", "pass1", false); err != nil {
		t.Fatalf("first CreateUser: %v", err)
	}
	_, err := s.CreateUser("BOB@example.com", "pass2", false)
	if err == nil {
		t.Fatal("expected error on duplicate email, got nil")
	}
}

func TestAuthenticateUser_CorrectPassword(t *testing.T) {
	s := newTestStore(t)

	if _, err := s.CreateUser("carol@example.com", "correcthorsebattery", false); err != nil {
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

	if _, err := s.CreateUser("dave@example.com", "rightpassword", false); err != nil {
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

	if _, err := s.CreateUser("eve@example.com", "pass", false); err != nil {
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

	user, err := s.CreateUser("frank@example.com", "pass", false)
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

	user, err := s.CreateUser("grace@example.com", "pass", false)
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

func TestCreateSignupRequest_ReopensWhenRulesAllow(t *testing.T) {
	s := newTestStore(t)

	if err := s.CreateSignupRequest("Retry@example.com"); err != nil {
		t.Fatalf("CreateSignupRequest create: %v", err)
	}

	var requestID int64
	if err := s.db.QueryRow(`SELECT id FROM signup_requests WHERE email = $1`, "retry@example.com").Scan(&requestID); err != nil {
		t.Fatalf("load signup request id: %v", err)
	}

	if err := s.CreateSignupRequest("RETRY@example.com"); err != nil {
		t.Fatalf("CreateSignupRequest pending duplicate: %v", err)
	}
	var pendingCount int
	if err := s.db.QueryRow(`SELECT COUNT(1) FROM signup_requests WHERE email = $1`, "retry@example.com").Scan(&pendingCount); err != nil {
		t.Fatalf("count pending requests: %v", err)
	}
	if pendingCount != 1 {
		t.Fatalf("expected one signup request row, got %d", pendingCount)
	}

	rejectedAt := time.Now().UTC().Add(-signupRetryWindow - time.Minute)
	if _, err := s.db.Exec(`
		UPDATE signup_requests
		SET status = $2, rejected_at = $3
		WHERE id = $1
	`, requestID, signupStatusRejected, rejectedAt); err != nil {
		t.Fatalf("mark rejected: %v", err)
	}
	if err := s.CreateSignupRequest("retry@example.com"); err != nil {
		t.Fatalf("CreateSignupRequest rejected reopen: %v", err)
	}
	var rejectedStatus string
	var rejectedReopenedAt time.Time
	if err := s.db.QueryRow(`SELECT status, requested_at FROM signup_requests WHERE id = $1`, requestID).Scan(&rejectedStatus, &rejectedReopenedAt); err != nil {
		t.Fatalf("load reopened rejected request: %v", err)
	}
	if rejectedStatus != signupStatusPending {
		t.Fatalf("status after rejected reopen = %q, want %q", rejectedStatus, signupStatusPending)
	}
	if !rejectedReopenedAt.After(rejectedAt) {
		t.Fatalf("expected requested_at to move forward after reopen, got %s <= %s", rejectedReopenedAt, rejectedAt)
	}

	user, err := s.CreateUser("retry@example.com", "initial-pass", false)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	expiredAt := time.Now().UTC().Add(-time.Minute)
	if _, err := s.db.Exec(`
		UPDATE signup_requests
		SET status = $2,
		    user_id = $3,
		    approved_at = $4,
		    setup_token_hash = $5,
		    setup_token_expires_at = $6,
		    setup_token_used_at = NULL
		WHERE id = $1
	`, requestID, signupStatusApproved, user.ID, time.Now().UTC().Add(-2*time.Hour), "expired-token-hash", expiredAt); err != nil {
		t.Fatalf("mark approved expired: %v", err)
	}
	if err := s.CreateSignupRequest("retry@example.com"); err != nil {
		t.Fatalf("CreateSignupRequest approved reopen: %v", err)
	}

	var approvedStatus string
	var reopenedUserID sql.NullInt64
	var clearedHash sql.NullString
	var clearedExpiry sql.NullTime
	if err := s.db.QueryRow(`
		SELECT status, user_id, setup_token_hash, setup_token_expires_at
		FROM signup_requests
		WHERE id = $1
	`, requestID).Scan(&approvedStatus, &reopenedUserID, &clearedHash, &clearedExpiry); err != nil {
		t.Fatalf("load reopened approved request: %v", err)
	}
	if approvedStatus != signupStatusPending {
		t.Fatalf("status after approved reopen = %q, want %q", approvedStatus, signupStatusPending)
	}
	if !reopenedUserID.Valid || reopenedUserID.Int64 != user.ID {
		t.Fatalf("expected reopened request to keep user_id %d, got %+v", user.ID, reopenedUserID)
	}
	if clearedHash.Valid {
		t.Fatalf("expected setup_token_hash cleared on reopen, got %q", clearedHash.String)
	}
	if clearedExpiry.Valid {
		t.Fatalf("expected setup_token_expires_at cleared on reopen, got %s", clearedExpiry.Time)
	}
}

func TestApproveSignupRequest_ReusesExistingUserAndRotatesSetupToken(t *testing.T) {
	s := newTestStore(t)

	if err := s.CreateSignupRequest("approve@example.com"); err != nil {
		t.Fatalf("CreateSignupRequest: %v", err)
	}
	var requestID int64
	if err := s.db.QueryRow(`SELECT id FROM signup_requests WHERE email = $1`, "approve@example.com").Scan(&requestID); err != nil {
		t.Fatalf("load request id: %v", err)
	}

	firstApproval, err := s.ApproveSignupRequest(requestID)
	if err != nil {
		t.Fatalf("ApproveSignupRequest first: %v", err)
	}
	if firstApproval.Email != "approve@example.com" {
		t.Fatalf("approval email = %q, want approve@example.com", firstApproval.Email)
	}
	if firstApproval.SetupToken == "" {
		t.Fatal("expected setup token on first approval")
	}

	var firstUserID int64
	var firstHash string
	if err := s.db.QueryRow(`
		SELECT user_id, setup_token_hash
		FROM signup_requests
		WHERE id = $1
	`, requestID).Scan(&firstUserID, &firstHash); err != nil {
		t.Fatalf("load first approval state: %v", err)
	}

	if _, err := s.db.Exec(`
		UPDATE signup_requests
		SET status = $2,
		    approved_at = NULL,
		    setup_token_hash = NULL,
		    setup_token_expires_at = NULL,
		    setup_token_used_at = NULL
		WHERE id = $1
	`, requestID, signupStatusPending); err != nil {
		t.Fatalf("reopen request manually: %v", err)
	}

	secondApproval, err := s.ApproveSignupRequest(requestID)
	if err != nil {
		t.Fatalf("ApproveSignupRequest second: %v", err)
	}
	if secondApproval.SetupToken == "" {
		t.Fatal("expected setup token on second approval")
	}
	if secondApproval.SetupToken == firstApproval.SetupToken {
		t.Fatal("expected rotated setup token on reapproval")
	}

	var secondUserID int64
	var secondHash string
	if err := s.db.QueryRow(`
		SELECT user_id, setup_token_hash
		FROM signup_requests
		WHERE id = $1
	`, requestID).Scan(&secondUserID, &secondHash); err != nil {
		t.Fatalf("load second approval state: %v", err)
	}
	if secondUserID != firstUserID {
		t.Fatalf("expected reused user_id %d, got %d", firstUserID, secondUserID)
	}
	if secondHash == firstHash {
		t.Fatal("expected setup token hash to rotate on reapproval")
	}

	var userCount int
	if err := s.db.QueryRow(`SELECT COUNT(1) FROM users WHERE email = $1`, "approve@example.com").Scan(&userCount); err != nil {
		t.Fatalf("count users: %v", err)
	}
	if userCount != 1 {
		t.Fatalf("expected exactly one user row, got %d", userCount)
	}
}

func TestSetupPassword_ConsumesTokenOnce(t *testing.T) {
	s := newTestStore(t)

	if err := s.CreateSignupRequest("setup@example.com"); err != nil {
		t.Fatalf("CreateSignupRequest: %v", err)
	}
	var requestID int64
	if err := s.db.QueryRow(`SELECT id FROM signup_requests WHERE email = $1`, "setup@example.com").Scan(&requestID); err != nil {
		t.Fatalf("load request id: %v", err)
	}

	approval, err := s.ApproveSignupRequest(requestID)
	if err != nil {
		t.Fatalf("ApproveSignupRequest: %v", err)
	}

	outcome, err := s.SetupPassword(approval.SetupToken, "chosen-password")
	if err != nil {
		t.Fatalf("SetupPassword first: %v", err)
	}
	if outcome.Email != "setup@example.com" {
		t.Fatalf("SetupPassword email = %q, want setup@example.com", outcome.Email)
	}
	if outcome.SessionToken == "" {
		t.Fatal("expected session token after setup password")
	}

	user, err := s.AuthenticateUser("setup@example.com", "chosen-password")
	if err != nil {
		t.Fatalf("AuthenticateUser: %v", err)
	}
	if user == nil {
		t.Fatal("expected created user to authenticate with chosen password")
	}

	reused, err := s.SetupPassword(approval.SetupToken, "second-password")
	if err != nil {
		t.Fatalf("SetupPassword second: %v", err)
	}
	if reused.SessionToken != "" || reused.Email != "" {
		t.Fatalf("expected zero-value outcome on token reuse, got %#v", reused)
	}

	var status string
	var usedAt time.Time
	var tokenHash sql.NullString
	if err := s.db.QueryRow(`
		SELECT status, setup_token_used_at, setup_token_hash
		FROM signup_requests
		WHERE id = $1
	`, requestID).Scan(&status, &usedAt, &tokenHash); err != nil {
		t.Fatalf("load completed signup request: %v", err)
	}
	if status != signupStatusCompleted {
		t.Fatalf("status after setup = %q, want %q", status, signupStatusCompleted)
	}
	if usedAt.IsZero() {
		t.Fatal("expected setup_token_used_at to be set")
	}
	if tokenHash.Valid {
		t.Fatalf("expected setup token hash cleared after use, got %q", tokenHash.String)
	}
}
