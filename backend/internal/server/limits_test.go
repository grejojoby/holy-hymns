package server

import (
	"fmt"
	"net/http/httptest"
	"testing"

	"holyhymns/internal/identity"
)

func TestSaturationReturnsRetryAndPreservesCORS(t *testing.T) {
	s := New(nil, identity.New(nil, identity.Config{}), "http://localhost:8081")
	for i := 0; i < cap(s.requests); i++ {
		s.requests <- struct{}{}
	}
	req := httptest.NewRequest("GET", "/v1/auth/providers", nil)
	req.Header.Set("Origin", "http://localhost:8081")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)
	if w.Code != 503 || w.Header().Get("Retry-After") != "1" || w.Header().Get("Access-Control-Allow-Origin") == "" {
		t.Fatal("missing retryable overload response", w.Code, w.Header())
	}
	<-s.requests
	w = httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatal("capacity did not recover", w.Code)
	}
}

func TestAnalyticsLimiterBoundsDistinctClients(t *testing.T) {
	s := New(nil, identity.New(nil, identity.Config{}), "")
	r := httptest.NewRequest("POST", "/v1/analytics", nil)
	for i := 0; i < 10000; i++ {
		r.RemoteAddr = fmt.Sprintf("[2001:db8::%x]:80", i+1)
		s.allow(r, "analytics", 120)
	}
	if len(s.limits) > 8192 {
		t.Fatal("unbounded distinct-client allocation", len(s.limits))
	}
}
