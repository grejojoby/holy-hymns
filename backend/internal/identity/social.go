package identity

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type socialClaims struct {
	Subject, Email, Name, Audience string
	EmailVerified                  bool
}
type socialInput struct {
	IDToken           string `json:"idToken"`
	Nonce             string `json:"nonce"`
	AuthorizationCode string `json:"authorizationCode,omitempty"`
}

func (s *Service) providerIDs(provider string) []string {
	switch provider {
	case "google":
		return s.cfg.GoogleClientIDs
	case "apple":
		return s.cfg.AppleClientIDs
	}
	return nil
}
func (s *Service) providers(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]bool{"google": len(s.cfg.GoogleClientIDs) > 0, "apple": s.appleConfigured()})
}
func (s *Service) challenge(w http.ResponseWriter, r *http.Request) {
	nonce, err := randomToken()
	if err != nil {
		internal(w, err)
		return
	}
	userID := ""
	if u := Current(r); u != nil {
		userID = u.ID
	}
	_, err = s.db.Exec(r.Context(), `INSERT INTO auth_challenges(nonce_hash,user_id,expires_at) VALUES($1,nullif($2,'')::uuid,now()+interval '5 minutes')`, tokenHash(nonce), userID)
	if err != nil {
		internal(w, err)
		return
	}
	writeJSON(w, 200, map[string]string{"nonce": nonce})
}
func (s *Service) verifySocial(ctx context.Context, provider, raw, nonce string) (socialClaims, error) {
	var result socialClaims
	ids := s.providerIDs(provider)
	v := s.verifiers[provider]
	if len(ids) == 0 || v == nil || len(raw) > 20000 || !validToken(nonce) {
		return result, errors.New("provider unavailable or invalid token")
	}
	token, err := v.Verify(ctx, raw)
	if err != nil {
		return result, err
	}
	if token.Subject == "" || len(token.Subject) > 255 || subtle.ConstantTimeCompare([]byte(token.Nonce), []byte(nonce)) != 1 || token.IssuedAt.After(time.Now().Add(time.Minute)) || token.IssuedAt.Before(time.Now().Add(-10*time.Minute)) {
		return result, errors.New("invalid subject, issuance time, or nonce")
	}
	accepted := false
	audience := ""
	for _, aud := range token.Audience {
		if slices.Contains(ids, aud) {
			accepted = true
			audience = aud
		}
	}
	if !accepted {
		return result, errors.New("invalid audience")
	}
	var claims struct {
		Email           string          `json:"email"`
		Name            string          `json:"name"`
		EmailVerified   json.RawMessage `json:"email_verified"`
		AuthorizedParty string          `json:"azp"`
	}
	if err = token.Claims(&claims); err != nil {
		return result, err
	}
	if (len(token.Audience) > 1 && claims.AuthorizedParty == "") || (claims.AuthorizedParty != "" && !slices.Contains(ids, claims.AuthorizedParty)) {
		return result, errors.New("invalid authorized party")
	}
	// Apple's OIDC endpoint may encode this standard boolean as a JSON string.
	verified := string(claims.EmailVerified) == "true" || string(claims.EmailVerified) == `"true"`
	email, _ := normalizeEmail(claims.Email)
	name := strings.TrimSpace(claims.Name)
	if len(name) > 200 {
		name = ""
	}
	return socialClaims{Subject: token.Subject, Email: email, Name: name, EmailVerified: verified, Audience: audience}, nil
}
func (s *Service) readSocial(w http.ResponseWriter, r *http.Request) (socialInput, socialClaims, bool) {
	var in socialInput
	if !decode(w, r, &in) {
		return in, socialClaims{}, false
	}
	provider := r.PathValue("provider")
	if len(s.providerIDs(provider)) == 0 || (provider == "apple" && !s.appleConfigured()) {
		writeError(w, 503, "provider_unavailable", "This sign-in provider is not configured.")
		return in, socialClaims{}, false
	}
	if provider == "apple" && (in.AuthorizationCode == "" || len(in.AuthorizationCode) > 8192) {
		writeError(w, 400, "authorization_code_required", "Start a new Apple sign-in to obtain an authorization code.")
		return in, socialClaims{}, false
	}
	claims, err := s.socialVerifier(r.Context(), provider, in.IDToken, in.Nonce)
	if err != nil {
		writeError(w, 401, "invalid_social_token", "Social sign-in could not be verified. Start a new sign-in attempt.")
		return in, claims, false
	}
	return in, claims, true
}
func consumeChallenge(ctx context.Context, tx pgx.Tx, nonce, userID string) error {
	var bound *string
	err := tx.QueryRow(ctx, `DELETE FROM auth_challenges WHERE nonce_hash=$1 AND expires_at>now() RETURNING user_id::text`, tokenHash(nonce)).Scan(&bound)
	if err != nil {
		return err
	}
	if userID == "" && bound != nil {
		return errors.New("challenge bound to signed-in account")
	}
	if userID != "" && (bound == nil || *bound != userID) {
		return errors.New("challenge not bound to this account")
	}
	return nil
}
func (s *Service) social(w http.ResponseWriter, r *http.Request) {
	in, claims, valid := s.readSocial(w, r)
	if !valid {
		return
	}
	provider := r.PathValue("provider")
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		internal(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	if err = consumeChallenge(r.Context(), tx, in.Nonce, ""); err != nil {
		writeError(w, 401, "invalid_nonce", "Start a new social sign-in attempt.")
		return
	}
	u, err := scanUser(tx.QueryRow(r.Context(), `SELECT u.id::text,u.email,u.name,u.role,u.verified,u.suspended FROM users u JOIN provider_identities p ON p.user_id=u.id WHERE p.provider=$1 AND p.subject=$2 FOR UPDATE OF u`, provider, claims.Subject))
	if errors.Is(err, pgx.ErrNoRows) {
		if claims.Email == "" || !claims.EmailVerified {
			writeError(w, 400, "verified_email_required", "This provider must supply a verified email to create an account.")
			return
		}
		// Email matches never confer ownership. Existing account must be signed in
		// separately and explicitly linked from its authenticated account screen.
		u, err = scanUser(tx.QueryRow(r.Context(), `INSERT INTO users(email,name,verified) VALUES($1,$2,true) ON CONFLICT(email) DO NOTHING RETURNING `+userColumns, claims.Email, claims.Name))
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, 409, "account_exists", "An account already uses this email. Sign in to it, then link this provider from your account screen.")
			return
		}
		if err != nil {
			internal(w, err)
			return
		}
		_, err = tx.Exec(r.Context(), `INSERT INTO provider_identities(provider,subject,user_id) VALUES($1,$2,$3)`, provider, claims.Subject, u.ID)
	}
	if conflict(err) {
		writeError(w, 409, "identity_exists", "This identity is already linked. Try signing in again.")
		return
	}
	if err != nil {
		internal(w, err)
		return
	}
	if u.Suspended || !u.Verified {
		writeError(w, 403, "account_unavailable", "This account is not available for sign-in.")
		return
	}
	if provider == "apple" {
		if err = s.storeAppleGrant(r.Context(), tx, u.ID, in, claims); err != nil {
			writeError(w, 503, "apple_exchange_failed", "Apple sign-in could not be completed. Start a new sign-in attempt.")
			return
		}
	}
	token, err := s.newSession(r.Context(), tx, u.ID)
	if err == nil {
		err = audit(r.Context(), tx, u.ID, u.ID, "social_login")
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
func (s *Service) link(w http.ResponseWriter, r *http.Request) {
	in, claims, valid := s.readSocial(w, r)
	if !valid {
		return
	}
	u := Current(r)
	var fresh bool
	err := s.db.QueryRow(r.Context(), `SELECT created_at>now()-interval '10 minutes' FROM auth_sessions WHERE token_hash=$1`, r.Context().Value(sessionKey{})).Scan(&fresh)
	if err != nil || !fresh {
		writeError(w, 401, "reauthentication_required", "Sign in again before linking another login method.")
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		internal(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	if err = consumeChallenge(r.Context(), tx, in.Nonce, u.ID); err != nil {
		writeError(w, 401, "invalid_nonce", "Start a new provider linking attempt.")
		return
	}
	var active bool
	err = tx.QueryRow(r.Context(), `SELECT NOT suspended AND verified FROM users WHERE id=$1 FOR UPDATE`, u.ID).Scan(&active)
	if err != nil || !active {
		writeError(w, 403, "account_unavailable", "This account is unavailable.")
		return
	}
	_, err = tx.Exec(r.Context(), `INSERT INTO provider_identities(provider,subject,user_id) VALUES($1,$2,$3)`, r.PathValue("provider"), claims.Subject, u.ID)
	if conflict(err) {
		writeError(w, 409, "identity_exists", "This provider is already linked to an account.")
		return
	}
	if err != nil {
		internal(w, err)
		return
	}
	if r.PathValue("provider") == "apple" {
		if err = s.storeAppleGrant(r.Context(), tx, u.ID, in, claims); err != nil {
			writeError(w, 503, "apple_exchange_failed", "Apple linking could not be completed. Start a new sign-in attempt.")
			return
		}
	}
	if err = audit(r.Context(), tx, u.ID, u.ID, "provider_linked"); err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		internal(w, err)
		return
	}
	ok(w, "Login method linked.")
}
