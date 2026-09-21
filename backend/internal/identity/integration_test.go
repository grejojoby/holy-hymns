package identity

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func testIdentity(t *testing.T, cfg Config) *Service {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is required for PostgreSQL integration tests")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	schema := fmt.Sprintf("identity_test_%d", time.Now().UnixNano())
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	pcfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	pcfg.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
	db, err := pgxpool.NewWithConfig(ctx, pcfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Close()
		_, err := admin.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE")
		admin.Close()
		if err != nil {
			t.Error(err)
		}
	})
	sql, err := os.ReadFile("../migrations/002_identity.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(ctx, `CREATE TABLE favorites(user_id uuid,song_id uuid)`); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(ctx, string(sql)); err != nil {
		t.Fatal(err)
	}
	appleSQL, err := os.ReadFile("../migrations/004_apple_grants.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(ctx, string(appleSQL)); err != nil {
		t.Fatal(err)
	}
	return New(db, cfg)
}
func testMailConfig() Config {
	return Config{PublicURL: "https://hymns.example", SMTPHost: "localhost", SMTPPort: 1025, SMTPFrom: "app@hymns.example", AllowInsecureSMTP: true, MailEncryptionKey: base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{42}, 32))}
}
func request(t *testing.T, s *Service, method, path, token string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var b bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&b).Encode(body); err != nil {
			t.Fatal(err)
		}
	}
	r := httptest.NewRequest(method, path, &b)
	r.RemoteAddr = "192.0.2.1:1234"
	r.Header.Set("Content-Type", "application/json")
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	mux := http.NewServeMux()
	s.Register(mux)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}
func requireStatus(t *testing.T, w *httptest.ResponseRecorder, status int) {
	t.Helper()
	if w.Code != status {
		t.Fatalf("expected %d, got %d: %s", status, w.Code, w.Body.String())
	}
}
func sessionToken(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Token == "" {
		t.Fatal("missing session token")
	}
	return body.Token
}
func queuedToken(t *testing.T, s *Service, email string) string {
	t.Helper()
	var data []byte
	err := s.db.QueryRow(context.Background(), `SELECT encrypted_body FROM mail_outbox WHERE recipient=$1 ORDER BY created_at DESC LIMIT 1`, email).Scan(&data)
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.decryptMail(email, data)
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`token=([A-Za-z0-9_-]+)`).FindStringSubmatch(p.Body)
	if len(m) != 2 {
		t.Fatal("missing token in queued email")
	}
	if bytes.Contains(data, []byte(m[1])) {
		t.Fatal("action token stored in plaintext")
	}
	return m[1]
}
func seedAccount(t *testing.T, s *Service, email, role string) (string, string) {
	t.Helper()
	ctx := context.Background()
	hash, err := s.hashPassword(ctx, "a valid long password")
	if err != nil {
		t.Fatal(err)
	}
	var id string
	err = s.db.QueryRow(ctx, `INSERT INTO users(email,name,password_hash,verified,role) VALUES($1,'Reader',$2,true,$3) RETURNING id::text`, email, hash, role).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	token, err := s.newSession(ctx, tx, id)
	if err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	return id, token
}

