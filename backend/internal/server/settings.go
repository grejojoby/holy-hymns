package server

import (
	"encoding/json"
	"net/http"
	"net/mail"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func (s *Server) saveCategory(w http.ResponseWriter, r *http.Request) {
	var c Category
	if !read(w, r, &c) {
		return
	}
	c.Name = strings.TrimSpace(c.Name)
	if c.Name == "" || len(c.Name) > 300 || len(c.NameMalayalam) > 600 || (c.Kind != "purpose" && c.Kind != "occasion" && c.Kind != "theme") {
		fail(w, 400, "category requires a name and valid kind")
		return
	}
	tx, e := beginContent(r.Context(), s.DB)
	if e != nil {
		dbError(w, e)
		return
	}
	defer tx.Rollback(r.Context())
	cid := r.PathValue("id")
	if cid == "" {
		c.Version = 1
		e = tx.QueryRow(r.Context(), `INSERT INTO categories(name,name_malayalam,kind,position) VALUES($1,$2,$3,$4) RETURNING id::text`, c.Name, c.NameMalayalam, c.Kind, c.Position).Scan(&cid)
	} else {
		if !validID(cid) {
			fail(w, 400, "invalid category")
			return
		}
		tag, err := tx.Exec(r.Context(), `UPDATE categories SET name=$2,name_malayalam=$3,kind=$4,position=$5,version=version+1 WHERE id=$1 AND version=$6`, cid, c.Name, c.NameMalayalam, c.Kind, c.Position, c.Version)
		e = err
		if e == nil && tag.RowsAffected() == 0 {
			fail(w, 409, "category changed or removed; reload before saving")
			return
		}
		c.Version++
	}
	if e != nil {
		if pgErr, ok := e.(*pgconn.PgError); ok && pgErr.Code == "23505" {
			fail(w, 409, "a category with this name and kind already exists")
			return
		}
		dbError(w, e)
		return
	}
	if e = change(r.Context(), tx, "category", ""); e != nil {
		dbError(w, e)
		return
	}
	if e = audit(r.Context(), tx, r, "category.save", cid); e != nil {
		dbError(w, e)
		return
	}
	if e = tx.Commit(r.Context()); e != nil {
		dbError(w, e)
		return
	}
	c.ID = cid
	write(w, 200, c)
}
func (s *Server) deleteCategory(w http.ResponseWriter, r *http.Request) {
	cid := id(w, r)
	if cid == "" {
		return
	}
	tx, e := beginContent(r.Context(), s.DB)
	if e != nil {
		dbError(w, e)
		return
	}
	defer tx.Rollback(r.Context())
	version, _ := strconv.Atoi(r.URL.Query().Get("version"))
	var current int
	if e = tx.QueryRow(r.Context(), `SELECT version FROM categories WHERE id=$1 FOR UPDATE`, cid).Scan(&current); e != nil {
		if e == pgx.ErrNoRows {
			fail(w, 404, "category not found")
		} else {
			dbError(w, e)
		}
		return
	}
	if version != current {
		fail(w, 409, "category changed; reload before deleting")
		return
	}
	// Keep historical revisions intact. Active drafts/public snapshots lose the category, and drafts' version guards advance.
	_, e = tx.Exec(r.Context(), `UPDATE songs SET draft=jsonb_set(draft,'{categoryIds}',COALESCE(draft->'categoryIds','[]'::jsonb)-$1),published=CASE WHEN published IS NULL THEN NULL ELSE jsonb_set(published,'{categoryIds}',COALESCE(published->'categoryIds','[]'::jsonb)-$1) END,version=version+1,updated_at=now() WHERE (draft->'categoryIds') ? $1 OR (published->'categoryIds') ? $1`, cid)
	if e != nil {
		dbError(w, e)
		return
	}
	if _, e = tx.Exec(r.Context(), `DELETE FROM categories WHERE id=$1`, cid); e != nil {
		dbError(w, e)
		return
	}
	if e = change(r.Context(), tx, "category", ""); e != nil {
		dbError(w, e)
		return
	}
	if e = audit(r.Context(), tx, r, "category.delete", cid); e != nil {
		dbError(w, e)
		return
	}
	if e = tx.Commit(r.Context()); e != nil {
		dbError(w, e)
		return
	}
	w.WriteHeader(204)
}
func (s *Server) saveConfig(w http.ResponseWriter, r *http.Request) {
	var c AppConfig
	if !read(w, r, &c) {
		return
	}
	c.AppName = strings.TrimSpace(c.AppName)
	if c.AppName == "" || len(c.AppName) > 100 || len(c.Announcement) > 2000 || len(c.AboutText) > 10000 {
		fail(w, 400, "invalid app settings")
		return
	}
	if c.SupportEmail != "" {
		if _, e := mail.ParseAddress(c.SupportEmail); e != nil {
			fail(w, 400, "invalid support email")
			return
		}
	}
	c.Revision = 0
	raw, _ := json.Marshal(c)
	tx, e := beginContent(r.Context(), s.DB)
	if e != nil {
		dbError(w, e)
		return
	}
	defer tx.Rollback(r.Context())
	tag, e := tx.Exec(r.Context(), `UPDATE app_config SET content=$1,version=version+1 WHERE version=$2`, raw, c.Version)
	if e != nil {
		dbError(w, e)
		return
	}
	if tag.RowsAffected() == 0 {
		fail(w, 409, "settings changed; reload before saving")
		return
	}
	if e = change(r.Context(), tx, "config", ""); e != nil {
		dbError(w, e)
		return
	}
	if e = audit(r.Context(), tx, r, "config.save", "app"); e != nil {
		dbError(w, e)
		return
	}
	if e = tx.Commit(r.Context()); e != nil {
		dbError(w, e)
		return
	}
	s.config(w, r)
}
