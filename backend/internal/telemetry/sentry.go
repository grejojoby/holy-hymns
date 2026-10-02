// Package telemetry reports operational failures without sending application data.
package telemetry

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/jackc/pgx/v5/pgconn"
)

type requestKey struct{}
type requestInfo struct {
	request *http.Request
	id      string
}
type entryKey struct{}

// Set by the image build; an explicit runtime value can override it.
var release string

func Init() error {
	// An empty DSN is an explicit opt-out, including in local development.
	if os.Getenv("SENTRY_DSN") == "" {
		return nil
	}
	version := os.Getenv("SENTRY_RELEASE")
	if version == "" {
		version = release
	}
	transport := sentry.NewHTTPTransport()
	transport.BufferSize = 30
	transport.Timeout = 3 * time.Second
	return sentry.Init(sentry.ClientOptions{
		Dsn: os.Getenv("SENTRY_DSN"), Environment: os.Getenv("SENTRY_ENVIRONMENT"),
		Release: version, SendDefaultPII: false,
		Debug:         os.Getenv("SENTRY_DEBUG") == "true",
		Transport:     transport,
		EnableTracing: false, BeforeSend: scrub,
	})
}

func Flush() bool { return sentry.Flush(3 * time.Second) }

func Entry(ctx context.Context, name string) context.Context {
	return context.WithValue(ctx, entryKey{}, name)
}

// Request gives each request a private hub and generated correlation ID. It
// deliberately does not call SetRequest, which would collect headers and bodies.
func Request(w http.ResponseWriter, r *http.Request) *http.Request {
	hub := sentry.CurrentHub().Clone()
	requestID := rand.Text()
	w.Header().Set("X-Request-ID", requestID)
	hub.ConfigureScope(func(scope *sentry.Scope) {
		scope.SetTag("request_id", requestID)
		scope.SetTag("method", r.Method)
	})
	r = r.WithContext(sentry.SetHubOnContext(Entry(r.Context(), "http"), hub))
	// Keep the same pointer passed to ServeMux so its resolved Pattern is visible.
	*r = *r.WithContext(context.WithValue(r.Context(), requestKey{}, requestInfo{r, requestID}))
	return r
}

// Error records full details locally, but only the operation, error type and
// SQLSTATE remotely. Driver/provider messages can contain passwords or user data.
func Error(ctx context.Context, operation string, err error) *sentry.EventID {
	return capture(ctx, operation, err, sentry.LevelError)
}

func Panic(ctx context.Context, value any) *sentry.EventID {
	return capture(ctx, "request panic", value, sentry.LevelFatal)
}

func capture(ctx context.Context, operation string, value any, level sentry.Level) *sentry.EventID {
	hub := sentry.GetHubFromContext(ctx)
	if hub == nil {
		hub = sentry.CurrentHub()
	}
	event := sentry.NewEvent()
	event.Level = level
	event.Message = operation
	event.Exception = []sentry.Exception{{Type: fmt.Sprintf("%T", value), Value: operation, Stacktrace: sentry.NewStacktrace()}}
	entry, _ := ctx.Value(entryKey{}).(string)
	if entry != "" {
		event.Tags["entry_point"] = entry
	}
	if info, ok := ctx.Value(requestKey{}).(requestInfo); ok && info.request.Pattern != "" {
		event.Transaction = info.request.Pattern
		event.Tags["route"] = info.request.Pattern
	}
	if err, ok := value.(error); ok {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) {
			event.Tags["sqlstate"] = pgErr.Code
		}
	}
	id := hub.CaptureEvent(event)
	attrs := []any{"error", value, "entry_point", entry, "route", event.Transaction}
	if info, ok := ctx.Value(requestKey{}).(requestInfo); ok {
		attrs = append(attrs, "request_id", info.id)
	}
	if id != nil {
		attrs = append(attrs, "sentry_event_id", string(*id))
	}
	slog.ErrorContext(ctx, operation, attrs...)
	return id
}

func scrub(event *sentry.Event, _ *sentry.EventHint) *sentry.Event {
	event.Request = nil
	event.User = sentry.User{}
	event.Breadcrumbs = nil
	event.ServerName = ""
	for i := range event.Exception {
		if stack := event.Exception[i].Stacktrace; stack != nil {
			for j := range stack.Frames {
				stack.Frames[j].Vars = nil
			}
		}
	}
	return event
}
