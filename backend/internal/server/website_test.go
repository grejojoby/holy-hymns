package server

import (
	"context"
	"encoding/xml"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPublicWebsitePublicationAndSEO(t *testing.T) {
	s, db, token := integration(t)
	draft := Song{Title: "Grace & Peace", TitleMalayalam: "കൃപ", LyricsMalayalam: "കർത്താവേ കനിയണമേ", LyricsManglish: "Grace in every morning\nPeace in every song", ReviewNotes: []string{"PRIVATE EDITORIAL NOTE"}}
	song := decodeTestSong(t, request(s, "POST", "/v1/admin/songs", token, draft))
	t.Cleanup(func() { db.Exec(context.Background(), `DELETE FROM songs WHERE id=$1`, song.ID) })
	path := "/lyrics/grace-peace-" + song.ID
	if rr := request(s, "GET", path, "", nil); rr.Code != 404 {
		t.Fatalf("draft page status = %d", rr.Code)
	}
	if rr := request(s, "POST", "/v1/admin/songs/"+song.ID+"/publish", token, map[string]int{"version": song.Version}); rr.Code != 200 {
		t.Fatal(rr.Body)
	}
	for _, path := range []string{"/", "/lyrics", path} {
		rr := request(s, "GET", path, "", nil)
		if rr.Code != 200 || !strings.Contains(rr.Header().Get("Content-Type"), "text/html") {
			t.Fatalf("%s: %d %s", path, rr.Code, rr.Body)
		}
		for _, want := range []string{"Grace &amp; Peace", `rel="canonical"`, `application/ld+json`, `name="description"`, `property="og:title"`} {
			if !strings.Contains(rr.Body.String(), want) {
				t.Errorf("%s missing %q", path, want)
			}
		}
		if strings.Contains(rr.Body.String(), "PRIVATE EDITORIAL NOTE") {
			t.Fatal("private metadata exposed")
		}
	}
	rr := request(s, "GET", path, "", nil)
	for _, want := range []string{draft.LyricsMalayalam, "Grace in every morning", "MusicComposition", `lang="ml"`, "https://holyhymns.in" + path} {
		if !strings.Contains(rr.Body.String(), want) {
			t.Errorf("lyric page missing %q", want)
		}
	}
	if rr := request(s, "GET", "/lyrics/old-name-"+song.ID, "", nil); rr.Code != 308 || rr.Header().Get("Location") != path {
		t.Fatalf("noncanonical slug: %d %s", rr.Code, rr.Header().Get("Location"))
	}
	for _, path := range []string{"/missing-page", "/lyrics/invalid", "/lyrics?page=99999"} {
		if rr := request(s, "GET", path, "", nil); rr.Code != 404 || !strings.Contains(rr.Header().Get("X-Robots-Tag"), "noindex") {
			t.Errorf("%s should be a noindex 404, got %d", path, rr.Code)
		}
	}
	if rr := request(s, "GET", "/lyrics?q=Grace", "", nil); rr.Code != 200 || !strings.Contains(rr.Body.String(), "Grace &amp; Peace") || !strings.Contains(rr.Header().Get("X-Robots-Tag"), "noindex") {
		t.Fatalf("search: %d %s", rr.Code, rr.Body)
	}
	rr = request(s, "GET", "/sitemap.xml", "", nil)
	var sitemap struct {
		URLs []struct {
			Loc string `xml:"loc"`
		} `xml:"url"`
	}
	if rr.Code != 200 || xml.Unmarshal(rr.Body.Bytes(), &sitemap) != nil || !strings.Contains(rr.Body.String(), "https://holyhymns.in"+path) {
		t.Fatal("invalid or incomplete sitemap", rr.Body)
	}
	if rr := request(s, "GET", "/robots.txt", "", nil); rr.Code != 200 || !strings.Contains(rr.Body.String(), "Sitemap: https://holyhymns.in/sitemap.xml") {
		t.Fatal("robots", rr.Body)
	}
	// Draft edits must not leak into either HTML or the sitemap.
	draft = song
	draft.Title = "UNPUBLISHED TITLE"
	edited := decodeTestSong(t, request(s, "PUT", "/v1/admin/songs/"+song.ID, token, draft))
	for _, p := range []string{path, "/sitemap.xml"} {
		if rr := request(s, "GET", p, "", nil); strings.Contains(rr.Body.String(), "UNPUBLISHED") {
			t.Fatal("draft leaked")
		}
	}
	if rr := request(s, "POST", "/v1/admin/songs/"+song.ID+"/unpublish", token, map[string]int{"version": edited.Version}); rr.Code != 200 {
		t.Fatal(rr.Body)
	}
	if rr := request(s, "GET", path, "", nil); rr.Code != 404 {
		t.Fatal("unpublished page remains available", rr.Code)
	}
	if rr := request(s, "GET", "/sitemap.xml", "", nil); strings.Contains(rr.Body.String(), song.ID) {
		t.Fatal("unpublished song in sitemap")
	}
}

func TestPublicWebsiteEscapesContent(t *testing.T) {
	s, db, token := integration(t)
	song := decodeTestSong(t, request(s, "POST", "/v1/admin/songs", token, Song{Title: `Hymn </script><script>alert(1)</script>`, LyricsManglish: `<img src=x onerror=alert(1)>`}))
	t.Cleanup(func() { db.Exec(context.Background(), `DELETE FROM songs WHERE id=$1`, song.ID) })
	if rr := request(s, "POST", "/v1/admin/songs/"+song.ID+"/publish", token, map[string]int{"version": song.Version}); rr.Code != 200 {
		t.Fatal(rr.Body)
	}
	rr := request(s, "GET", "/lyrics/"+song.ID, "", nil)
	if rr.Code != 308 {
		t.Fatalf("slug redirect: %d", rr.Code)
	}
	rr = request(s, "GET", rr.Header().Get("Location"), "", nil)
	if rr.Code != 200 || strings.Contains(rr.Body.String(), `<script>alert(1)</script>`) || strings.Contains(rr.Body.String(), `<img src=x`) {
		t.Fatal("unsafe page", rr.Body)
	}
	// Host headers never control canonical URLs.
	r := httptest.NewRequest("GET", "http://evil.example/", nil)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if strings.Contains(w.Body.String(), "evil.example") {
		t.Fatal("host poisoned metadata")
	}
}
