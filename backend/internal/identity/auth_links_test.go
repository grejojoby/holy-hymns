package identity

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestAuthLandingIsSafeToPreview(t *testing.T) {
	// No database: GET/HEAD must never look up or consume account tokens.
	s := New(nil, Config{})
	for _, action := range []string{"verify", "reset-password", "accept-invitation"} {
		for _, method := range []string{http.MethodGet, http.MethodHead} {
			t.Run(method+"/"+action, func(t *testing.T) {
				w := request(t, s, method, "/auth/"+action+"?token=do-not-reflect-me", "", nil)
				requireStatus(t, w, http.StatusOK)
				for header, expected := range map[string]string{
					"Content-Type":           "text/html; charset=utf-8",
					"Cache-Control":          "no-store",
					"Referrer-Policy":        "no-referrer",
					"X-Content-Type-Options": "nosniff",
					"X-Frame-Options":        "DENY",
					"X-Robots-Tag":           "noindex, nofollow",
				} {
					if w.Header().Get(header) != expected {
						t.Errorf("%s = %q", header, w.Header().Get(header))
					}
				}
				if csp := w.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "default-src 'none'") || !strings.Contains(csp, "frame-ancestors 'none'") || strings.Contains(csp, "unsafe-inline") {
					t.Errorf("unsafe CSP: %q", csp)
				}
				if strings.Contains(w.Body.String(), "do-not-reflect-me") {
					t.Fatal("query token reflected into HTML")
				}
				if method == http.MethodGet && !strings.Contains(w.Body.String(), "Open Holy Hymns") {
					t.Fatal("missing app handoff")
				}
			})
		}
	}
	requireStatus(t, request(t, s, "GET", "/auth/unknown", "", nil), http.StatusNotFound)
	requireStatus(t, request(t, s, "POST", "/auth/verify", "", nil), http.StatusMethodNotAllowed)
}

func TestAuthLandingAssets(t *testing.T) {
	s := New(nil, Config{})
	for path, contentType := range map[string]string{
		"/auth/link.js":  "text/javascript; charset=utf-8",
		"/auth/link.css": "text/css; charset=utf-8",
	} {
		w := request(t, s, "GET", path, "", nil)
		requireStatus(t, w, http.StatusOK)
		if w.Header().Get("Content-Type") != contentType || w.Body.Len() == 0 {
			t.Fatalf("invalid asset %s", path)
		}
	}
}

func TestActionEmailsUseHTTPSFragmentLinks(t *testing.T) {
	s := testIdentity(t, testMailConfig())
	ctx := context.Background()
	id, _ := seedAccount(t, s, "reader@example.com", "reader")
	for purpose, action := range map[string]string{"verify": "verify", "reset": "reset-password", "invite": "accept-invitation"} {
		tx, err := s.db.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err = s.queueAction(ctx, tx, id, "reader@example.com", purpose, time.Hour); err != nil {
			tx.Rollback(ctx)
			t.Fatal(err)
		}
		if err = tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		var encrypted []byte
		if err = s.db.QueryRow(ctx, `SELECT encrypted_body FROM mail_outbox WHERE purpose=$1`, purpose).Scan(&encrypted); err != nil {
			t.Fatal(err)
		}
		payload, err := s.decryptMail("reader@example.com", encrypted)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(payload.Body, "https://hymns.example/auth/"+action+"#token=") || strings.Contains(payload.Body, "holyhymns://") || strings.Contains(payload.Body, "?token=") {
			t.Errorf("%s email must use a browser-openable link with the token in its fragment", purpose)
		}
	}
}
