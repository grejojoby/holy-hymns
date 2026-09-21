package identity

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type credentials struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Name     string `json:"name"`
}

const registrationMessage = "If this address needs verification, a message has been queued. Otherwise, sign in or reset your password."

func (s *Service) register(w http.ResponseWriter, r *http.Request) {
	var in credentials
	if !decode(w, r, &in) {
		return
	}
	email, err := normalizeEmail(in.Email)
	if err != nil || len(in.Name) > 200 || !validPassword(in.Password) {
		writeError(w, 400, "invalid_registration", "Enter a valid email, name up to 200 bytes, and a password of at least 12 characters (maximum 1024 bytes).")
		return
	}
	if !s.mailReady(w) {
		return
	}
	if !s.rate(w, r, "account:"+email, 8, time.Hour) {
		return
	}
	hash, err := s.hashPassword(r.Context(), in.Password)
	if err != nil {
		internal(w, err)
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		internal(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	var id string
	err = tx.QueryRow(r.Context(), `INSERT INTO users(email,name,password_hash) VALUES($1,$2,$3) ON CONFLICT(email) DO NOTHING RETURNING id::text`, email, strings.TrimSpace(in.Name), hash).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		// Existing accounts retain their credentials. A repeat registration can only
		// resend verification, never change the account's password or name.
		var verified bool
		err = tx.QueryRow(r.Context(), `SELECT id::text,verified FROM users WHERE email=$1 FOR UPDATE`, email).Scan(&id, &verified)
		if err != nil {
			internal(w, err)
			return
		}
		if verified {
			ok(w, registrationMessage)
			return
		}
	} else if err != nil {
		internal(w, err)
		return
	}
	if err = s.queueAction(r.Context(), tx, id, email, "verify", 24*time.Hour); err != nil {
		_ = tx.Rollback(r.Context())
		s.mailError(w, err)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		internal(w, err)
		return
	}
	ok(w, registrationMessage)
}

func (s *Service) login(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if !decode(w, r, &in) {
		return
	}
	email, err := normalizeEmail(in.Email)
	if err != nil {
		writeError(w, 401, "invalid_credentials", "Email or password is incorrect.")
		return
	}
	if !s.rate(w, r, "login:"+email, 12, 15*time.Minute) {
		return
	}
	var password *string
	u := &User{}
	err = s.db.QueryRow(r.Context(), `SELECT `+userColumns+`,password_hash FROM users WHERE email=$1`, email).Scan(&u.ID, &u.Email, &u.Name, &u.Role, &u.Verified, &u.Suspended, &password)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		internal(w, err)
		return
	}
	// Equalize expensive work for unknown accounts to reduce enumeration timing.
	encoded := "$argon2id$v=19$m=65536,t=3,p=1$AAAAAAAAAAAAAAAAAAAAAA$AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	if password != nil {
		encoded = *password
	}
	valid := s.checkPassword(r.Context(), encoded, in.Password)
	if !valid || err != nil || password == nil {
		writeError(w, 401, "invalid_credentials", "Email or password is incorrect.")
		return
	}
	if !u.Verified || u.Suspended {
		writeError(w, 403, "account_unavailable", "Verify your email before signing in. Suspended accounts cannot sign in.")
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		internal(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	// Re-read under lock: password changes and suspension cannot race session creation.
	var same bool
	err = tx.QueryRow(r.Context(), `SELECT password_hash=$2 AND verified AND NOT suspended FROM users WHERE id=$1 FOR UPDATE`, u.ID, encoded).Scan(&same)
	if err != nil || !same {
		writeError(w, 401, "invalid_credentials", "Please sign in again.")
		return
	}
	token, err := s.newSession(r.Context(), tx, u.ID)
	if err == nil {
		err = audit(r.Context(), tx, u.ID, u.ID, "login")
	}
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		internal(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"token": token, "user": u})
}

func (s *Service) verify(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Token string `json:"token"`
	}
	if !decode(w, r, &in) {
		return
	}
	if !validToken(in.Token) {
		invalidAction(w)
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		internal(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	var id string
	err = tx.QueryRow(r.Context(), `DELETE FROM auth_tokens WHERE token_hash=$1 AND purpose='verify' AND expires_at>now() RETURNING user_id::text`, tokenHash(in.Token)).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		invalidAction(w)
		return
	}
	if err != nil {
		internal(w, err)
		return
	}
	_, err = tx.Exec(r.Context(), `UPDATE users SET verified=true WHERE id=$1`, id)
	if err == nil {
		err = audit(r.Context(), tx, id, id, "email_verified")
	}
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		internal(w, err)
		return
	}
	ok(w, "Email verified. You can now sign in.")
}
func invalidAction(w http.ResponseWriter) {
	writeError(w, 400, "invalid_token", "This link is invalid, expired, or already used.")
}

