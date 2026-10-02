package telemetry

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/jackc/pgx/v5/pgconn"
)

type recorder struct {
	mu     sync.Mutex
	events []*sentry.Event
}

func (r *recorder) Configure(sentry.ClientOptions)        {}
func (r *recorder) Flush(time.Duration) bool              { return true }
func (r *recorder) FlushWithContext(context.Context) bool { return true }
func (r *recorder) Close()                                {}
func (r *recorder) SendEvent(e *sentry.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, e)
}

func testClient(t *testing.T) *recorder {
	t.Helper()
	transport := &recorder{}
	client, err := sentry.NewClient(sentry.ClientOptions{Dsn: "https://public@example.com/1", Transport: transport, BeforeSend: scrub})
	if err != nil {
		t.Fatal(err)
	}
	old := sentry.CurrentHub().Client()
	sentry.CurrentHub().BindClient(client)
	t.Cleanup(func() { sentry.CurrentHub().BindClient(old) })
	return transport
}

func TestRequestErrorsArePrivateAndCorrelated(t *testing.T) {
	recorded := testClient(t)
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/songs/{id}", func(w http.ResponseWriter, r *http.Request) {
		Error(r.Context(), "database operation failed", &pgconn.PgError{Code: "23505", Message: "secret-lyric password=secret-password", Detail: "private@example.com"})
	})
	var wg sync.WaitGroup
	for range 5 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := httptest.NewRequest("POST", "/v1/songs/private-id?token=secret-token", strings.NewReader(`{"password":"secret-password"}`))
			r.Header.Set("Authorization", "Bearer private-bearer")
			w := httptest.NewRecorder()
			r = Request(w, r)
			mux.ServeHTTP(w, r)
		}()
	}
	wg.Wait()
	if len(recorded.events) != 5 {
		t.Fatalf("events = %d", len(recorded.events))
	}
	ids := map[string]bool{}
	for _, event := range recorded.events {
		id := event.Tags["request_id"]
		if len(id) != 26 || ids[id] {
			t.Fatalf("missing or reused request ID: %q", id)
		}
		ids[id] = true
		if event.Transaction != "POST /v1/songs/{id}" || event.Tags["sqlstate"] != "23505" || event.Tags["entry_point"] != "http" {
			t.Fatalf("missing safe context: %+v", event)
		}
		if len(event.Exception[0].Stacktrace.Frames) == 0 {
			t.Fatal("missing stack")
		}
		payload, _ := json.Marshal(event)
		for _, private := range []string{"secret-", "private-id", "private-bearer", "private@example.com", "Authorization"} {
			if strings.Contains(string(payload), private) {
				t.Fatalf("private data leaked: %s", private)
			}
		}
	}
}

func TestPanicAndWorkerErrors(t *testing.T) {
	recorded := testClient(t)
	ctx := Entry(context.Background(), "mailer")
	Error(ctx, "mail worker operation failed", errors.New("private smtp payload"))
	func() {
		defer func() {
			if v := recover(); v != nil {
				Panic(ctx, v)
			}
		}()
		panic("private panic value")
	}()
	if len(recorded.events) != 2 || recorded.events[1].Level != sentry.LevelFatal {
		t.Fatal("worker/panic events missing")
	}
	for _, event := range recorded.events {
		payload, _ := json.Marshal(event)
		if strings.Contains(string(payload), "private") {
			t.Fatal("error payload leaked")
		}
		if event.Tags["entry_point"] != "mailer" {
			t.Fatal("missing worker attribution")
		}
	}
}

func TestConfiguredTransportDeliversAndFlushes(t *testing.T) {
	payloads := make(chan string, 1)
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		payloads <- string(body)
		w.WriteHeader(http.StatusOK)
	}))
	defer endpoint.Close()
	t.Setenv("SENTRY_DSN", strings.Replace(endpoint.URL, "http://", "http://public@", 1)+"/1")
	t.Setenv("SENTRY_ENVIRONMENT", "test")
	old := sentry.CurrentHub().Client()
	t.Cleanup(func() { sentry.CurrentHub().BindClient(old) })
	if err := Init(); err != nil {
		t.Fatal(err)
	}
	id := Error(Entry(context.Background(), "verification"), "transport verification", errors.New("do-not-upload-secret"))
	if id == nil || !Flush() {
		t.Fatal("event not flushed")
	}
	select {
	case body := <-payloads:
		if !strings.Contains(body, string(*id)) || !strings.Contains(body, "transport verification") {
			t.Fatal("missing event on the wire")
		}
		if strings.Contains(body, "do-not-upload-secret") {
			t.Fatal("private error message sent")
		}
	default:
		t.Fatal("flush succeeded without delivering the event")
	}
}
