package server

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// The public canonical origin is fixed; never derive SEO URLs from an untrusted Host.
const websiteOrigin = "https://holyhymns.in"
const websitePageSize = 30

//go:embed website/*
var websiteFiles embed.FS

var websiteAssets = func() map[string]string {
	paths := map[string]string{}
	entries, err := websiteFiles.ReadDir("website")
	if err != nil {
		panic(err)
	}
	for _, entry := range entries {
		b, err := websiteFiles.ReadFile("website/" + entry.Name())
		if err != nil {
			panic(err)
		}
		paths[entry.Name()] = fmt.Sprintf("/site-assets/%s?v=%x", entry.Name(), sha256.Sum256(b))
	}
	return paths
}()

var websiteTemplate = template.Must(template.New("page.html").Funcs(template.FuncMap{
	"songPath":  songPath,
	"songTitle": songTitle,
	"excerpt":   lyricExcerpt,
	"asset":     websiteAsset,
	"number":    func(i int) string { return fmt.Sprintf("%02d", i+1) },
}).ParseFS(websiteFiles, "website/page.html"))

type websitePage struct {
	Title, Description, Canonical, Robots, Kind, Query string
	Songs                                              []Song
	Song                                               Song
	Total, Page                                        int
	Previous, Next                                     string
	Schema                                             template.JS
}

func websiteAsset(name string) string {
	path, ok := websiteAssets[name]
	if !ok {
		panic("unknown website asset: " + name)
	}
	return path
}

func (s *Server) registerWebsite(m *http.ServeMux) {
	m.HandleFunc("GET /{$}", s.websiteHome)
	m.HandleFunc("GET /lyrics", s.websiteCatalog)
	m.HandleFunc("GET /lyrics/{$}", func(w http.ResponseWriter, r *http.Request) {
		target := "/lyrics"
		if r.URL.RawQuery != "" {
			target += "?" + r.URL.RawQuery
		}
		http.Redirect(w, r, target, http.StatusPermanentRedirect)
	})
	m.HandleFunc("GET /lyrics/{slug}", s.websiteSong)
	m.HandleFunc("GET /sitemap.xml", s.websiteSitemap)
	m.HandleFunc("GET /robots.txt", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprint(w, "User-agent: *\nAllow: /\nDisallow: /v1/\nDisallow: /auth/\n\nSitemap: "+websiteOrigin+"/sitemap.xml\n")
	})
	assets, _ := fs.Sub(websiteFiles, "website")
	files := http.StripPrefix("/site-assets/", http.FileServer(http.FS(assets)))
	m.HandleFunc("GET /site-assets/{file}", func(w http.ResponseWriter, r *http.Request) {
		switch r.PathValue("file") {
		case "styles.css", "reader.js", "hero.webp", "share.jpg", "favicon.svg", "malayalam.ttf", "NotoSansMalayalam-OFL.txt":
			w.Header().Set("Cache-Control", "public, max-age=86400")
			if r.URL.RequestURI() == websiteAsset(r.PathValue("file")) {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			}
			files.ServeHTTP(w, r)
		default:
			s.websiteNotFound(w, r)
		}
	})
	m.HandleFunc("GET /", s.websiteNotFound)
}

func songTitle(song Song) string {
	if song.Title != "" {
		return song.Title
	}
	return song.TitleMalayalam
}

func songPath(song Song) string {
	var slug strings.Builder
	for _, c := range strings.ToLower(song.Title) {
		if c >= 'a' && c <= 'z' || c >= '0' && c <= '9' {
			slug.WriteRune(c)
		} else if slug.Len() > 0 && !strings.HasSuffix(slug.String(), "-") {
			slug.WriteByte('-')
		}
		if slug.Len() >= 70 {
			break
		}
	}
	name := strings.Trim(slug.String(), "-")
	if name == "" {
		name = "hymn"
	}
	return "/lyrics/" + name + "-" + song.ID
}

func lyricExcerpt(song Song) string {
	text := song.LyricsManglish
	if text == "" {
		text = song.LyricsMalayalam
	}
	runes := []rune(strings.Join(strings.Fields(text), " "))
	if len(runes) > 110 {
		return string(runes[:107]) + "…"
	}
	return string(runes)
}