func TestEmailRegistrationVerificationResetAndSessionRevocation(t *testing.T) {
	s := testIdentity(t, testMailConfig())
	email := "reader@example.com"
	registered := request(t, s, "POST", "/v1/auth/register", "", credentials{Email: email, Password: "a valid long password", Name: "Reader"})
	requireStatus(t, registered, 200)
	verifyToken := queuedToken(t, s, email)
	requireStatus(t, request(t, s, "POST", "/v1/auth/login", "", map[string]string{"email": email, "password": "a valid long password"}), 403)
	requireStatus(t, request(t, s, "POST", "/v1/auth/verify", "", map[string]string{"token": verifyToken}), 200)
	requireStatus(t, request(t, s, "POST", "/v1/auth/verify", "", map[string]string{"token": verifyToken}), 400)
	repeated := request(t, s, "POST", "/v1/auth/register", "", credentials{Email: email, Password: "replacement password rejected", Name: "Changed"})
	requireStatus(t, repeated, 200)
	if registered.Body.String() != repeated.Body.String() {
		t.Fatal("registration response discloses account existence")
	}
	login := request(t, s, "POST", "/v1/auth/login", "", map[string]string{"email": email, "password": "a valid long password"})
	requireStatus(t, login, 200)
	token := sessionToken(t, login)
	requireStatus(t, request(t, s, "GET", "/v1/auth/me", token, nil), 200)
	requireStatus(t, request(t, s, "GET", "/v1/admin/users", token, nil), 403)
	requireStatus(t, request(t, s, "POST", "/v1/auth/forgot-password", "", map[string]string{"email": email}), 200)
	resetToken := queuedToken(t, s, email)
	requireStatus(t, request(t, s, "POST", "/v1/auth/reset-password", "", map[string]string{"token": resetToken, "password": "a brand new long password"}), 200)
	requireStatus(t, request(t, s, "GET", "/v1/auth/me", token, nil), 401)
	requireStatus(t, request(t, s, "POST", "/v1/auth/reset-password", "", map[string]string{"token": resetToken, "password": "a brand new long password"}), 400)
	login = request(t, s, "POST", "/v1/auth/login", "", map[string]string{"email": email, "password": "a brand new long password"})
	requireStatus(t, login, 200)
	token = sessionToken(t, login)
	requireStatus(t, request(t, s, "POST", "/v1/auth/logout-all", token, nil), 200)
	requireStatus(t, request(t, s, "GET", "/v1/auth/me", token, nil), 401)
}

func TestMailUnconfiguredAndPersistentQuotasFailClosed(t *testing.T) {
	s := testIdentity(t, Config{})
	requireStatus(t, request(t, s, "POST", "/v1/auth/register", "", credentials{Email: "one@example.com", Password: "a valid long password"}), 503)
	s.cfg = testMailConfig()
	s.cfg.MailDailyLimit = 1
	s.cfg.MailMonthlyLimit = 2000
	requireStatus(t, request(t, s, "POST", "/v1/auth/register", "", credentials{Email: "one@example.com", Password: "a valid long password"}), 200)
	// A new service instance sees the same persisted quota.
	restarted := New(s.db, s.cfg)
	requireStatus(t, request(t, restarted, "POST", "/v1/auth/register", "", credentials{Email: "two@example.com", Password: "a valid long password"}), 429)
	var count int
	if err := s.db.QueryRow(context.Background(), `SELECT count(*) FROM users WHERE email='two@example.com'`).Scan(&count); err != nil || count != 0 {
		t.Fatal("quota failure did not rollback account", count, err)
	}
	alerts, err := s.MailAlerts(context.Background())
	if err != nil || len(alerts) == 0 {
		t.Fatal("quota not visible to owner", alerts, err)
	}
}

func TestOwnerGuardAndRoleChangesImmediatelyInvalidateSessions(t *testing.T) {
	s := testIdentity(t, testMailConfig())
	ownerID, owner := seedAccount(t, s, "owner@example.com", "owner")
	_, admin := seedAccount(t, s, "admin@example.com", "admin")
	readerID, reader := seedAccount(t, s, "reader@example.com", "reader")
	requireStatus(t, request(t, s, "PUT", "/v1/admin/users/"+readerID, admin, map[string]string{"role": "admin"}), 403)
	requireStatus(t, request(t, s, "PUT", "/v1/admin/users/"+ownerID, admin, map[string]bool{"suspended": true}), 403)
	requireStatus(t, request(t, s, "PUT", "/v1/admin/users/"+ownerID, owner, map[string]string{"role": "reader"}), 409)
	requireStatus(t, request(t, s, "DELETE", "/v1/auth/account", owner, map[string]string{"password": "a valid long password"}), 409)
	requireStatus(t, request(t, s, "PUT", "/v1/admin/users/"+readerID, owner, map[string]string{"role": "admin"}), 200)
	requireStatus(t, request(t, s, "GET", "/v1/auth/me", reader, nil), 401)
	login := request(t, s, "POST", "/v1/auth/login", "", map[string]string{"email": "reader@example.com", "password": "a valid long password"})
	requireStatus(t, login, 200)
	reader = sessionToken(t, login)
	requireStatus(t, request(t, s, "PUT", "/v1/admin/users/"+readerID, owner, map[string]bool{"suspended": true}), 200)
	requireStatus(t, request(t, s, "GET", "/v1/admin/users", reader, nil), 401)
	requireStatus(t, request(t, s, "POST", "/v1/auth/login", "", map[string]string{"email": "reader@example.com", "password": "a valid long password"}), 403)
}

