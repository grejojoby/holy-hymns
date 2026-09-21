// Package identity owns authentication, session revocation, authorization and mail delivery.
package identity

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Config struct {
	PublicURL, SMTPHost, SMTPUser, SMTPPassword, SMTPFrom, MailEncryptionKey string
	AppleTeamID, AppleKeyID, ApplePrivateKey                                 string
	SMTPPort, MailDailyLimit, MailMonthlyLimit                               int
	GoogleClientIDs, AppleClientIDs                                          []string
	TrustedProxyCIDRs                                                        []string
	AllowInsecureSMTP                                                        bool
}
type User struct {
	ID        string `json:"id"`
	Email     string `json:"email"`
	Name      string `json:"name"`
	Role      string `json:"role"`
	Verified  bool   `json:"verified"`
	Suspended bool   `json:"suspended"`
}
type Service struct {
	db                            *pgxpool.Pool
	cfg                           Config
	hashSlots                     chan struct{}
	verifiers                     map[string]*oidc.IDTokenVerifier
	appleClient                   *http.Client
	appleTokenURL, appleRevokeURL string
	// Injectable only within this package for deterministic provider boundary tests.
	socialVerifier func(context.Context, string, string, string) (socialClaims, error)
}
type contextKey struct{}
type sessionKey struct{}

const userColumns = "id::text,email,name,role,verified,suspended"

func New(db *pgxpool.Pool, cfg Config) *Service {
	if cfg.SMTPPort == 0 {
		cfg.SMTPPort = 587
	}
	if cfg.MailDailyLimit <= 0 || cfg.MailDailyLimit > 80 {
		cfg.MailDailyLimit = 80
	}
	if cfg.MailMonthlyLimit <= 0 || cfg.MailMonthlyLimit > 2000 {
		cfg.MailMonthlyLimit = 2000
	}
	s := &Service{db: db, cfg: cfg, hashSlots: make(chan struct{}, 1), verifiers: map[string]*oidc.IDTokenVerifier{}}
	s.appleClient = &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	s.appleTokenURL = "https://appleid.apple.com/auth/token"
	s.appleRevokeURL = "https://appleid.apple.com/auth/revoke"
	client := &http.Client{Timeout: 10 * time.Second}
	ctx := oidc.ClientContext(context.Background(), client)
	s.verifiers["google"] = oidc.NewVerifier("https://accounts.google.com", oidc.NewRemoteKeySet(ctx, "https://www.googleapis.com/oauth2/v3/certs"), &oidc.Config{SkipClientIDCheck: true, SupportedSigningAlgs: []string{"RS256"}})
	s.verifiers["apple"] = oidc.NewVerifier("https://appleid.apple.com", oidc.NewRemoteKeySet(ctx, "https://appleid.apple.com/auth/keys"), &oidc.Config{SkipClientIDCheck: true, SupportedSigningAlgs: []string{"RS256"}})
	s.socialVerifier = s.verifySocial
	return s
}

