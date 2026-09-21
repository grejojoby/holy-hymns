package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"holyhymns/internal/identity"
	"holyhymns/internal/importer"
	"holyhymns/internal/migrations"
)

func integration(t *testing.T) (*Server, *pgxpool.Pool, string) {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not configured")
	}
	ctx := context.Background()
	admin, e := pgxpool.New(ctx, url)
	if e != nil {
		t.Fatal(e)
	}
	schema := fmt.Sprintf("catalog_test_%d", time.Now().UnixNano())
	if _, e = admin.Exec(ctx, "CREATE SCHEMA "+schema); e != nil {
		admin.Close()
		t.Fatal(e)
	}
	cfg, e := pgxpool.ParseConfig(url)
	if e != nil {
		t.Fatal(e)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
	db, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		db.Close()
		_, e := admin.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE")
		admin.Close()
		if e != nil {
			t.Error(e)
		}
	})
	// Keep migration bookkeeping inside this disposable schema, even when public has its own.
	if _, e = db.Exec(ctx, "CREATE TABLE "+schema+".schema_migrations(name text PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now())"); e != nil {
		t.Fatal(e)
	}
	if e = migrations.Apply(ctx, db); e != nil {
		t.Fatal(e)
	}
	auth := identity.New(db, identity.Config{})
	email := fmt.Sprintf("catalog-%d@example.test", time.Now().UnixNano())
	password := "Test-Only-Passphrase-234!"
	if e = auth.BootstrapOwner(ctx, email, password, "Test Owner"); e != nil {
		t.Fatal(e)
	}
	s := New(db, auth, "http://localhost:8081")
	rr := request(s, "POST", "/v1/auth/login", "", map[string]string{"email": email, "password": password})
	if rr.Code != 200 {
		t.Fatalf("login %d %s", rr.Code, rr.Body)
	}
	var session struct {
		Token string        `json:"token"`
		User  identity.User `json:"user"`
	}
	json.Unmarshal(rr.Body.Bytes(), &session)
	t.Cleanup(func() { db.Exec(ctx, `DELETE FROM users WHERE id=$1`, session.User.ID) })
	return s, db, session.Token
}
func request(s *Server, method, path, token string, body any) *httptest.ResponseRecorder {
	var b bytes.Buffer
	if body != nil {
		json.NewEncoder(&b).Encode(body)
	}
	r := httptest.NewRequest(method, path, &b)
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}
func decodeTestSong(t *testing.T, w *httptest.ResponseRecorder) Song {
	t.Helper()
	if w.Code != 200 {
		t.Fatalf("request: %d %s", w.Code, w.Body)
	}
	var s Song
	if e := json.Unmarshal(w.Body.Bytes(), &s); e != nil {
		t.Fatal(e)
	}
	return s
}