func TestOwnerTransferRequiresVerifiedTargetAndAllowsOriginalOwnerDeletion(t *testing.T) {
	s := testIdentity(t, testMailConfig())
	_, owner := seedAccount(t, s, "owner@example.com", "owner")
	newID, newToken := seedAccount(t, s, "next@example.com", "reader")
	_, err := s.db.Exec(context.Background(), `UPDATE users SET verified=false WHERE id=$1`, newID)
	if err != nil {
		t.Fatal(err)
	}
	requireStatus(t, request(t, s, "PUT", "/v1/admin/users/"+newID, owner, map[string]string{"role": "owner"}), 409)
	_, err = s.db.Exec(context.Background(), `UPDATE users SET verified=true WHERE id=$1`, newID)
	if err != nil {
		t.Fatal(err)
	}
	requireStatus(t, request(t, s, "PUT", "/v1/admin/users/"+newID, owner, map[string]string{"role": "owner"}), 200)
	requireStatus(t, request(t, s, "GET", "/v1/auth/me", newToken, nil), 401)
	requireStatus(t, request(t, s, "DELETE", "/v1/auth/account", owner, map[string]string{"password": "a valid long password"}), 200)
	login := request(t, s, "POST", "/v1/auth/login", "", map[string]string{"email": "next@example.com", "password": "a valid long password"})
	requireStatus(t, login, 200)
	requireStatus(t, request(t, s, "DELETE", "/v1/auth/account", sessionToken(t, login), map[string]string{"password": "a valid long password"}), 409)
}

