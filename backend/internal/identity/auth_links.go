package identity

import (
	_ "embed"
	"net/http"
)

// These assets are embedded so the auth landing page ships with the API binary.
//
//go:embed web/auth-link.html
var authLinkHTML string

//go:embed web/auth-link.js
var authLinkJS string

//go:embed web/auth-link.css
var authLinkCSS string

func registerAuthLinks(mux *http.ServeMux) {
	mux.HandleFunc("GET /auth/{action}", func(w http.ResponseWriter, r *http.Request) {
		switch r.PathValue("action") {
		case "verify", "reset-password", "accept-invitation":
			writeAuthAsset(w, r, "text/html; charset=utf-8", authLinkHTML)
		default:
			http.NotFound(w, r)
		}
	})
	mux.HandleFunc("GET /auth/link.js", func(w http.ResponseWriter, r *http.Request) {
		writeAuthAsset(w, r, "text/javascript; charset=utf-8", authLinkJS)
	})
	mux.HandleFunc("GET /auth/link.css", func(w http.ResponseWriter, r *http.Request) {
		writeAuthAsset(w, r, "text/css; charset=utf-8", authLinkCSS)
	})
}

func writeAuthAsset(w http.ResponseWriter, r *http.Request, contentType, body string) {
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("X-Robots-Tag", "noindex, nofollow")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; style-src 'self'; connect-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
	if r.Method != http.MethodHead {
		_, _ = w.Write([]byte(body))
	}
}
