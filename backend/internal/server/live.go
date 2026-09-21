package server

import (
	"fmt"
	"net/http"
	"time"
)

// Events are hints; durable content_state is always authoritative after reconnect.
func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	select {
	case s.streams <- struct{}{}:
		defer func() { <-s.streams }()
	default:
		fail(w, 503, "live updates busy; retry shortly")
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		fail(w, 500, "streaming unavailable")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	last := int64(-1)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	heartbeat := 0
	for {
		var revision int64
		if e := s.DB.QueryRow(r.Context(), `SELECT revision FROM content_state`).Scan(&revision); e != nil {
			return
		}
		controller := http.NewResponseController(w)
		controller.SetWriteDeadline(time.Now().Add(10 * time.Second))
		if revision != last {
			if _, e := fmt.Fprintf(w, "id: %d\nevent: content\ndata: {\"revision\":%d}\n\n", revision, revision); e != nil {
				return
			}
			flusher.Flush()
			last = revision
		} else if heartbeat%10 == 0 {
			if _, e := fmt.Fprint(w, ": keepalive\n\n"); e != nil {
				return
			}
			flusher.Flush()
		}
		heartbeat++
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
		}
	}
}