func Current(r *http.Request) *User { u, _ := r.Context().Value(contextKey{}).(*User); return u }
func roleAllows(actual, required string) bool {
	switch required {
	case "reader":
		return actual == "reader" || actual == "admin" || actual == "owner"
	case "admin":
		return actual == "admin" || actual == "owner"
	case "owner":
		return actual == "owner"
	}
	return false
}
func scanUser(row pgx.Row) (*User, error) {
	var u User
	err := row.Scan(&u.ID, &u.Email, &u.Name, &u.Role, &u.Verified, &u.Suspended)
	return &u, err
}
func (s *Service) authenticate(r *http.Request) (*User, []byte, error) {
	header := r.Header.Get("Authorization")
	if !strings.HasPrefix(header, "Bearer ") {
		return nil, nil, errors.New("missing bearer token")
	}
	token := strings.TrimPrefix(header, "Bearer ")
	if !validToken(token) {
		return nil, nil, errors.New("invalid session")
	}
	hash := tokenHash(token)
	u, err := scanUser(s.db.QueryRow(r.Context(), `SELECT u.id::text,u.email,u.name,u.role,u.verified,u.suspended FROM users u JOIN auth_sessions s ON s.user_id=u.id WHERE s.token_hash=$1 AND s.expires_at>now() AND u.verified AND NOT u.suspended`, hash))
	return u, hash, err
}
func (s *Service) Require(role string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, h, err := s.authenticate(r)
		if err != nil {
			writeError(w, 401, "unauthorized", "Please sign in again.")
			return
		}
		if !roleAllows(u.Role, role) {
			writeError(w, 403, "forbidden", "You do not have access to this action.")
			return
		}
		ctx := context.WithValue(context.WithValue(r.Context(), contextKey{}, u), sessionKey{}, h)
		next(w, r.WithContext(ctx))
	}
}
func (s *Service) Optional(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			next(w, r)
			return
		}
		s.Require("reader", next)(w, r)
	}
}
func (s *Service) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/auth/register", s.limited(s.register))
	mux.HandleFunc("POST /v1/auth/login", s.limited(s.login))
	mux.HandleFunc("GET /v1/auth/me", s.Require("reader", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, Current(r)) }))
	mux.HandleFunc("POST /v1/auth/verify", s.limited(s.verify))
	mux.HandleFunc("POST /v1/auth/forgot-password", s.limited(s.forgotPassword))
	mux.HandleFunc("POST /v1/auth/reset-password", s.limited(s.resetPassword))
	mux.HandleFunc("POST /v1/auth/logout", s.Require("reader", s.logout))
	mux.HandleFunc("POST /v1/auth/logout-all", s.Require("reader", s.logoutAll))
	mux.HandleFunc("DELETE /v1/auth/account", s.Require("reader", s.limited(s.deleteAccount)))
	mux.HandleFunc("GET /v1/auth/providers", s.providers)
	mux.HandleFunc("GET /v1/auth/challenge", s.limited(s.Optional(s.challenge)))
	mux.HandleFunc("POST /v1/auth/social/{provider}", s.limited(s.social))
	mux.HandleFunc("POST /v1/auth/link/{provider}", s.Require("reader", s.limited(s.link)))
	mux.HandleFunc("POST /v1/auth/accept-invitation", s.limited(s.acceptInvitation))
	mux.HandleFunc("GET /v1/admin/users", s.Require("admin", s.users))
	mux.HandleFunc("PUT /v1/admin/users/{id}", s.Require("admin", s.updateUser))
	mux.HandleFunc("POST /v1/admin/invitations", s.Require("owner", s.invite))
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 32*1024)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		writeError(w, 400, "invalid_request", "Invalid JSON request.")
		return false
	}
	if err := d.Decode(&struct{}{}); err != io.EOF {
		writeError(w, 400, "invalid_request", "Only one JSON object is allowed.")
		return false
	}
	return true
}
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if status != 204 {
		_ = json.NewEncoder(w).Encode(v)
	}
}
func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]string{"error": message, "code": code})
}
func ok(w http.ResponseWriter, message string) {
	writeJSON(w, 200, map[string]string{"message": message})
}
func internal(w http.ResponseWriter, err error) {
	slog.Error("identity operation failed", "error", err)
	writeError(w, 500, "internal_error", "This action could not be completed. Please try again.")
}
func conflict(err error) bool {
	var pg *pgconn.PgError
	return errors.As(err, &pg) && pg.Code == "23505"
}

func (s *Service) limited(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.rate(w, r, "ip:"+s.clientIP(r), 60, 15*time.Minute) {
			return
		}
		next(w, r)
	}
}

// Only the configured reverse proxy may supply the client address. Caddy must
// overwrite X-Real-IP, not append or pass through the incoming header.
func (s *Service) clientIP(r *http.Request) string {
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		ip = r.RemoteAddr
	}
	peer, err := netip.ParseAddr(ip)
	if err != nil {
		return ip
	}
	for _, raw := range s.cfg.TrustedProxyCIDRs {
		prefix, err := netip.ParsePrefix(raw)
		if err == nil && prefix.Contains(peer.Unmap()) {
			if forwarded, err := netip.ParseAddr(r.Header.Get("X-Real-IP")); err == nil {
				return forwarded.Unmap().String()
			}
		}
	}
	return peer.Unmap().String()
}

// ClientIP exposes the same trusted-proxy policy for other request limiters.
func (s *Service) ClientIP(r *http.Request) string { return s.clientIP(r) }
func (s *Service) rate(w http.ResponseWriter, r *http.Request, key string, max int, period time.Duration) bool {
	var n int
	err := s.db.QueryRow(r.Context(), `INSERT INTO auth_rate_limits(bucket,hits,reset_at) VALUES($1,1,now()+make_interval(secs => $2)) ON CONFLICT(bucket) DO UPDATE SET hits=CASE WHEN auth_rate_limits.reset_at<=now() THEN 1 ELSE auth_rate_limits.hits+1 END,reset_at=CASE WHEN auth_rate_limits.reset_at<=now() THEN excluded.reset_at ELSE auth_rate_limits.reset_at END RETURNING hits`, tokenHash(key), period.Seconds()).Scan(&n)
	if err != nil {
		internal(w, err)
		return false
	}
	if n > max {
		w.Header().Set("Retry-After", "900")
		writeError(w, 429, "rate_limited", "Too many attempts. Please try again later.")
		return false
	}
	return true
}
func audit(ctx context.Context, tx pgx.Tx, actor, target, action string) error {
	_, err := tx.Exec(ctx, `INSERT INTO security_audit(actor_id,target_id,action) VALUES(nullif($1,'')::uuid,nullif($2,'')::uuid,$3)`, actor, target, action)
	return err
}
func (s *Service) newSession(ctx context.Context, tx pgx.Tx, userID string) (string, error) {
	token, err := randomToken()
	if err != nil {
		return "", err
	}
	_, err = tx.Exec(ctx, `INSERT INTO auth_sessions(token_hash,user_id,expires_at) VALUES($1,$2,now()+interval '30 days')`, tokenHash(token), userID)
	return token, err
}
