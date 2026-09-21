package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

func (s *Server) listAdminSongs(w http.ResponseWriter, r *http.Request) {
	limit, offset := page(r)
	q := clampQuery(r)
	var total int
	if e := s.DB.QueryRow(r.Context(), `SELECT count(*) FROM songs WHERE $1='' OR strpos(lower(draft->>'title'),$1)>0 OR strpos(draft->>'titleMalayalam',$1)>0`, q).Scan(&total); e != nil {
		dbError(w, e)
		return
	}
	rows, err := s.DB.Query(r.Context(), `SELECT id::text,draft,version,published IS NOT NULL,updated_at FROM songs WHERE $1='' OR strpos(lower(draft->>'title'),$1)>0 OR strpos(draft->>'titleMalayalam',$1)>0 ORDER BY updated_at DESC,id LIMIT $2 OFFSET $3`, q, limit, offset)
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
		var published bool
		var updated time.Time
		if e := rows.Scan(&sid, &raw, &version, &published, &updated); e != nil {
			dbError(w, e)
			return
		}
		status := "draft"
		if published {
			status = "published"
		}
		v, e := decodeSong(raw, sid, version, status, updated.Format(time.RFC3339))
		if e != nil {
			dbError(w, e)
			return
		}
		items = append(items, v)
	}
	if rows.Err() != nil {
		dbError(w, rows.Err())
		return
	}
	write(w, 200, map[string]any{"items": items, "total": total})
}
func (s *Server) getAdminSong(w http.ResponseWriter, r *http.Request) {
	sid := id(w, r)
	if sid == "" {
		return
	}
	var raw []byte
	var version int
	var published bool
	var updated time.Time
	if e := s.DB.QueryRow(r.Context(), `SELECT draft,version,published IS NOT NULL,updated_at FROM songs WHERE id=$1`, sid).Scan(&raw, &version, &published, &updated); e != nil {
		dbError(w, e)
		return
	}
	status := "draft"
	if published {
		status = "published"
	}
	v, e := decodeSong(raw, sid, version, status, updated.Format(time.RFC3339))
	if e != nil {
		dbError(w, e)
		return
	}
	write(w, 200, v)
}
func (s *Server) saveSong(w http.ResponseWriter, r *http.Request) {
	var song Song
	if !read(w, r, &song) {
		return
	}
	reviewNotesOmitted := song.ReviewNotes == nil
	song.clean()
	if e := song.validate(false); e != nil {
		fail(w, 400, e.Error())
		return
	}
	tx, e := beginContent(r.Context(), s.DB)
	if e != nil {
		dbError(w, e)
		return
	}
	defer tx.Rollback(r.Context())
	if !checkCategories(w, r, tx, song.CategoryIDs) {
		return
	}
	sid := r.PathValue("id")
	version := 1
	published := false
	if sid != "" {
		if !validID(sid) {
			fail(w, 400, "invalid identifier")
			return
		}
		var current int
		var existingJSON []byte
		if e = tx.QueryRow(r.Context(), `SELECT version,published IS NOT NULL,draft FROM songs WHERE id=$1 FOR UPDATE`, sid).Scan(&current, &published, &existingJSON); e != nil {
			dbError(w, e)
			return
		}
		var existing Song
		if e = json.Unmarshal(existingJSON, &existing); e != nil {
			dbError(w, e)
			return
		}
		if existing.SourceURL != "" {
			song.SourceURL = existing.SourceURL
		}
		if reviewNotesOmitted {
			song.ReviewNotes = existing.ReviewNotes
		}
		if current != song.Version {
			fail(w, 409, "song changed; reload before saving")
			return
		}
		version = current + 1
	}
	song.Version = version
	song.Status = "draft"
	song.ID = sid
	song.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	raw, _ := json.Marshal(song)
	if sid == "" {
		e = tx.QueryRow(r.Context(), `INSERT INTO songs(draft) VALUES($1) RETURNING id::text`, raw).Scan(&sid)
	} else {
		_, e = tx.Exec(r.Context(), `UPDATE songs SET draft=$2,version=$3,updated_at=now() WHERE id=$1`, sid, raw, version)
	}
	if e != nil {
		dbError(w, e)
		return
	}
	if _, e = tx.Exec(r.Context(), `INSERT INTO song_revisions(song_id,version,content,actor_id) VALUES($1,$2,$3,$4)`, sid, version, raw, actor(r)); e != nil {
		dbError(w, e)
		return
	}
	if e = audit(r.Context(), tx, r, "song.save", sid); e != nil {
		dbError(w, e)
		return
	}
	if e = tx.Commit(r.Context()); e != nil {
		dbError(w, e)
		return
	}
	song.ID = sid
	if published {
		song.Status = "published"
	}
	write(w, 200, song)
}
func checkCategories(w http.ResponseWriter, r *http.Request, tx pgx.Tx, ids []string) bool {
	for _, id := range ids {
		var exists bool
		if e := tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM categories WHERE id=$1)`, id).Scan(&exists); e != nil {
			dbError(w, e)
			return false
		}
		if !exists {
			fail(w, 400, "a category no longer exists")
			return false
		}
	}
	return true
}
func (s *Server) publish(w http.ResponseWriter, r *http.Request)   { s.setPublication(w, r, true) }
func (s *Server) unpublish(w http.ResponseWriter, r *http.Request) { s.setPublication(w, r, false) }
func (s *Server) setPublication(w http.ResponseWriter, r *http.Request, publish bool) {
	sid := id(w, r)
	if sid == "" {
		return
	}
	var body struct {
		Version int `json:"version"`
	}
	if !read(w, r, &body) {
		return
	}
	tx, e := beginContent(r.Context(), s.DB)
	if e != nil {
		dbError(w, e)
		return
	}
	defer tx.Rollback(r.Context())
	var raw []byte
	var version int
	if e = tx.QueryRow(r.Context(), `SELECT draft,version FROM songs WHERE id=$1 FOR UPDATE`, sid).Scan(&raw, &version); e != nil {
		dbError(w, e)
		return
	}
	if version != body.Version {
		fail(w, 409, "song changed; reload before publishing")
		return
	}
	var song Song
	if e = json.Unmarshal(raw, &song); e != nil {
		dbError(w, e)
		return
	}
	song.clean()
	kind := "unpublish"
	if publish {
		if e = song.validate(true); e != nil {
			fail(w, 400, e.Error())
			return
		}
		if !checkCategories(w, r, tx, song.CategoryIDs) {
			return
		}
		kind = "publish"
		title := normalize(song.Title + " " + song.TitleMalayalam + " " + strings.Join(song.Aliases, " "))
		text := normalize(title + " " + song.LyricsMalayalam + " " + song.LyricsManglish)
		public := song
		public.ReviewNotes = nil
		publicJSON, _ := json.Marshal(public)
		// Explicit publication approves the import. Historical revisions retain its review notes.
		_, e = tx.Exec(r.Context(), `UPDATE songs SET draft=jsonb_set(draft,'{reviewNotes}','[]'::jsonb),published=$4,published_version=version,search_title=$2,search_text=$3,search_document=setweight(to_tsvector('simple',$2),'A') || setweight(to_tsvector('simple',$3),'B'),published_at=now(),updated_at=now() WHERE id=$1`, sid, title, text, publicJSON)
	} else {
		_, e = tx.Exec(r.Context(), `UPDATE songs SET published=NULL,published_version=NULL,search_title='',search_text='',search_document=''::tsvector,updated_at=now() WHERE id=$1`, sid)
	}
	if e != nil {
		dbError(w, e)
		return
	}
	if e = change(r.Context(), tx, kind, sid); e != nil {
		dbError(w, e)
		return
	}
	if e = audit(r.Context(), tx, r, "song."+kind, sid); e != nil {
		dbError(w, e)
		return
	}
	if e = tx.Commit(r.Context()); e != nil {
		dbError(w, e)
		return
	}
	write(w, 200, map[string]string{"status": kind})
}
func (s *Server) revisions(w http.ResponseWriter, r *http.Request) {
	sid := id(w, r)
	if sid == "" {
		return
	}
	rows, e := s.DB.Query(r.Context(), `SELECT id::text,version,created_at,content FROM song_revisions WHERE song_id=$1 ORDER BY version DESC LIMIT 100`, sid)
	if e != nil {
		dbError(w, e)
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var rid string
		var version int
		var at time.Time
		var raw json.RawMessage
		if e = rows.Scan(&rid, &version, &at, &raw); e != nil {
			dbError(w, e)
			return
		}
		items = append(items, map[string]any{"id": rid, "version": version, "createdAt": at, "content": raw})
	}
	if rows.Err() != nil {
		dbError(w, rows.Err())
		return
	}
	write(w, 200, map[string]any{"items": items})
}
func (s *Server) restore(w http.ResponseWriter, r *http.Request) {
	sid := id(w, r)
	if sid == "" {
		return
	}
	var body struct {
		RevisionID string `json:"revisionId"`
		Version    int    `json:"version"`
	}
	if !read(w, r, &body) {
		return
	}
	if !validID(body.RevisionID) {
		fail(w, 400, "invalid revision")
		return
	}
	tx, e := beginContent(r.Context(), s.DB)
	if e != nil {
		dbError(w, e)
		return
	}
	defer tx.Rollback(r.Context())
	var current int
	var published bool
	if e = tx.QueryRow(r.Context(), `SELECT version,published IS NOT NULL FROM songs WHERE id=$1 FOR UPDATE`, sid).Scan(&current, &published); e != nil {
		dbError(w, e)
		return
	}
	if current != body.Version {
		fail(w, 409, "song changed; reload before restoring")
		return
	}
	var raw []byte
	if e = tx.QueryRow(r.Context(), `SELECT content FROM song_revisions WHERE id=$1 AND song_id=$2`, body.RevisionID, sid).Scan(&raw); e != nil {
		dbError(w, e)
		return
	}
	var song Song
	if e = json.Unmarshal(raw, &song); e != nil {
		dbError(w, e)
		return
	}
	song.clean()
	// Deleted taxonomy must not strand restored drafts with invisible invalid IDs.
	validCategories := []string{}
	for _, categoryID := range song.CategoryIDs {
		var exists bool
		if e = tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM categories WHERE id=$1)`, categoryID).Scan(&exists); e != nil {
			dbError(w, e)
			return
		}
		if exists {
			validCategories = append(validCategories, categoryID)
		}
	}
	song.CategoryIDs = validCategories
	song.Version = current + 1
	song.ID = sid
	song.Status = "draft"
	song.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	raw, _ = json.Marshal(song)
	if _, e = tx.Exec(r.Context(), `UPDATE songs SET draft=$2,version=$3,updated_at=now() WHERE id=$1`, sid, raw, song.Version); e != nil {
		dbError(w, e)
		return
	}
	if _, e = tx.Exec(r.Context(), `INSERT INTO song_revisions(song_id,version,content,actor_id) VALUES($1,$2,$3,$4)`, sid, song.Version, raw, actor(r)); e != nil {
		dbError(w, e)
		return
	}
	if e = audit(r.Context(), tx, r, "song.restore", sid); e != nil {
		dbError(w, e)
		return
	}
	if e = tx.Commit(r.Context()); e != nil {
		dbError(w, e)
		return
	}
	if published {
		song.Status = "published"
	}
	write(w, 200, song)
}
