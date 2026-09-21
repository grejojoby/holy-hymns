package identity

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

func (s *Service) users(w http.ResponseWriter, r *http.Request) {
	limit := 100
	offset := 0
	if v, e := strconv.Atoi(r.URL.Query().Get("limit")); e == nil && v > 0 && v <= 100 {
		limit = v
	}
	if v, e := strconv.Atoi(r.URL.Query().Get("offset")); e == nil && v > 0 {
		offset = v
	}
	rows, err := s.db.Query(r.Context(), `SELECT `+userColumns+` FROM users ORDER BY created_at DESC,id LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		internal(w, err)
		return
	}
	defer rows.Close()
	items := []User{}
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			internal(w, err)
			return
		}
		items = append(items, *u)
	}
	if err = rows.Err(); err != nil {
		internal(w, err)
		return
	}
	var total int
	if err = s.db.QueryRow(r.Context(), `SELECT count(*) FROM users`).Scan(&total); err != nil {
		internal(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"items": items, "total": total})
}

func (s *Service) updateUser(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Suspended *bool   `json:"suspended"`
		Role      *string `json:"role"`
	}
	if !decode(w, r, &in) {
		return
	}
	if in.Role == nil && in.Suspended == nil {
		writeError(w, 400, "invalid_request", "Specify a role or suspension change.")
		return
	}
	if in.Role != nil && *in.Role != "admin" && *in.Role != "reader" && *in.Role != "owner" {
		writeError(w, 400, "invalid_role", "Role must be reader, admin, or owner.")
		return
	}
	actor := Current(r)
	if actor.Role != "owner" && in.Role != nil {
		writeError(w, 403, "forbidden", "Only the owner can change roles.")
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
	target, err := scanUser(tx.QueryRow(r.Context(), `SELECT `+userColumns+` FROM users WHERE id::text=$1 FOR UPDATE`, r.PathValue("id")))
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, 404, "not_found", "Account not found.")
		return
	}
	if err != nil {
		internal(w, err)
		return
	}
	if actor.Role != "owner" && target.Role != "reader" {
		writeError(w, 403, "forbidden", "Administrators can only suspend reader accounts.")
		return
	}
	newRole := target.Role
	if in.Role != nil {
		newRole = *in.Role
	}
	suspended := target.Suspended
	if in.Suspended != nil {
		suspended = *in.Suspended
	}
	if newRole == "owner" && target.Role != "owner" && (!target.Verified || suspended) {
		writeError(w, 409, "owner_must_be_active", "An owner must have a verified, active account.")
		return
	}
	if target.Role == "owner" && (newRole != "owner" || suspended) {
		var n int
		if err = tx.QueryRow(r.Context(), `SELECT count(*) FROM users WHERE role='owner' AND verified AND NOT suspended`).Scan(&n); err != nil {
			internal(w, err)
			return
		}
		if n <= 1 {
			writeError(w, 409, "last_owner", "The last active owner cannot be removed or suspended.")
			return
		}
	}
	_, err = tx.Exec(r.Context(), `UPDATE users SET role=$2,suspended=$3 WHERE id=$1`, target.ID, newRole, suspended)
	if err == nil {
		_, err = tx.Exec(r.Context(), `DELETE FROM auth_sessions WHERE user_id=$1`, target.ID)
	}
	if err == nil {
		err = audit(r.Context(), tx, actor.ID, target.ID, "account_access_changed")
	}
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		internal(w, err)
		return
	}
	target.Role = newRole
	target.Suspended = suspended
	writeJSON(w, 200, target)
}

func (s *Service) invite(w http.ResponseWriter, r *http.Request) {
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
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		internal(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	var id string
	var role string
	var suspended bool
	err = tx.QueryRow(r.Context(), `SELECT id::text,role,suspended FROM users WHERE email=$1 FOR UPDATE`, email).Scan(&id, &role, &suspended)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		internal(w, err)
		return
	}
	if suspended || role == "admin" || role == "owner" {
		writeError(w, 409, "account_unavailable", "This account is suspended or already has administrative access.")
		return
	}
	if err = s.queueAction(r.Context(), tx, id, email, "invite", 48*time.Hour); err != nil {
		_ = tx.Rollback(r.Context())
		s.mailError(w, err)
		return
	}
	if err = audit(r.Context(), tx, Current(r).ID, id, "admin_invited"); err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		internal(w, err)
		return
	}
	ok(w, "An administrator invitation has been queued.")
}

func (s *Service) acceptInvitation(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Token    string `json:"token"`
		Password string `json:"password"`
		Name     string `json:"name"`
	}
	if !decode(w, r, &in) {
		return
	}
	if !validToken(in.Token) {
		invalidAction(w)
		return
	}
	if len(in.Name) > 200 {
		writeError(w, 400, "invalid_name", "Name is too long.")
		return
	}
	var actor *User
	if r.Header.Get("Authorization") != "" {
		var err error
		actor, _, err = s.authenticate(r)
		if err != nil {
			writeError(w, 401, "unauthorized", "Sign in before accepting this invitation.")
			return
		}
	}
	var email string
	err := s.db.QueryRow(r.Context(), `SELECT email FROM auth_tokens WHERE token_hash=$1 AND purpose='invite' AND expires_at>now()`, tokenHash(in.Token)).Scan(&email)
	if errors.Is(err, pgx.ErrNoRows) {
		invalidAction(w)
		return
	}
	if err != nil {
		internal(w, err)
		return
	}
	var exists bool
	if err = s.db.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM users WHERE email=$1)`, email).Scan(&exists); err != nil {
		internal(w, err)
		return
	}
	var hash string
	if exists {
		if actor == nil || actor.Email != email {
			writeError(w, 409, "sign_in_required", "Sign in to the invited account, then accept this invitation.")
			return
		}
	} else {
		hash, err = s.hashPassword(r.Context(), in.Password)
		if err != nil {
			writeError(w, 400, "invalid_password", err.Error())
			return
		}
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
	err = tx.QueryRow(r.Context(), `DELETE FROM auth_tokens WHERE token_hash=$1 AND purpose='invite' AND expires_at>now() RETURNING email`, tokenHash(in.Token)).Scan(&email)
	if errors.Is(err, pgx.ErrNoRows) {
		invalidAction(w)
		return
	}
	if err != nil {
		internal(w, err)
		return
	}
	var id string
	if exists {
		if actor == nil || actor.Email != email {
			writeError(w, 403, "forbidden", "Invitation belongs to another account.")
			return
		}
		err = tx.QueryRow(r.Context(), `UPDATE users SET role='admin',verified=true WHERE id=$1 AND role='reader' AND NOT suspended RETURNING id::text`, actor.ID).Scan(&id)
	} else {
		err = tx.QueryRow(r.Context(), `INSERT INTO users(email,name,password_hash,role,verified) VALUES($1,$2,$3,'admin',true) RETURNING id::text`, email, strings.TrimSpace(in.Name), hash).Scan(&id)
	}
	if conflict(err) || errors.Is(err, pgx.ErrNoRows) {
		writeError(w, 409, "account_changed", "The account changed. Sign in and retry the invitation.")
		return
	}
	if err != nil {
		internal(w, err)
		return
	}
	if _, err = tx.Exec(r.Context(), `DELETE FROM auth_sessions WHERE user_id=$1`, id); err == nil {
		err = audit(r.Context(), tx, id, id, "admin_invitation_accepted")
	}
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		internal(w, err)
		return
	}
	ok(w, "Invitation accepted. Sign in to manage Holy Hymns.")
}
