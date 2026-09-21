package server

import (
	"net/http"
	"strconv"

	"holyhymns/internal/identity"
)

func (s *Server) track(w http.ResponseWriter, r *http.Request) {
	if u := identity.Current(r); u != nil && (u.Role == "admin" || u.Role == "owner") {
		w.WriteHeader(204)
		return
	}
	if !s.allow(r, "analytics", 120) {
		fail(w, 429, "analytics rate limit reached")
		return
	}
	var v struct {
		Event       string `json:"event"`
		SongID      string `json:"songId"`
		CategoryID  string `json:"categoryId"`
		ResultCount *int   `json:"resultCount"`
	}
	if !read(w, r, &v) {
		return
	}
	switch v.Event {
	case "song_open", "favorite":
		if !validID(v.SongID) {
			fail(w, 400, "song identifier required")
			return
		}
		var ok bool
		if e := s.DB.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM songs WHERE id=$1 AND published IS NOT NULL)`, v.SongID).Scan(&ok); e != nil {
			dbError(w, e)
			return
		}
		if !ok {
			fail(w, 404, "song unavailable")
			return
		}
		v.CategoryID = ""
	case "category_open":
		if !validID(v.CategoryID) {
			fail(w, 400, "category identifier required")
			return
		}
		var ok bool
		if e := s.DB.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM categories WHERE id=$1)`, v.CategoryID).Scan(&ok); e != nil {
			dbError(w, e)
			return
		}
		if !ok {
			fail(w, 404, "category unavailable")
			return
		}
		v.SongID = ""
	case "search":
		v.SongID = ""
		v.CategoryID = ""
		if v.ResultCount == nil || *v.ResultCount < 0 {
			fail(w, 400, "result count required")
			return
		}
	default:
		fail(w, 400, "unknown analytics event")
		return
	}
	tx, e := s.DB.Begin(r.Context())
	if e != nil {
		dbError(w, e)
		return
	}
	defer tx.Rollback(r.Context())
	const insert = `INSERT INTO analytics_daily(event,song_id,category_id,count) VALUES($1,$2,$3,1) ON CONFLICT(day,event,song_id,category_id) DO UPDATE SET count=analytics_daily.count+1`
	if _, e = tx.Exec(r.Context(), insert, v.Event, v.SongID, v.CategoryID); e != nil {
		dbError(w, e)
		return
	}
	if v.Event == "search" && *v.ResultCount == 0 {
		if _, e = tx.Exec(r.Context(), insert, "no_results", "", ""); e != nil {
			dbError(w, e)
			return
		}
	}
	if e = tx.Commit(r.Context()); e != nil {
		dbError(w, e)
		return
	}
	w.WriteHeader(204)
}
func (s *Server) analytics(w http.ResponseWriter, r *http.Request) {
	days, _ := strconv.Atoi(r.URL.Query().Get("days"))
	if days < 1 {
		days = 30
	}
	if days > 90 {
		days = 90
	}
	rows, e := s.DB.Query(r.Context(), `SELECT day::text,event,sum(count) FROM analytics_daily WHERE day>=CURRENT_DATE-($1::integer-1) GROUP BY day,event ORDER BY day`, days)
	if e != nil {
		dbError(w, e)
		return
	}
	daily := []map[string]any{}
	totals := map[string]int64{"song_open": 0, "category_open": 0, "search": 0, "no_results": 0, "favorite": 0}
	for rows.Next() {
		var date, event string
		var count int64
		if e = rows.Scan(&date, &event, &count); e != nil {
			rows.Close()
			dbError(w, e)
			return
		}
		daily = append(daily, map[string]any{"date": date, "event": event, "count": count})
		totals[event] += count
	}
	err := rows.Err()
	rows.Close()
	if err != nil {
		dbError(w, err)
		return
	}
	rows, e = s.DB.Query(r.Context(), `SELECT s.id::text,COALESCE(s.published->>'title',s.draft->>'title'),sum(a.count) AS views FROM analytics_daily a JOIN songs s ON s.id::text=a.song_id WHERE a.day>=CURRENT_DATE-($1::integer-1) AND a.event='song_open' GROUP BY s.id ORDER BY views DESC LIMIT 20`, days)
	if e != nil {
		dbError(w, e)
		return
	}
	songs := []map[string]any{}
	for rows.Next() {
		var id, title string
		var count int64
		if e = rows.Scan(&id, &title, &count); e != nil {
			rows.Close()
			dbError(w, e)
			return
		}
		songs = append(songs, map[string]any{"id": id, "title": title, "count": count})
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		dbError(w, err)
		return
	}
	alerts, e := s.Auth.MailAlerts(r.Context())
	if e != nil {
		dbError(w, e)
		return
	}
	write(w, 200, map[string]any{"daily": daily, "totals": totals, "topSongs": songs, "mailAlerts": alerts})
}
