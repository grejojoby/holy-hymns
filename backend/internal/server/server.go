package server

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"holyhymns/internal/identity"
)

type Server struct {
	DB               *pgxpool.Pool
	Auth             *identity.Service
	origin           string
	mu               sync.Mutex
	limits           map[string]window
	nextLimitCleanup time.Time
	streams          chan struct{}
	requests         chan struct{}
}
type window struct {
	start time.Time
	count int
}

func New(db *pgxpool.Pool, auth *identity.Service, origin string) *Server {
	return &Server{DB: db, Auth: auth, origin: origin, limits: map[string]window{}, streams: make(chan struct{}, 250), requests: make(chan struct{}, 32)}
}
func (s *Server) Handler() http.Handler {
	m := http.NewServeMux()
	s.Auth.Register(m)
	m.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if s.DB.Ping(ctx) != nil {
			fail(w, 503, "database unavailable")
			return
		}
		write(w, 200, map[string]string{"status": "ok"})
	})
	m.HandleFunc("GET /v1/songs", s.listSongs)
	m.HandleFunc("GET /v1/songs/{id}", s.getSong)
	m.HandleFunc("GET /v1/categories", s.Auth.Optional(s.categories))
	m.HandleFunc("GET /v1/config", s.config)
	m.HandleFunc("GET /v1/events", s.events)
	m.HandleFunc("GET /v1/me/favorites", s.Auth.Require("reader", s.favorites))
	m.HandleFunc("PUT /v1/me/favorites/{id}", s.Auth.Require("reader", s.favorite))
	m.HandleFunc("DELETE /v1/me/favorites/{id}", s.Auth.Require("reader", s.favorite))
	m.HandleFunc("GET /v1/admin/songs", s.Auth.Require("admin", s.listAdminSongs))
	m.HandleFunc("GET /v1/admin/songs/{id}", s.Auth.Require("admin", s.getAdminSong))
	m.HandleFunc("POST /v1/admin/songs", s.Auth.Require("admin", s.saveSong))
	m.HandleFunc("POST /v1/admin/imports/lyrics", s.Auth.Require("admin", s.importLyrics))
	m.HandleFunc("PUT /v1/admin/songs/{id}", s.Auth.Require("admin", s.saveSong))
	m.HandleFunc("POST /v1/admin/songs/{id}/publish", s.Auth.Require("admin", s.publish))
	m.HandleFunc("POST /v1/admin/songs/{id}/unpublish", s.Auth.Require("admin", s.unpublish))
	m.HandleFunc("GET /v1/admin/songs/{id}/revisions", s.Auth.Require("admin", s.revisions))
	m.HandleFunc("POST /v1/admin/songs/{id}/restore", s.Auth.Require("admin", s.restore))
	m.HandleFunc("POST /v1/admin/categories", s.Auth.Require("admin", s.saveCategory))
	m.HandleFunc("PUT /v1/admin/categories/{id}", s.Auth.Require("admin", s.saveCategory))
	m.HandleFunc("DELETE /v1/admin/categories/{id}", s.Auth.Require("admin", s.deleteCategory))
	m.HandleFunc("PUT /v1/admin/config", s.Auth.Require("admin", s.saveConfig))
	m.HandleFunc("POST /v1/analytics", s.Auth.Optional(s.track))
	m.HandleFunc("GET /v1/admin/analytics", s.Auth.Require("admin", s.analytics))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		if origin := r.Header.Get("Origin"); origin != "" {
			if origin != s.origin {
				fail(w, 403, "origin not allowed")
				return
			}
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Last-Event-ID")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		}
		if r.Method == "OPTIONS" {
			w.WriteHeader(204)
			return
		}
		if r.URL.Path != "/v1/events" {
			select {
			case s.requests <- struct{}{}:
				defer func() { <-s.requests }()
			default:
				w.Header().Set("Retry-After", "1")
				fail(w, 503, "server busy; retry shortly")
				return
			}
			ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
			defer cancel()
			r = r.WithContext(ctx)
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		defer func() {
			if v := recover(); v != nil {
				slog.Error("request panic", "path", r.URL.Path, "error", v)
				fail(w, 500, "unexpected server error")
			}
		}()
		m.ServeHTTP(w, r)
	})
}
func write(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if v != nil {
		json.NewEncoder(w).Encode(v)
	}
}
func fail(w http.ResponseWriter, status int, message string) {
	write(w, status, map[string]string{"error": message})
}
func read(w http.ResponseWriter, r *http.Request, dest any) bool {
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(dest); err != nil {
		fail(w, 400, "invalid JSON body")
		return false
	}
	return true
}
func dbError(w http.ResponseWriter, err error) {
	if errors.Is(err, context.Canceled) {
		return
	}
	if errors.Is(err, context.DeadlineExceeded) {
		fail(w, 503, "request timed out; retry shortly")
		return
	}
	if errors.Is(err, pgx.ErrNoRows) {
		fail(w, 404, "not found")
		return
	}
	slog.Error("database operation failed", "error", err)
	fail(w, 500, "database operation failed")
}
func page(r *http.Request) (int, int) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 {
		limit = 30
	}
	if limit > 100 {
		limit = 100
	}
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if offset < 0 {
		offset = 0
	}
	if offset > 100000 {
		offset = 100000
	}
	return limit, offset
}
func id(w http.ResponseWriter, r *http.Request) string {
	v := r.PathValue("id")
	if !validID(v) {
		fail(w, 400, "invalid identifier")
		return ""
	}
	return v
}
func actor(r *http.Request) string {
	u := identity.Current(r)
	if u == nil {
		return ""
	}
	return u.ID
}
func audit(ctx context.Context, tx pgx.Tx, r *http.Request, action, target string) error {
	_, err := tx.Exec(ctx, `INSERT INTO admin_audit(actor_id,action,target_id) VALUES(NULLIF($1,'')::uuid,$2,$3)`, actor(r), action, target)
	return err
}
func change(ctx context.Context, tx pgx.Tx, kind, songID string) error {
	var revision int64
	if e := tx.QueryRow(ctx, `UPDATE content_state SET revision=revision+1 RETURNING revision`).Scan(&revision); e != nil {
		return e
	}
	_, err := tx.Exec(ctx, `INSERT INTO content_changes(revision,kind,song_id) VALUES($1,$2,NULLIF($3,'')::uuid)`, revision, kind, songID)
	return err
}

