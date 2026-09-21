package identity

import (
	"context"
	"encoding/base64"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPasswordHashRequiresCorrectPasswordAndRejectsUnboundedParameters(t *testing.T) {
	s := New(nil, Config{})
	hash, err := s.hashPassword(context.Background(), "a valid long password")
	if err != nil {
		t.Fatal(err)
	}
	if !s.checkPassword(context.Background(), hash, "a valid long password") {
		t.Fatal("correct password rejected")
	}
	if s.checkPassword(context.Background(), hash, "different password") {
		t.Fatal("wrong password accepted")
	}
	for _, bad := range []string{"", "plaintext", strings.Replace(hash, "m=65536", "m=4294967295", 1), strings.Replace(hash, "t=3", "t=9999", 1)} {
		if s.checkPassword(context.Background(), bad, "a valid long password") {
			t.Fatal("malformed hash accepted")
		}
	}
}

func TestAccountAndRoleValidation(t *testing.T) {
	for _, bad := range []string{"", "person", "Name <person@example.com>", "x@example.com\r\nBcc:x@y.com"} {
		if _, err := normalizeEmail(bad); err == nil {
			t.Fatalf("accepted invalid email %q", bad)
		}
	}
	if got, err := normalizeEmail(" Person@Example.com "); err != nil || got != "person@example.com" {
		t.Fatal(got, err)
	}
	if validPassword("short") || validPassword(strings.Repeat("a", 1025)) || !validPassword("a sufficiently long password") {
		t.Fatal("password limits")
	}
	if roleAllows("reader", "admin") || roleAllows("admin", "owner") || !roleAllows("owner", "admin") || roleAllows("owner", "invented") {
		t.Fatal("authorization matrix")
	}
}

func TestMailConfigurationFailsClosed(t *testing.T) {
	if New(nil, Config{}).mailConfigured() {
		t.Fatal("unconfigured mail allowed")
	}
	if New(nil, Config{SMTPHost: "localhost", SMTPFrom: "a@b.com", PublicURL: "javascript:bad"}).mailConfigured() {
		t.Fatal("invalid public URL allowed")
	}
	if !New(nil, Config{SMTPHost: "localhost", SMTPPort: 1025, SMTPFrom: "a@b.com", PublicURL: "https://hymns.example", AllowInsecureSMTP: true, MailEncryptionKey: base64.StdEncoding.EncodeToString(make([]byte, 32))}).mailConfigured() {
		t.Fatal("valid SMTP rejected")
	}
}

func TestForwardingHeadersOnlyTrustedFromConfiguredProxy(t *testing.T) {
	s := New(nil, Config{TrustedProxyCIDRs: []string{"172.31.0.0/24"}})
	r := httptest.NewRequest("POST", "/v1/auth/login", nil)
	r.Header.Set("X-Real-IP", "203.0.113.5")
	r.RemoteAddr = "192.0.2.1:1234"
	if s.clientIP(r) != "192.0.2.1" {
		t.Fatal("untrusted proxy spoof accepted")
	}
	r.RemoteAddr = "172.31.0.2:1234"
	if s.clientIP(r) != "203.0.113.5" {
		t.Fatal("trusted proxy ignored")
	}
	r.Header.Set("X-Real-IP", "invalid")
	if s.clientIP(r) != "172.31.0.2" {
		t.Fatal("invalid forwarded address accepted")
	}
}
