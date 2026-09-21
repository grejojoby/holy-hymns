package identity

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
)

func TestSocialJWTVerificationRejectsWrongSignatureIssuerAudienceNonceAndExpiry(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	wrongKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	s := New(nil, Config{GoogleClientIDs: []string{"holy-hymns-client"}})
	s.verifiers["google"] = oidc.NewVerifier("https://accounts.google.com", &oidc.StaticKeySet{PublicKeys: []crypto.PublicKey{&key.PublicKey}}, &oidc.Config{SkipClientIDCheck: true, SupportedSigningAlgs: []string{"RS256"}})
	nonce, _ := randomToken()
	base := func() map[string]any {
		return map[string]any{"iss": "https://accounts.google.com", "aud": "holy-hymns-client", "sub": "provider-subject", "email": "reader@example.com", "email_verified": true, "iat": time.Now().Unix(), "exp": time.Now().Add(time.Hour).Unix(), "nonce": nonce}
	}
	sign := func(claims map[string]any, key *rsa.PrivateKey) string {
		header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`))
		body, _ := json.Marshal(claims)
		data := header + "." + base64.RawURLEncoding.EncodeToString(body)
		hash := sha256.Sum256([]byte(data))
		sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, hash[:])
		if err != nil {
			t.Fatal(err)
		}
		return data + "." + base64.RawURLEncoding.EncodeToString(sig)
	}
	if c, err := s.verifySocial(context.Background(), "google", sign(base(), key), nonce); err != nil || c.Subject != "provider-subject" || !c.EmailVerified {
		t.Fatalf("valid token rejected: %+v %v", c, err)
	}
	for name, change := range map[string]func(map[string]any){
		"issuer":           func(c map[string]any) { c["iss"] = "https://attacker.example" },
		"audience":         func(c map[string]any) { c["aud"] = "someone-else" },
		"nonce":            func(c map[string]any) { c["nonce"] = "wrong" },
		"expired":          func(c map[string]any) { c["exp"] = time.Now().Add(-time.Hour).Unix() },
		"missing issuance": func(c map[string]any) { delete(c, "iat") },
		"authorized party": func(c map[string]any) { c["azp"] = "another-client" },
	} {
		t.Run(name, func(t *testing.T) {
			claims := base()
			change(claims)
			if _, err := s.verifySocial(context.Background(), "google", sign(claims, key), nonce); err == nil {
				t.Fatal("unsafe token accepted")
			}
		})
	}
	if _, err := s.verifySocial(context.Background(), "google", sign(base(), wrongKey), nonce); err == nil {
		t.Fatal("invalid signature accepted")
	}
}