func TestSocialAccountsRequireExplicitLinkAndOneUseChallenge(t *testing.T) {
	cfg := testMailConfig()
	cfg.GoogleClientIDs = []string{"client"}
	s := testIdentity(t, cfg)
	_, reader := seedAccount(t, s, "reader@example.com", "reader")
	s.socialVerifier = func(_ context.Context, provider, token, nonce string) (socialClaims, error) {
		return socialClaims{Subject: "subject", Email: "reader@example.com", EmailVerified: true}, nil
	}
	challenge := func(bearer string) string {
		w := request(t, s, "GET", "/v1/auth/challenge", bearer, nil)
		requireStatus(t, w, 200)
		var v struct {
			Nonce string `json:"nonce"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &v)
		return v.Nonce
	}
	nonce := challenge("")
	requireStatus(t, request(t, s, "POST", "/v1/auth/social/google", "", socialInput{IDToken: "provider-token", Nonce: nonce}), 409)
	nonce = challenge(reader)
	requireStatus(t, request(t, s, "POST", "/v1/auth/link/google", reader, socialInput{IDToken: "provider-token", Nonce: nonce}), 200)
	nonce = challenge("")
	w := request(t, s, "POST", "/v1/auth/social/google", "", socialInput{IDToken: "provider-token", Nonce: nonce})
	requireStatus(t, w, 200)
	requireStatus(t, request(t, s, "POST", "/v1/auth/social/google", "", socialInput{IDToken: "provider-token", Nonce: nonce}), 401)
	if token := sessionToken(t, w); !validToken(token) {
		t.Fatal("invalid session token")
	}
}

func TestInvitationExistingAccountCannotReplacePasswordAndDeletionErasesData(t *testing.T) {
	s := testIdentity(t, testMailConfig())
	_, owner := seedAccount(t, s, "owner@example.com", "owner")
	readerID, reader := seedAccount(t, s, "reader@example.com", "reader")
	requireStatus(t, request(t, s, "POST", "/v1/admin/invitations", owner, map[string]string{"email": "reader@example.com"}), 200)
	token := queuedToken(t, s, "reader@example.com")
	requireStatus(t, request(t, s, "POST", "/v1/auth/accept-invitation", "", map[string]string{"token": token, "password": "attacker replaces password"}), 409)
	requireStatus(t, request(t, s, "POST", "/v1/auth/accept-invitation", reader, map[string]string{"token": token}), 200)
	requireStatus(t, request(t, s, "GET", "/v1/auth/me", reader, nil), 401)
	login := request(t, s, "POST", "/v1/auth/login", "", map[string]string{"email": "reader@example.com", "password": "a valid long password"})
	requireStatus(t, login, 200)
	reader = sessionToken(t, login)
	if _, err := s.db.Exec(context.Background(), `INSERT INTO favorites(user_id,song_id) VALUES($1,gen_random_uuid())`, readerID); err != nil {
		t.Fatal(err)
	}
	requireStatus(t, request(t, s, "DELETE", "/v1/auth/account", reader, map[string]string{"password": "wrong password"}), 401)
	requireStatus(t, request(t, s, "DELETE", "/v1/auth/account", reader, map[string]string{"password": "a valid long password"}), 200)
	for _, table := range []string{"auth_sessions", "provider_identities", "favorites"} {
		var n int
		if err := s.db.QueryRow(context.Background(), "SELECT count(*) FROM "+table+" WHERE user_id=$1", readerID).Scan(&n); err != nil || n != 0 {
			t.Fatalf("%s remains: %d %v", table, n, err)
		}
	}
	var count int
	_ = s.db.QueryRow(context.Background(), `SELECT count(*) FROM mail_outbox WHERE recipient='reader@example.com'`).Scan(&count)
	if count != 0 {
		t.Fatal("mail PII remains")
	}
}

func TestExpiredResetAndBootstrapRecoveryAreAudited(t *testing.T) {
	s := testIdentity(t, testMailConfig())
	ctx := context.Background()
	if err := s.BootstrapOwner(ctx, "owner@example.com", "a valid long password", "Owner"); err != nil {
		t.Fatal(err)
	}
	if err := s.BootstrapOwner(ctx, "other@example.com", "a valid long password", "Other"); err == nil {
		t.Fatal("duplicate bootstrap allowed")
	}
	login := request(t, s, "POST", "/v1/auth/login", "", map[string]string{"email": "owner@example.com", "password": "a valid long password"})
	requireStatus(t, login, 200)
	owner := sessionToken(t, login)
	requireStatus(t, request(t, s, "POST", "/v1/auth/forgot-password", "", map[string]string{"email": "owner@example.com"}), 200)
	token := queuedToken(t, s, "owner@example.com")
	_, err := s.db.Exec(ctx, `UPDATE auth_tokens SET expires_at=now()-interval '1 second'`)
	if err != nil {
		t.Fatal(err)
	}
	requireStatus(t, request(t, s, "POST", "/v1/auth/reset-password", "", map[string]string{"token": token, "password": "new valid long password"}), 400)
	if err = s.RecoverOwner(ctx, "owner@example.com", "recovered long password"); err != nil {
		t.Fatal(err)
	}
	requireStatus(t, request(t, s, "GET", "/v1/auth/me", owner, nil), 401)
	var actions string
	if err = s.db.QueryRow(ctx, `SELECT string_agg(action,',') FROM security_audit`).Scan(&actions); err != nil || !strings.Contains(actions, "owner_bootstrapped") || !strings.Contains(actions, "owner_recovered") {
		t.Fatal(actions, err)
	}
}