func (s *Service) forgotPassword(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email string `json:"email"`
	}
	if !decode(w, r, &in) {
		return
	}
	email, err := normalizeEmail(in.Email)
	if err != nil {
		writeError(w, 400, "invalid_email", "Enter a valid email address.")
		return
	}
	if !s.mailReady(w) {
		return
	}
	if !s.rate(w, r, "recovery:"+email, 5, time.Hour) {
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		internal(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	var id string
	err = tx.QueryRow(r.Context(), `SELECT id::text FROM users WHERE email=$1 AND NOT suspended FOR UPDATE`, email).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		ok(w, "If this account exists, a password reset email will be sent.")
		return
	}
	if err != nil {
		internal(w, err)
		return
	}
	if err = s.queueAction(r.Context(), tx, id, email, "reset", time.Hour); err != nil {
		_ = tx.Rollback(r.Context())
		s.mailError(w, err)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		internal(w, err)
		return
	}
	ok(w, "If this account exists, a password reset email will be sent.")
}

func (s *Service) resetPassword(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Token    string `json:"token"`
		Password string `json:"password"`
	}
	if !decode(w, r, &in) {
		return
	}
	if !validToken(in.Token) {
		invalidAction(w)
		return
	}
	if !validPassword(in.Password) {
		writeError(w, 400, "invalid_password", "Use at least 12 characters (maximum 1024 bytes).")
		return
	}
	// Validate before spending a bounded hash slot; consume atomically afterwards.
	var exists bool
	err := s.db.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM auth_tokens WHERE token_hash=$1 AND purpose='reset' AND expires_at>now())`, tokenHash(in.Token)).Scan(&exists)
	if err != nil {
		internal(w, err)
		return
	}
	if !exists {
		invalidAction(w)
		return
	}
	hash, err := s.hashPassword(r.Context(), in.Password)
	if err != nil {
		internal(w, err)
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		internal(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	var id string
	err = tx.QueryRow(r.Context(), `DELETE FROM auth_tokens WHERE token_hash=$1 AND purpose='reset' AND expires_at>now() RETURNING user_id::text`, tokenHash(in.Token)).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		invalidAction(w)
		return
	}
	if err != nil {
		internal(w, err)
		return
	}
	_, err = tx.Exec(r.Context(), `UPDATE users SET password_hash=$2,verified=true WHERE id=$1 AND NOT suspended`, id, hash)
	if err == nil {
		_, err = tx.Exec(r.Context(), `DELETE FROM auth_sessions WHERE user_id=$1`, id)
	}
	if err == nil {
		_, err = tx.Exec(r.Context(), `DELETE FROM auth_tokens WHERE user_id=$1 AND purpose IN ('reset','verify')`, id)
	}
	if err == nil {
		err = audit(r.Context(), tx, id, id, "password_reset")
	}
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		internal(w, err)
		return
	}
	ok(w, "Password reset. Sign in with your new password.")
}
func (s *Service) logout(w http.ResponseWriter, r *http.Request) {
	_, err := s.db.Exec(r.Context(), `DELETE FROM auth_sessions WHERE token_hash=$1`, r.Context().Value(sessionKey{}))
	if err != nil {
		internal(w, err)
		return
	}
	ok(w, "Signed out.")
}
func (s *Service) logoutAll(w http.ResponseWriter, r *http.Request) {
	_, err := s.db.Exec(r.Context(), `DELETE FROM auth_sessions WHERE user_id=$1`, Current(r).ID)
	if err != nil {
		internal(w, err)
		return
	}
	ok(w, "Signed out on all devices.")
}

func (s *Service) deleteAccount(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Password string `json:"password"`
	}
	if !decode(w, r, &in) {
		return
	}
	u := Current(r)
	var encoded *string
	var created time.Time
	err := s.db.QueryRow(r.Context(), `SELECT u.password_hash,s.created_at FROM users u JOIN auth_sessions s ON s.user_id=u.id WHERE u.id=$1 AND s.token_hash=$2`, u.ID, r.Context().Value(sessionKey{})).Scan(&encoded, &created)
	if err != nil {
		writeError(w, 401, "unauthorized", "Sign in again before deleting your account.")
		return
	}
	if encoded != nil {
		if !s.checkPassword(r.Context(), *encoded, in.Password) {
			writeError(w, 401, "invalid_credentials", "Enter your current password to delete your account.")
			return
		}
	} else if time.Since(created) > 10*time.Minute {
		writeError(w, 401, "reauthentication_required", "Sign in again before deleting your account.")
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		internal(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	if _, err = tx.Exec(r.Context(), `SELECT pg_advisory_xact_lock(802011)`); err != nil {
		internal(w, err)
		return
	}
	var owners int
	err = tx.QueryRow(r.Context(), `SELECT count(*) FROM users WHERE role='owner' AND verified AND NOT suspended`).Scan(&owners)
	if err != nil {
		internal(w, err)
		return
	}
	var currentRole string
	if err = tx.QueryRow(r.Context(), `SELECT role FROM users WHERE id=$1 FOR UPDATE`, u.ID).Scan(&currentRole); err != nil {
		internal(w, err)
		return
	}
	if currentRole == "owner" && owners <= 1 {
		writeError(w, 409, "last_owner", "Promote another verified account to owner before deleting this account.")
		return
	}
	manualAppleRevocation, err := s.revokeAppleGrant(r.Context(), tx, u.ID)
	if err != nil {
		writeError(w, 503, "apple_revocation_failed", "Apple access could not be revoked. Your account has been retained; retry deletion shortly or contact the owner.")
		return
	}
	if err = audit(r.Context(), tx, u.ID, u.ID, "account_deleted"); err != nil {
		internal(w, err)
		return
	}
	_, err = tx.Exec(r.Context(), `DELETE FROM mail_outbox WHERE recipient=$1`, u.Email)
	if err == nil {
		_, err = tx.Exec(r.Context(), `DELETE FROM auth_tokens WHERE email=$1`, u.Email)
	}
	// Favorites are owned by the catalogue subsystem and deliberately removed here.
	if err == nil {
		_, err = tx.Exec(r.Context(), `DELETE FROM favorites WHERE user_id=$1`, u.ID)
	}
	if err == nil {
		_, err = tx.Exec(r.Context(), `DELETE FROM users WHERE id=$1`, u.ID)
	}
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		internal(w, err)
		return
	}
	if manualAppleRevocation {
		writeJSON(w, 200, map[string]string{"message": "Your account and favorites have been deleted. Also remove Holy Hymns from Sign in with Apple in your Apple account settings.", "manualRevocationUrl": appleManualRevocationURL})
		return
	}
	ok(w, "Your account and favorites have been deleted.")
}

func (s *Service) BootstrapOwner(ctx context.Context, email, password, name string) error {
	return s.ownerCommand(ctx, email, password, name, false)
}
func (s *Service) RecoverOwner(ctx context.Context, email, password string) error {
	return s.ownerCommand(ctx, email, password, "", true)
}
func (s *Service) ownerCommand(ctx context.Context, email, password, name string, recover bool) error {
	email, err := normalizeEmail(email)
	if err != nil {
		return err
	}
	if len(name) > 200 {
		return errors.New("name too long")
	}
	hash, err := s.hashPassword(ctx, password)
	if err != nil {
		return err
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(802011)`); err != nil {
		return err
	}
	var n int
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM users WHERE role='owner'`).Scan(&n); err != nil {
		return err
	}
	if n > 0 && !recover {
		return errors.New("an owner already exists; use the explicit recovery command")
	}
	var id string
	err = tx.QueryRow(ctx, `INSERT INTO users(email,name,password_hash,role,verified) VALUES($1,$2,$3,'owner',true) ON CONFLICT(email) DO UPDATE SET password_hash=excluded.password_hash,role='owner',verified=true,suspended=false RETURNING id::text`, email, name, hash).Scan(&id)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `DELETE FROM auth_sessions WHERE user_id=$1`, id); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `DELETE FROM auth_tokens WHERE user_id=$1 OR email=$2`, id, email); err != nil {
		return err
	}
	action := "owner_bootstrapped"
	if recover {
		action = "owner_recovered"
	}
	if err = audit(ctx, tx, "", id, action); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
