package identity

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
)

func appleTestConfig(t *testing.T) Config {
	t.Helper()
	cfg := testMailConfig()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	cfg.AppleTeamID = "TESTTEAMID"
	cfg.AppleKeyID = "TESTKEY123"
	cfg.ApplePrivateKey = string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
	cfg.AppleClientIDs = []string{"com.holyhymns.app"}
	return cfg
}

func TestAppleClientSecretUsesBoundedES256JWTAndValidConfiguration(t *testing.T) {
	cfg := appleTestConfig(t)
	s := New(nil, cfg)
	if !s.appleConfigured() {
		t.Fatal("valid Apple configuration rejected")
	}
	secret, err := s.appleClientSecret("com.holyhymns.app")
	if err != nil {
		t.Fatal(err)
	}
	signed, err := jose.ParseSigned(secret, []jose.SignatureAlgorithm{jose.ES256})
	if err != nil {
		t.Fatal(err)
	}
	key, _ := s.appleSigningKey()
	payload, err := signed.Verify(&key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	var claims struct {
		Issuer   string `json:"iss"`
		Subject  string `json:"sub"`
		Audience string `json:"aud"`
		Expires  int64  `json:"exp"`
	}
	if err = json.Unmarshal(payload, &claims); err != nil {
		t.Fatal(err)
	}
	if claims.Issuer != cfg.AppleTeamID || claims.Subject != "com.holyhymns.app" || claims.Audience != "https://appleid.apple.com" || claims.Expires > time.Now().Add(6*time.Minute).Unix() {
		t.Fatal("invalid client secret claims")
	}
	if signed.Signatures[0].Header.KeyID != cfg.AppleKeyID {
		t.Fatal("missing Apple key ID")
	}
	if _, err = s.appleClientSecret("foreign.client"); err == nil {
		t.Fatal("unconfigured client accepted")
	}
	cfg.ApplePrivateKey = strings.ReplaceAll(cfg.ApplePrivateKey, "\n", `\n`)
	if !New(nil, cfg).appleConfigured() {
		t.Fatal("escaped PEM rejected")
	}
	cfg.MailEncryptionKey = ""
	if New(nil, cfg).appleConfigured() {
		t.Fatal("Apple enabled without secure grant storage")
	}
}

func TestAppleCodeExchangeEncryptionRevocationAndDeletion(t *testing.T) {
	s := testIdentity(t, appleTestConfig(t))
	var revokeFails atomic.Bool
	revokeFails.Store(true)
	var revokeCalls atomic.Int32
	apple := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		if r.PostForm.Get("client_id") != "com.holyhymns.app" || r.PostForm.Get("client_secret") == "" {
			t.Error("Apple request omitted client authentication")
			w.WriteHeader(400)
			return
		}
		switch r.URL.Path {
		case "/token":
			if r.PostForm.Get("code") != "one-use-code" || r.PostForm.Get("grant_type") != "authorization_code" {
				t.Error("wrong code exchange fields")
				w.WriteHeader(400)
				return
			}
			writeJSON(w, 200, map[string]string{"refresh_token": "test-secret-refresh-grant", "id_token": "exchange-id-token"})
		case "/revoke":
			revokeCalls.Add(1)
			if r.PostForm.Get("token") != "test-secret-refresh-grant" || r.PostForm.Get("token_type_hint") != "refresh_token" {
				t.Error("wrong revocation fields")
				w.WriteHeader(400)
				return
			}
			if revokeFails.Load() {
				w.WriteHeader(503)
				return
			}
			w.WriteHeader(200)
		default:
			w.WriteHeader(404)
		}
	}))
	defer apple.Close()
	s.appleTokenURL = apple.URL + "/token"
	s.appleRevokeURL = apple.URL + "/revoke"
	s.appleClient = apple.Client()
	s.socialVerifier = func(_ context.Context, provider, raw, nonce string) (socialClaims, error) {
		return socialClaims{Subject: "apple-subject", Email: "apple@example.com", Audience: "com.holyhymns.app", EmailVerified: true}, nil
	}
	challenge := request(t, s, "GET", "/v1/auth/challenge", "", nil)
	requireStatus(t, challenge, 200)
	var nonce struct {
		Nonce string `json:"nonce"`
	}
	_ = json.Unmarshal(challenge.Body.Bytes(), &nonce)
	requireStatus(t, request(t, s, "POST", "/v1/auth/social/apple", "", socialInput{IDToken: "device-id-token", Nonce: nonce.Nonce}), 400)
	login := request(t, s, "POST", "/v1/auth/social/apple", "", socialInput{IDToken: "device-id-token", Nonce: nonce.Nonce, AuthorizationCode: "one-use-code"})
	requireStatus(t, login, 200)
	token := sessionToken(t, login)
	var encrypted []byte
	var userID string
	if err := s.db.QueryRow(context.Background(), `SELECT encrypted_refresh_token,user_id::text FROM provider_identities WHERE provider='apple'`).Scan(&encrypted, &userID); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encrypted, []byte("test-secret-refresh-grant")) {
		t.Fatal("Apple refresh grant stored in plaintext")
	}
	if _, err := s.decryptAppleGrant("another-user", "com.holyhymns.app", encrypted); err == nil {
		t.Fatal("refresh grant moved to another account")
	}
	requireStatus(t, request(t, s, "DELETE", "/v1/auth/account", token, map[string]string{}), 503)
	requireStatus(t, request(t, s, "GET", "/v1/auth/me", token, nil), 200)
	revokeFails.Store(false)
	requireStatus(t, request(t, s, "DELETE", "/v1/auth/account", token, map[string]string{}), 200)
	requireStatus(t, request(t, s, "GET", "/v1/auth/me", token, nil), 401)
	var count int
	if err := s.db.QueryRow(context.Background(), `SELECT count(*) FROM provider_identities WHERE user_id=$1`, userID).Scan(&count); err != nil || count != 0 {
		t.Fatal("deleted Apple grant retained", count, err)
	}
	if revokeCalls.Load() != 2 {
		t.Fatal("expected failure followed by successful revocation", revokeCalls.Load())
	}
}