func TestPublicationIsolationAndSearch(t *testing.T) {
	s, db, token := integration(t)
	ctx := context.Background()
	draft := Song{Title: "Nanniyode test hymn", LyricsMalayalam: "കർത്താവേ കനിയണമേ\nനന്ദിയോടെ സ്തുതി", LyricsManglish: "Karthave kaniyaname\nNanniyode sthuthi", Aliases: []string{"Nandiyode"}, ReviewNotes: []string{"Private editorial note"}}
	song := decodeTestSong(t, request(s, "POST", "/v1/admin/songs", token, draft))
	t.Cleanup(func() { db.Exec(ctx, `DELETE FROM songs WHERE id=$1`, song.ID) })
	if rr := request(s, "GET", "/v1/songs/"+song.ID, "", nil); rr.Code != 404 {
		t.Fatal("draft exposed", rr.Body)
	}
	if rr := request(s, "GET", "/v1/admin/songs", "", nil); rr.Code != 401 {
		t.Fatal("guest admin access", rr.Code)
	}
	rr := request(s, "POST", "/v1/admin/songs/"+song.ID+"/publish", token, map[string]int{"version": song.Version})
	if rr.Code != 200 {
		t.Fatal(rr.Body)
	}
	draft = song
	draft.LyricsManglish = "Unpublished secret verse"
	edited := decodeTestSong(t, request(s, "PUT", "/v1/admin/songs/"+song.ID, token, draft))
	public := decodeTestSong(t, request(s, "GET", "/v1/songs/"+song.ID, "", nil))
	if len(public.ReviewNotes) != 0 {
		t.Fatal("private review notes leaked")
	}
	if strings.Contains(public.LyricsManglish, "secret") {
		t.Fatal("draft leaked")
	}
	if rr = request(s, "PUT", "/v1/admin/songs/"+song.ID, token, draft); rr.Code != 409 {
		t.Fatal("stale update accepted", rr.Body)
	}
	for _, q := range []string{"Karthave", "Nandiyode", "കർത്താവേ", "Karthave കർത്താവേ", "Nanniyde test hymn"} {
		rr = request(s, "GET", "/v1/songs?q="+url.QueryEscape(q), "", nil)
		if rr.Code != 200 || !strings.Contains(rr.Body.String(), song.ID) {
			t.Fatalf("query %q: %s", q, rr.Body)
		}
	}
	rr = request(s, "POST", "/v1/admin/songs/"+song.ID+"/unpublish", token, map[string]int{"version": edited.Version})
	if rr.Code != 200 {
		t.Fatal(rr.Body)
	}
	if rr = request(s, "GET", "/v1/songs/"+song.ID, "", nil); rr.Code != 404 {
		t.Fatal("withdrawn song visible")
	}
	var revision int64
	if e := db.QueryRow(ctx, `SELECT revision FROM content_state`).Scan(&revision); e != nil || revision < 2 {
		t.Fatal("missing durable revision", revision, e)
	}
}
func TestImportDoesNotOverwriteEdits(t *testing.T) {
	s, db, token := integration(t)
	ctx := context.Background()
	source := fmt.Sprintf("test-import-%d", time.Now().UnixNano())
	entry := importer.Entry{SourceID: source, SourceURL: "https://grejolyrics.blogspot.com/test", Title: "Import test", LyricsManglish: "Original lyrics", Hash: "a", ReviewNotes: []string{"Check duplicate"}}
	report, e := ImportEntries(ctx, db, []importer.Entry{entry}, false)
	if e != nil || report.Created != 1 {
		t.Fatal(report, e)
	}
	t.Cleanup(func() { db.Exec(ctx, `DELETE FROM songs WHERE source_id=$1`, source) })
	var sid string
	if e = db.QueryRow(ctx, `SELECT id::text FROM songs WHERE source_id=$1`, source).Scan(&sid); e != nil {
		t.Fatal(e)
	}
	edited := decodeTestSong(t, request(s, "PUT", "/v1/admin/songs/"+sid, token, map[string]any{"title": "Import test", "lyricsManglish": "Manual correction", "version": 1}))
	if edited.SourceURL != entry.SourceURL || len(edited.ReviewNotes) != 1 {
		t.Fatal("edit lost import metadata", edited)
	}
	report, e = ImportEntries(ctx, db, []importer.Entry{entry}, false)
	if e != nil || report.Unchanged != 1 {
		t.Fatal(report, e)
	}
	entry.Hash = "b"
	entry.LyricsManglish = "Changed source"
	report, e = ImportEntries(ctx, db, []importer.Entry{entry}, false)
	if e != nil || report.Conflicts != 1 {
		t.Fatal(report, e)
	}
	var text string
	if e = db.QueryRow(ctx, `SELECT draft->>'lyricsManglish' FROM songs WHERE source_id=$1`, source).Scan(&text); e != nil || text != "Manual correction" {
		t.Fatal(text, e)
	}
}

