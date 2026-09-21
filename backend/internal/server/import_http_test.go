package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func extractedEntry() map[string]any {
	return map[string]any{
		"sourceId":  "tag:blogger.com,1999:blog-6936100729546217639.post-8331476033853071764",
		"sourceUrl": "https://grejolyrics.blogspot.com/2023/12/thiruvosthi-nalkuvan-vaidhikan-lyrics.html",
		"hash":      strings.Repeat("a", 64), "title": "Thiruvosthi Nalkuvan Vaidhikan", "titleMalayalam": "തിരുവോസ്തി",
		"lyricsMalayalam": "തിരുവോസ്തി\n\nകർത്താവേ", "lyricsManglish": "Thiruvosthi\n\nKarthave", "credits": "Grejo Lyrics",
		"labels": []string{"Communion"}, "links": []map[string]string{{"label": "Listen", "url": "https://www.youtube.com/watch?v=test"}}, "reviewNotes": []string{"Review extracted verses before publishing."},
	}
}
func importReport(t *testing.T, w *httptest.ResponseRecorder) ImportReport {
	t.Helper()
	if w.Code != 200 {
		t.Fatalf("import status %d: %s", w.Code, w.Body)
	}
	var report ImportReport
	if err := json.Unmarshal(w.Body.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	return report
}

func TestHTTPImportRejectsGuestsAndReaders(t *testing.T) {
	s, db, token := integration(t)
	body := map[string]any{"entry": extractedEntry(), "dryRun": false}
	if w := request(s, http.MethodPost, "/v1/admin/imports/lyrics", "", body); w.Code != 401 {
		t.Fatalf("guest status %d", w.Code)
	}
	if _, err := db.Exec(context.Background(), `UPDATE users SET role='reader'`); err != nil {
		t.Fatal(err)
	}
	if w := request(s, http.MethodPost, "/v1/admin/imports/lyrics", token, body); w.Code != 403 {
		t.Fatalf("reader status %d", w.Code)
	}
}

func TestHTTPImportIsDraftOnlyIdempotentAndPreservesEdits(t *testing.T) {
	s, db, token := integration(t)
	ctx := context.Background()
	if _, err := db.Exec(ctx, `UPDATE users SET role='admin'`); err != nil {
		t.Fatal(err)
	}
	entry := extractedEntry()
	body := map[string]any{"entry": entry, "dryRun": false}
	if report := importReport(t, request(s, "POST", "/v1/admin/imports/lyrics", token, body)); report.Created != 1 || report.Total != 1 {
		t.Fatal(report)
	}
	var sid string
	var published bool
	var sourceHTML string
	if err := db.QueryRow(ctx, `SELECT id::text,published IS NOT NULL,COALESCE(source_html,'') FROM songs WHERE source_id=$1`, entry["sourceId"]).Scan(&sid, &published, &sourceHTML); err != nil {
		t.Fatal(err)
	}
	if published || sourceHTML != "" {
		t.Fatal("HTTP import published or stored HTML")
	}
	if w := request(s, "GET", "/v1/songs/"+sid, "", nil); w.Code != 404 {
		t.Fatal("draft publicly visible", w.Code)
	}
	var auditCount int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM admin_audit a JOIN users u ON u.id=a.actor_id WHERE action='lyrics.import' AND target_id=$1`, entry["sourceId"]).Scan(&auditCount); err != nil || auditCount != 1 {
		t.Fatal("missing actor audit", auditCount, err)
	}
	if report := importReport(t, request(s, "POST", "/v1/admin/imports/lyrics", token, body)); report.Unchanged != 1 {
		t.Fatal(report)
	}
	if w := request(s, "PUT", "/v1/admin/songs/"+sid, token, map[string]any{"title": "Edited in app", "lyricsManglish": "Manual correction", "version": 1}); w.Code != 200 {
		t.Fatal(w.Body)
	}
	entry["hash"] = strings.Repeat("b", 64)
	entry["lyricsManglish"] = "Changed on blog"
	if report := importReport(t, request(s, "POST", "/v1/admin/imports/lyrics", token, body)); report.Conflicts != 1 || report.Created != 0 {
		t.Fatal(report)
	}
	var lyrics string
	if err := db.QueryRow(ctx, `SELECT draft->>'lyricsManglish' FROM songs WHERE id=$1`, sid).Scan(&lyrics); err != nil || lyrics != "Manual correction" {
		t.Fatal("app edits overwritten", lyrics, err)
	}
}

func TestHTTPImportDryRunWritesNothingAndAuditFailureRollsBack(t *testing.T) {
	s, db, token := integration(t)
	ctx := context.Background()
	entry := extractedEntry()
	if report := importReport(t, request(s, "POST", "/v1/admin/imports/lyrics", token, map[string]any{"entry": entry, "dryRun": true})); report.Created != 1 {
		t.Fatal(report)
	}
	assertEmpty := func() {
		t.Helper()
		for _, table := range []string{"songs", "categories", "song_revisions", "import_runs", "admin_audit"} {
			var n int
			if err := db.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&n); err != nil || n != 0 {
				t.Fatalf("%s contains %d rows: %v", table, n, err)
			}
		}
	}
	assertEmpty()
	if _, err := db.Exec(ctx, `ALTER TABLE admin_audit ADD CONSTRAINT reject_import_test CHECK(action <> 'lyrics.import')`); err != nil {
		t.Fatal(err)
	}
	if w := request(s, "POST", "/v1/admin/imports/lyrics", token, map[string]any{"entry": entry, "dryRun": false}); w.Code != 500 {
		t.Fatalf("audit failure status %d: %s", w.Code, w.Body)
	}
	assertEmpty()
	if _, err := db.Exec(ctx, `ALTER TABLE admin_audit DROP CONSTRAINT reject_import_test`); err != nil {
		t.Fatal(err)
	}
	if report := importReport(t, request(s, "POST", "/v1/admin/imports/lyrics", token, map[string]any{"entry": entry, "dryRun": false})); report.Created != 1 {
		t.Fatal("retry could not recover", report)
	}
}

func TestConcurrentHTTPImportsKeepOneSourceAndReportChangedSource(t *testing.T) {
	s, db, token := integration(t)
	responses := make(chan *httptest.ResponseRecorder, 2)
	for _, hash := range []string{strings.Repeat("a", 64), strings.Repeat("b", 64)} {
		entry := extractedEntry()
		entry["hash"] = hash
		go func() {
			responses <- request(s, "POST", "/v1/admin/imports/lyrics", token, map[string]any{"entry": entry})
		}()
	}
	created, conflicts := 0, 0
	for range 2 {
		report := importReport(t, <-responses)
		created += report.Created
		conflicts += report.Conflicts
	}
	if created != 1 || conflicts != 1 {
		t.Fatal("concurrent import reports", created, conflicts)
	}
	var count int
	if err := db.QueryRow(context.Background(), `SELECT count(*) FROM songs`).Scan(&count); err != nil || count != 1 {
		t.Fatal("duplicate songs", count, err)
	}
}

func TestHTTPImportValidatesSourceMetadataLinksAndBody(t *testing.T) {
	s, _, token := integration(t)
	for name, modify := range map[string]func(map[string]any){
		"foreign source ID":     func(e map[string]any) { e["sourceId"] = "tag:blogger.com,1999:blog-1.post-8331476033853071764" },
		"non-numeric post ID":   func(e map[string]any) { e["sourceId"] = "tag:blogger.com,1999:blog-6936100729546217639.post-example" },
		"foreign host":          func(e map[string]any) { e["sourceUrl"] = "https://example.com/2023/12/song.html" },
		"HTTP source":           func(e map[string]any) { e["sourceUrl"] = "http://grejolyrics.blogspot.com/2023/12/song.html" },
		"source credentials":    func(e map[string]any) { e["sourceUrl"] = "https://user@grejolyrics.blogspot.com/2023/12/song.html" },
		"non-permalink":         func(e map[string]any) { e["sourceUrl"] = "https://grejolyrics.blogspot.com/feeds/posts/default" },
		"invalid hash":          func(e map[string]any) { e["hash"] = "not-a-hash" },
		"overlong lyrics":       func(e map[string]any) { e["lyricsManglish"] = strings.Repeat("a", 200001) },
		"overlong credits":      func(e map[string]any) { e["credits"] = strings.Repeat("a", 4001) },
		"null text":             func(e map[string]any) { e["lyricsMalayalam"] = "text\x00" },
		"too many labels":       func(e map[string]any) { e["labels"] = make([]string, 31) },
		"too many review notes": func(e map[string]any) { e["reviewNotes"] = make([]string, 31) },
		"insecure media": func(e map[string]any) {
			e["links"] = []map[string]string{{"label": "Listen", "url": "http://example.com/song"}}
		},
		"overlong media label": func(e map[string]any) {
			e["links"] = []map[string]string{{"label": strings.Repeat("a", 121), "url": "https://example.com/song"}}
		},
		"HTML input": func(e map[string]any) { e["rawHTML"] = "<p>not supported</p>" },
	} {
		t.Run(name, func(t *testing.T) {
			entry := extractedEntry()
			modify(entry)
			w := request(s, "POST", "/v1/admin/imports/lyrics", token, map[string]any{"entry": entry, "dryRun": false})
			if w.Code != 400 {
				t.Fatalf("invalid input status %d: %s", w.Code, w.Body)
			}
		})
	}
	for _, body := range []string{`{}`, `{"entry":null}`, `{"entry":`, strings.Repeat(" ", 1<<20) + `{}`, `{"entry":{}} {}`} {
		r := httptest.NewRequest("POST", "/v1/admin/imports/lyrics", strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		if w.Code != 400 {
			t.Fatalf("invalid body status %d", w.Code)
		}
	}
}