func TestAppleCodeExchangeCannotReplaceAnotherVerifiedSubject(t *testing.T) {
	s := New(nil, appleTestConfig(t))
	apple := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]string{"refresh_token": "refresh", "id_token": "wrong-subject"})
	}))
	defer apple.Close()
	s.appleTokenURL = apple.URL
	s.appleClient = apple.Client()
	s.socialVerifier = func(_ context.Context, _, _, _ string) (socialClaims, error) {
		return socialClaims{Subject: "another-person", Audience: "com.holyhymns.app"}, nil
	}
	if _, err := s.exchangeApple(context.Background(), socialInput{AuthorizationCode: "code"}, socialClaims{Subject: "expected-person", Audience: "com.holyhymns.app"}); err == nil {
		t.Fatal("mismatched authorization code accepted")
	}
}

func TestMissingLegacyAppleGrantStillAllowsLocalDeletion(t *testing.T) {
	s := testIdentity(t, Config{})
	id, token := seedAccount(t, s, "legacy@example.com", "reader")
	if _, err := s.db.Exec(context.Background(), `INSERT INTO provider_identities(provider,subject,user_id) VALUES('apple','legacy-subject',$1)`, id); err != nil {
		t.Fatal(err)
	}
	w := request(t, s, "DELETE", "/v1/auth/account", token, map[string]string{"password": "a valid long password"})
	requireStatus(t, w, 200)
	if !strings.Contains(w.Body.String(), "manualRevocationUrl") {
		t.Fatal("missing manual Apple revocation guidance")
	}
	requireStatus(t, request(t, s, "GET", "/v1/auth/me", token, nil), 401)
}