func TestReaderCannotCallAnyAdminOperation(t *testing.T) {
	s, db, token := integration(t)
	if _, e := db.Exec(context.Background(), `UPDATE users SET role='reader'`); e != nil {
		t.Fatal(e)
	}
	const fake = "00000000-0000-0000-0000-000000000000"
	for _, test := range []struct{ method, path string }{
		{"GET", "/v1/admin/songs"}, {"GET", "/v1/admin/songs/" + fake}, {"POST", "/v1/admin/songs"}, {"PUT", "/v1/admin/songs/" + fake},
		{"POST", "/v1/admin/songs/" + fake + "/publish"}, {"POST", "/v1/admin/songs/" + fake + "/unpublish"}, {"GET", "/v1/admin/songs/" + fake + "/revisions"}, {"POST", "/v1/admin/songs/" + fake + "/restore"},
		{"POST", "/v1/admin/categories"}, {"PUT", "/v1/admin/categories/" + fake}, {"DELETE", "/v1/admin/categories/" + fake}, {"PUT", "/v1/admin/config"}, {"GET", "/v1/admin/analytics"}, {"GET", "/v1/admin/users"}, {"POST", "/v1/admin/invitations"},
	} {
		if rr := request(s, test.method, test.path, token, map[string]any{}); rr.Code != 403 {
			t.Fatalf("reader allowed %s %s: %d", test.method, test.path, rr.Code)
		}
	}
}

func TestSearchPrioritizesExactThenPartialTitles(t *testing.T) {
	s, _, token := integration(t)
	for _, title := range []string{"A lyric match", "Merciful", "Sing Mercy", "Mercy"} {
		song := decodeTestSong(t, request(s, "POST", "/v1/admin/songs", token, Song{Title: title, LyricsManglish: "Mercy in every verse"}))
		if rr := request(s, "POST", "/v1/admin/songs/"+song.ID+"/publish", token, map[string]int{"version": song.Version}); rr.Code != 200 {
			t.Fatal(rr.Body)
		}
	}
	rr := request(s, "GET", "/v1/songs?q=mercy", "", nil)
	var result struct {
		Items []Song `json:"items"`
	}
	if e := json.Unmarshal(rr.Body.Bytes(), &result); e != nil {
		t.Fatal(e)
	}
	if len(result.Items) != 4 || result.Items[0].Title != "Mercy" || result.Items[1].Title != "Sing Mercy" {
		t.Fatal("incorrect search priority", rr.Body)
	}
}
func TestAdminAnalyticsExcluded(t *testing.T) {
	s, db, token := integration(t)
	var before, after int64
	db.QueryRow(context.Background(), `SELECT COALESCE(sum(count),0) FROM analytics_daily`).Scan(&before)
	rr := request(s, http.MethodPost, "/v1/analytics", token, map[string]any{"event": "search", "resultCount": 0})
	if rr.Code != 204 {
		t.Fatal(rr.Body)
	}
	db.QueryRow(context.Background(), `SELECT COALESCE(sum(count),0) FROM analytics_daily`).Scan(&after)
	if before != after {
		t.Fatal("admin activity counted")
	}
}

