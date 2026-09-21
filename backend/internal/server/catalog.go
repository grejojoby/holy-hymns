package server

import (
	"encoding/json"
	"net/http"
	"time"

	"holyhymns/internal/identity"
)

func (s *Server) listSongs(w http.ResponseWriter, r *http.Request) {
	limit, offset := page(r)
	q := clampQuery(r)
	category := r.URL.Query().Get("category")
	if category != "" && !validID(category) {
		fail(w, 400, "invalid category")
		return
	}
	featured := r.URL.Query().Get("featured") == "true"
	letter := normalize(r.URL.Query().Get("letter"))
	recent := r.URL.Query().Get("sort") == "recent"
	const filter = `published IS NOT NULL AND ($1='' OR search_document @@ plainto_tsquery('simple',$1) OR search_title % $1 OR strpos(search_text,$1)>0)
 AND ($2='' OR (published->'categoryIds') ? $2) AND (NOT $3 OR (published->>'featured')::boolean=true)
 AND ($4='' OR starts_with(lower(published->>'title'),$4) OR starts_with(published->>'titleMalayalam',$4))`
	var total int
	if err := s.DB.QueryRow(r.Context(), `SELECT count(*) FROM songs WHERE `+filter, q, category, featured, letter).Scan(&total); err != nil {
		dbError(w, err)
		return
	}
	rows, err := s.DB.Query(r.Context(), `SELECT id::text,published,published_version,published_at FROM songs WHERE `+filter+`
 ORDER BY CASE WHEN $1<>'' AND (lower(published->>'title')=$1 OR published->>'titleMalayalam'=$1) THEN 0
 WHEN $1<>'' AND strpos(search_title,$1)>0 THEN 1
 WHEN $1<>'' AND strpos(search_text,$1)>0 THEN 2
 WHEN $1<>'' AND search_document @@ plainto_tsquery('simple',$1) THEN 3 ELSE 4 END,
 CASE WHEN $1<>'' THEN ts_rank(search_document,plainto_tsquery('simple',$1)) + similarity(search_title,$1) ELSE 0 END DESC,
 CASE WHEN $5 THEN published_at END DESC,lower(published->>'title'),id LIMIT $6 OFFSET $7`, q, category, featured, letter, recent, limit, offset)
	if err != nil {
		dbError(w, err)
		return
	}
	defer rows.Close()
	items := []Song{}
	for rows.Next() {
		var sid string
		var raw []byte
		var version int
		var updated time.Time
		if err = rows.Scan(&sid, &raw, &version, &updated); err != nil {
			dbError(w, err)
			return
		}
		song, e := decodeSong(raw, sid, version, "published", updated.Format(time.RFC3339))
		if e != nil {
			dbError(w, e)
			return
		}
		items = append(items, song)
	}
	if rows.Err() != nil {
		dbError(w, rows.Err())
		return
	}
	write(w, 200, map[string]any{"items": items, "total": total})
}
func (s *Server) getSong(w http.ResponseWriter, r *http.Request) {
	sid := id(w, r)
	if sid == "" {
		return
	}
	var raw []byte
	var version int
	var updated time.Time
	if err := s.DB.QueryRow(r.Context(), `SELECT published,published_version,published_at FROM songs WHERE id=$1 AND published IS NOT NULL`, sid).Scan(&raw, &version, &updated); err != nil {
		dbError(w, err)
		return
	}
	song, e := decodeSong(raw, sid, version, "published", updated.Format(time.RFC3339))
	if e != nil {
		dbError(w, e)
		return
	}
	write(w, 200, song)
}
func (s *Server) categories(w http.ResponseWriter, r *http.Request) {
	u := identity.Current(r)
	staff := u != nil && (u.Role == "admin" || u.Role == "owner")
	rows, err := s.DB.Query(r.Context(), `SELECT id::text,name,name_malayalam,kind,position,version FROM categories c WHERE $1 OR EXISTS(SELECT 1 FROM songs s WHERE s.published IS NOT NULL AND (s.published->'categoryIds') ? c.id::text) ORDER BY position,name`, staff)
	if err != nil {
		dbError(w, err)
		return
	}
	defer rows.Close()
	items := []Category{}
	for rows.Next() {
		var c Category
		if err = rows.Scan(&c.ID, &c.Name, &c.NameMalayalam, &c.Kind, &c.Position, &c.Version); err != nil {
			dbError(w, err)
			return
		}
		items = append(items, c)
	}
	if rows.Err() != nil {
		dbError(w, rows.Err())
		return
	}
	write(w, 200, map[string]any{"items": items})
}
func (s *Server) config(w http.ResponseWriter, r *http.Request) {
	var raw []byte
	var revision int64
	var version int
	if err := s.DB.QueryRow(r.Context(), `SELECT content,revision,version FROM app_config CROSS JOIN content_state`).Scan(&raw, &revision, &version); err != nil {
		dbError(w, err)
		return
	}
	var c AppConfig
	if err := json.Unmarshal(raw, &c); err != nil {
		dbError(w, err)
		return
	}
	c.Revision = revision
	c.Version = version
	write(w, 200, c)
}
func (s *Server) favorites(w http.ResponseWriter, r *http.Request) {
	rows, err := s.DB.Query(r.Context(), `SELECT f.song_id::text FROM favorites f JOIN songs s ON s.id=f.song_id WHERE f.user_id=$1 AND s.published IS NOT NULL ORDER BY f.created_at DESC`, actor(r))
	if err != nil {
		dbError(w, err)
		return
	}
	defer rows.Close()
	items := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			dbError(w, err)
			return
		}
		items = append(items, id)
	}
	if rows.Err() != nil {
		dbError(w, rows.Err())
		return
	}
	write(w, 200, map[string]any{"items": items})
}
func (s *Server) favorite(w http.ResponseWriter, r *http.Request) {
	sid := id(w, r)
	if sid == "" {
		return
	}
	if r.Method == "DELETE" {
		if _, err := s.DB.Exec(r.Context(), `DELETE FROM favorites WHERE user_id=$1 AND song_id=$2`, actor(r), sid); err != nil {
			dbError(w, err)
			return
		}
	} else {
		var exists bool
		if err := s.DB.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM songs WHERE id=$1 AND published IS NOT NULL)`, sid).Scan(&exists); err != nil {
			dbError(w, err)
			return
		}
		if !exists {
			fail(w, 404, "song unavailable")
			return
		}
		if _, err := s.DB.Exec(r.Context(), `INSERT INTO favorites(user_id,song_id) VALUES($1,$2) ON CONFLICT DO NOTHING`, actor(r), sid); err != nil {
			dbError(w, err)
			return
		}
	}
	w.WriteHeader(204)
}