func (s *Server) websiteSongs(r *http.Request, query string, limit, offset int) ([]Song, int, error) {
	const filter = `published IS NOT NULL AND ($1='' OR search_document @@ plainto_tsquery('simple',$1) OR search_title % $1 OR strpos(search_text,$1)>0)`
	var total int
	if err := s.DB.QueryRow(r.Context(), `SELECT count(*) FROM songs WHERE `+filter, query).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.DB.Query(r.Context(), `SELECT id::text,published,published_version,published_at FROM songs WHERE `+filter+`
	 ORDER BY CASE WHEN $1<>'' AND lower(published->>'title')=$1 THEN 0 ELSE 1 END,
	 CASE WHEN $1<>'' THEN ts_rank(search_document,plainto_tsquery('simple',$1)) + similarity(search_title,$1) ELSE 0 END DESC,
	 lower(published->>'title'),id LIMIT $2 OFFSET $3`, query, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	songs := []Song{}
	for rows.Next() {
		var sid string
		var raw []byte
		var version int
		var updated time.Time
		if err := rows.Scan(&sid, &raw, &version, &updated); err != nil {
			return nil, 0, err
		}
		song, err := decodeSong(raw, sid, version, "published", updated.Format(time.RFC3339))
		if err != nil {
			return nil, 0, err
		}
		songs = append(songs, song)
	}
	return songs, total, rows.Err()
}

func (s *Server) websiteHome(w http.ResponseWriter, r *http.Request) {
	songs, total, err := s.websiteSongs(r, "", 6, 0)
	if err != nil {
		s.websiteError(w, r, err)
		return
	}
	p := websitePage{Kind: "home", Title: "Holy Hymns — Malayalam Christian Song Lyrics & Manglish", Description: "Find Malayalam Christian song lyrics in Malayalam and Manglish. Explore Holy Hymns, a quiet space for familiar hymns, personal prayer and worship.", Canonical: websiteOrigin + "/", Songs: songs, Total: total}
	p.Schema = schemaJSON(map[string]any{"@context": "https://schema.org", "@type": "WebSite", "@id": websiteOrigin + "/#website", "name": "Holy Hymns", "url": p.Canonical, "description": p.Description, "inLanguage": []string{"en", "ml"}})
	renderWebsite(w, p, 200)
}

func catalogPath(page int, query string) string {
	v := url.Values{}
	if page > 1 {
		v.Set("page", strconv.Itoa(page))
	}
	if query != "" {
		v.Set("q", query)
	}
	if len(v) == 0 {
		return "/lyrics"
	}
	return "/lyrics?" + v.Encode()
}

func (s *Server) websiteCatalog(w http.ResponseWriter, r *http.Request) {
	n := 1
	if raw := r.URL.Query().Get("page"); raw != "" {
		var err error
		n, err = strconv.Atoi(raw)
		if err != nil || n < 1 || n > 3334 {
			s.websiteNotFound(w, r)
			return
		}
	}
	query := clampQuery(r)
	songs, total, err := s.websiteSongs(r, query, websitePageSize, (n-1)*websitePageSize)
	if err != nil {
		s.websiteError(w, r, err)
		return
	}
	if n > 1 && len(songs) == 0 {
		s.websiteNotFound(w, r)
		return
	}
	p := websitePage{Kind: "catalog", Title: "Malayalam Christian Song Lyrics — Holy Hymns", Description: "Browse Malayalam Christian hymns and read complete lyrics in Malayalam and Manglish. Search by song title or a line you remember.", Canonical: websiteOrigin + catalogPath(n, ""), Songs: songs, Total: total, Page: n, Query: query}
	if n > 1 {
		p.Title = fmt.Sprintf("Malayalam Christian Song Lyrics — Page %d | Holy Hymns", n)
		p.Previous = catalogPath(n-1, query)
	}
	if n*websitePageSize < total {
		p.Next = catalogPath(n+1, query)
	}
	if query != "" {
		p.Robots = "noindex, follow"
		p.Title = "Search hymn lyrics — Holy Hymns"
		p.Canonical = websiteOrigin + "/lyrics"
	}
	items := []map[string]any{}
	for i, song := range songs {
		items = append(items, map[string]any{"@type": "ListItem", "position": (n-1)*websitePageSize + i + 1, "name": songTitle(song), "url": websiteOrigin + songPath(song)})
	}
	p.Schema = schemaJSON(map[string]any{"@context": "https://schema.org", "@type": "CollectionPage", "name": p.Title, "url": p.Canonical, "mainEntity": map[string]any{"@type": "ItemList", "itemListElement": items}})
	renderWebsite(w, p, 200)
}

func (s *Server) websiteSong(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	if len(slug) < 36 {
		s.websiteNotFound(w, r)
		return
	}
	sid := slug[len(slug)-36:]
	if !validID(sid) {
		s.websiteNotFound(w, r)
		return
	}
	var raw []byte
	var version int
	var updated time.Time
	err := s.DB.QueryRow(r.Context(), `SELECT published,published_version,published_at FROM songs WHERE id=$1 AND published IS NOT NULL`, sid).Scan(&raw, &version, &updated)
	if errors.Is(err, pgx.ErrNoRows) {
		s.websiteNotFound(w, r)
		return
	}
	if err != nil {
		s.websiteError(w, r, err)
		return
	}
	song, err := decodeSong(raw, strings.ToLower(sid), version, "published", updated.Format(time.RFC3339))
	if err != nil {
		s.websiteError(w, r, err)
		return
	}
	if r.URL.Path != songPath(song) {
		http.Redirect(w, r, songPath(song), http.StatusPermanentRedirect)
		return
	}
	p := websitePage{Kind: "song", Title: songTitle(song) + " Lyrics — Malayalam & Manglish | Holy Hymns", Description: songTitle(song) + " lyrics. " + lyricExcerpt(song), Canonical: websiteOrigin + songPath(song), Song: song}
	lyrics := []map[string]any{}
	if song.LyricsMalayalam != "" {
		lyrics = append(lyrics, map[string]any{"@type": "CreativeWork", "inLanguage": "ml", "text": song.LyricsMalayalam})
	}
	if song.LyricsManglish != "" {
		lyrics = append(lyrics, map[string]any{"@type": "CreativeWork", "inLanguage": "ml-Latn", "text": song.LyricsManglish})
	}
	p.Schema = schemaJSON(map[string]any{"@context": "https://schema.org", "@graph": []any{
		map[string]any{"@type": "WebPage", "@id": p.Canonical, "url": p.Canonical, "name": p.Title, "description": p.Description, "dateModified": song.UpdatedAt, "mainEntity": map[string]any{"@type": "MusicComposition", "name": songTitle(song), "alternateName": song.TitleMalayalam, "lyrics": lyrics}},
		map[string]any{"@type": "BreadcrumbList", "itemListElement": []any{map[string]any{"@type": "ListItem", "position": 1, "name": "Holy Hymns", "item": websiteOrigin + "/"}, map[string]any{"@type": "ListItem", "position": 2, "name": "Lyrics", "item": websiteOrigin + "/lyrics"}, map[string]any{"@type": "ListItem", "position": 3, "name": songTitle(song), "item": p.Canonical}}},
	}})
	renderWebsite(w, p, 200)
}

// json.Marshal escapes HTML delimiters before this trusted JSON reaches the script element.
func schemaJSON(value any) template.JS { b, _ := json.Marshal(value); return template.JS(b) }

func renderWebsite(w http.ResponseWriter, p websitePage, status int) {
	if p.Robots == "" {
		p.Robots = "index, follow, max-image-preview:large"
	}
	var b bytes.Buffer
	if err := websiteTemplate.Execute(&b, p); err != nil {
		slog.Error("render website", "error", err)
		fail(w, 500, "page unavailable")
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("X-Robots-Tag", p.Robots)
	// Always read the current publication snapshot, including after an unpublish.
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(status)
	w.Write(b.Bytes())
}

func (s *Server) websiteNotFound(w http.ResponseWriter, r *http.Request) {
	renderWebsite(w, websitePage{Kind: "notfound", Title: "Hymn not found — Holy Hymns", Description: "This page is unavailable. Browse the Holy Hymns lyric collection.", Robots: "noindex, follow"}, 404)
}

func (s *Server) websiteError(w http.ResponseWriter, r *http.Request, err error) {
	slog.Error("load public website", "error", err)
	w.Header().Set("Retry-After", "30")
	renderWebsite(w, websitePage{Kind: "error", Title: "Please try again — Holy Hymns", Description: "The hymn library is temporarily unavailable.", Robots: "noindex, follow"}, 503)
}

type sitemapURL struct {
	Loc     string `xml:"loc"`
	LastMod string `xml:"lastmod,omitempty"`
}
type sitemapDocument struct {
	XMLName xml.Name     `xml:"urlset"`
	XMLNS   string       `xml:"xmlns,attr"`
	URLs    []sitemapURL `xml:"url"`
}

func (s *Server) websiteSitemap(w http.ResponseWriter, r *http.Request) {
	// Select only the fields needed for URLs: never load the complete lyrics library.
	rows, err := s.DB.Query(r.Context(), `SELECT id::text,COALESCE(published->>'title',''),published_at FROM songs WHERE published IS NOT NULL ORDER BY id LIMIT 48001`)
	if err != nil {
		s.websiteError(w, r, err)
		return
	}
	defer rows.Close()
	doc := sitemapDocument{XMLNS: "http://www.sitemaps.org/schemas/sitemap/0.9", URLs: []sitemapURL{{Loc: websiteOrigin + "/"}, {Loc: websiteOrigin + "/lyrics"}}}
	count := 0
	for rows.Next() {
		var song Song
		var updated time.Time
		if err := rows.Scan(&song.ID, &song.Title, &updated); err != nil {
			s.websiteError(w, r, err)
			return
		}
		doc.URLs = append(doc.URLs, sitemapURL{Loc: websiteOrigin + songPath(song), LastMod: updated.Format(time.RFC3339)})
		count++
	}
	if err := rows.Err(); err != nil {
		s.websiteError(w, r, err)
		return
	}
	if count > 48000 {
		s.websiteError(w, r, errors.New("sitemap requires partitioning"))
		return
	}
	for n := 2; (n-1)*websitePageSize < count; n++ {
		doc.URLs = append(doc.URLs, sitemapURL{Loc: websiteOrigin + catalogPath(n, "")})
	}
	b, err := xml.Marshal(doc)
	if err != nil {
		s.websiteError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	fmt.Fprint(w, xml.Header)
	w.Write(b)
}