func TestTaxonomyAndSettingsConflicts(t *testing.T) {
	s, _, token := integration(t)
	rr := request(s, "POST", "/v1/admin/categories", token, Category{Name: "Test occasion", Kind: "occasion"})
	if rr.Code != 200 {
		t.Fatal(rr.Code, rr.Body)
	}
	var category Category
	json.Unmarshal(rr.Body.Bytes(), &category)
	category.Name = "Updated occasion"
	if rr = request(s, "PUT", "/v1/admin/categories/"+category.ID, token, category); rr.Code != 200 {
		t.Fatal(rr.Body)
	}
	if rr = request(s, "PUT", "/v1/admin/categories/"+category.ID, token, category); rr.Code != 409 {
		t.Fatal("stale category accepted", rr.Code)
	}
	if rr = request(s, "DELETE", "/v1/admin/categories/"+category.ID+"?version=1", token, nil); rr.Code != 409 {
		t.Fatal("stale category delete accepted", rr.Code)
	}
	if rr = request(s, "GET", "/v1/categories", "", nil); strings.Contains(rr.Body.String(), category.ID) {
		t.Fatal("unpublished taxonomy leaked")
	}
	song := decodeTestSong(t, request(s, "POST", "/v1/admin/songs", token, Song{Title: "Taxonomy test", LyricsManglish: "Test lyric", CategoryIDs: []string{category.ID}}))
	if rr = request(s, "POST", "/v1/admin/songs/"+song.ID+"/publish", token, map[string]int{"version": song.Version}); rr.Code != 200 {
		t.Fatal(rr.Body)
	}
	if rr = request(s, "GET", "/v1/categories", "", nil); !strings.Contains(rr.Body.String(), category.ID) {
		t.Fatal("published taxonomy missing")
	}
	var revisions struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	rr = request(s, "GET", "/v1/admin/songs/"+song.ID+"/revisions", token, nil)
	json.Unmarshal(rr.Body.Bytes(), &revisions)
	if len(revisions.Items) != 1 {
		t.Fatal(rr.Body)
	}
	if rr = request(s, "DELETE", "/v1/admin/categories/"+category.ID+"?version=2", token, nil); rr.Code != 204 {
		t.Fatal(rr.Body)
	}
	song = decodeTestSong(t, request(s, "GET", "/v1/admin/songs/"+song.ID, token, nil))
	restored := decodeTestSong(t, request(s, "POST", "/v1/admin/songs/"+song.ID+"/restore", token, map[string]any{"version": song.Version, "revisionId": revisions.Items[0].ID}))
	if len(restored.CategoryIDs) != 0 {
		t.Fatal("restore retained deleted taxonomy")
	}
	if restored.Status != "published" {
		t.Fatal("restore hid the existing publication state")
	}
	public := decodeTestSong(t, request(s, "GET", "/v1/songs/"+song.ID, "", nil))
	if public.Version == restored.Version {
		t.Fatal("restore published a private revision")
	}
	var config AppConfig
	rr = request(s, "GET", "/v1/config", "", nil)
	json.Unmarshal(rr.Body.Bytes(), &config)
	config.Announcement = "Changed"
	if rr = request(s, "PUT", "/v1/admin/config", token, config); rr.Code != 200 {
		t.Fatal(rr.Body)
	}
	if rr = request(s, "PUT", "/v1/admin/config", token, config); rr.Code != 409 {
		t.Fatal("stale settings accepted", rr.Body)
	}
}

func TestLivePublicationAndReconnect(t *testing.T) {
	s, db, token := integration(t)
	httpServer := httptest.NewServer(s.Handler())
	defer httpServer.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", httpServer.URL+"/v1/events", nil)
	res, e := http.DefaultClient.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	defer res.Body.Close()
	scanner := bufio.NewScanner(res.Body)
	nextRevision := func() string {
		t.Helper()
		for scanner.Scan() {
			if strings.HasPrefix(scanner.Text(), "data: ") {
				return scanner.Text()
			}
		}
		t.Fatal("SSE disconnected", scanner.Err())
		return ""
	}
	first := nextRevision()
	song := decodeTestSong(t, request(s, "POST", "/v1/admin/songs", token, Song{Title: "Live test", LyricsManglish: "Live lyric"}))
	start := time.Now()
	if rr := request(s, "POST", "/v1/admin/songs/"+song.ID+"/publish", token, map[string]int{"version": song.Version}); rr.Code != 200 {
		t.Fatal(rr.Body)
	}
	next := nextRevision()
	if next == first || time.Since(start) >= 5*time.Second {
		t.Fatal("publish not reflected within five seconds")
	}
	// A new application instance reads the same durable revision after restart.
	restarted := New(db, s.Auth, "")
	if before, after := request(s, "GET", "/v1/config", "", nil), request(restarted, "GET", "/v1/config", "", nil); before.Body.String() != after.Body.String() {
		t.Fatal("restart lost content revision")
	}
	if rr := request(restarted, "POST", "/v1/admin/songs/"+song.ID+"/unpublish", token, map[string]int{"version": song.Version}); rr.Code != 200 {
		t.Fatal(rr.Body)
	}
	if nextRevision() == next {
		t.Fatal("unpublish did not advance revision")
	}
	if rr := request(restarted, "GET", "/v1/songs/"+song.ID, "", nil); rr.Code != 404 {
		t.Fatal("unpublish not reconciled")
	}
}