// Editorial writes are infrequent. A shared lock order prevents taxonomy deletion
// racing a draft save or publication while references are held in JSON snapshots.
func beginContent(ctx context.Context, db *pgxpool.Pool) (pgx.Tx, error) {
	tx, err := db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(48197001)`); err != nil {
		tx.Rollback(ctx)
		return nil, err
	}
	return tx, nil
}
func (s *Server) allow(r *http.Request, key string, n int) bool {
	key = key + ":" + s.Auth.ClientIP(r)
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	if len(s.limits) >= 4096 && now.After(s.nextLimitCleanup) {
		for k, w := range s.limits {
			if now.Sub(w.start) > time.Minute {
				delete(s.limits, k)
			}
		}
		s.nextLimitCleanup = now.Add(time.Minute)
	}
	if _, exists := s.limits[key]; !exists && len(s.limits) >= 8192 {
		return false
	}
	v := s.limits[key]
	if now.Sub(v.start) > time.Minute {
		v = window{start: now}
	}
	if v.count >= n {
		return false
	}
	v.count++
	s.limits[key] = v
	return true
}
func clampQuery(r *http.Request) string {
	q := normalize(r.URL.Query().Get("q"))
	if len(q) > 500 {
		q = q[:500]
	}
	return strings.ToValidUTF8(q, "")
}
