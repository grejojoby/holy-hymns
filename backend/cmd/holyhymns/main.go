package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"holyhymns/internal/identity"
	"holyhymns/internal/importer"
	"holyhymns/internal/migrations"
	"holyhymns/internal/server"
)

func env(name, def string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return def
}
func number(name string, def int) int {
	v, e := strconv.Atoi(os.Getenv(name))
	if e != nil || v < 1 {
		return def
	}
	return v
}
func list(name string) []string {
	out := []string{}
	for _, v := range strings.Split(os.Getenv(name), ",") {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	return out
}
func main() {
	if e := run(); e != nil {
		slog.Error("Holy Hymns stopped", "error", e)
		os.Exit(1)
	}
}
func run() error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	cmd := "serve"
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}
	if cmd == "export-lyrics" {
		return exportLyrics(ctx, os.Args[2:])
	}
	if cmd == "health" {
		client := http.Client{Timeout: 3 * time.Second}
		addr := env("HTTP_ADDR", ":8080")
		if strings.HasPrefix(addr, ":") {
			addr = "127.0.0.1" + addr
		}
		res, e := client.Get("http://" + addr + "/healthz")
		if e != nil {
			return e
		}
		defer res.Body.Close()
		if res.StatusCode != 200 {
			return fmt.Errorf("health returned %d", res.StatusCode)
		}
		return nil
	}
	cfg, e := pgxpool.ParseConfig(os.Getenv("DATABASE_URL"))
	if e != nil {
		return e
	}
	cfg.MaxConns = int32(number("DB_MAX_CONNS", 8))
	cfg.MinConns = 1
	cfg.MaxConnIdleTime = 5 * time.Minute
	db, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		return e
	}
	defer db.Close()
	if e = db.Ping(ctx); e != nil {
		return e
	}
	if e = migrations.Apply(ctx, db); e != nil {
		return e
	}
	auth := identity.New(db, identity.Config{PublicURL: env("PUBLIC_URL", "http://localhost:8080"), SMTPHost: os.Getenv("SMTP_HOST"), SMTPPort: number("SMTP_PORT", 587), SMTPUser: os.Getenv("SMTP_USER"), SMTPPassword: os.Getenv("SMTP_PASSWORD"), SMTPFrom: os.Getenv("SMTP_FROM"), MailEncryptionKey: os.Getenv("MAIL_ENCRYPTION_KEY"), MailDailyLimit: number("MAIL_DAILY_LIMIT", 80), MailMonthlyLimit: number("MAIL_MONTHLY_LIMIT", 2000), TrustedProxyCIDRs: list("TRUSTED_PROXY_CIDRS"), GoogleClientIDs: list("GOOGLE_CLIENT_IDS"), AppleClientIDs: list("APPLE_CLIENT_IDS"), AppleTeamID: os.Getenv("APPLE_TEAM_ID"), AppleKeyID: os.Getenv("APPLE_KEY_ID"), ApplePrivateKey: os.Getenv("APPLE_PRIVATE_KEY"), AllowInsecureSMTP: os.Getenv("ALLOW_INSECURE_SMTP") == "true"})
	switch cmd {
	case "migrate":
		fmt.Println("Database migrations applied.")
		return nil
	case "owner", "recover-owner":
		fs := flag.NewFlagSet(cmd, flag.ContinueOnError)
		email := fs.String("email", "", "owner email")
		name := fs.String("name", "Owner", "display name")
		if e = fs.Parse(os.Args[2:]); e != nil {
			return e
		}
		password := os.Getenv("HOLY_HYMNS_OWNER_PASSWORD")
		if password == "" || *email == "" {
			return errors.New("--email and HOLY_HYMNS_OWNER_PASSWORD are required")
		}
		if cmd == "owner" {
			e = auth.BootstrapOwner(ctx, *email, password, *name)
		} else {
			e = auth.RecoverOwner(ctx, *email, password)
		}
		if e == nil {
			fmt.Println("Owner operation completed and audited.")
		}
		return e
	case "import":
		fs := flag.NewFlagSet(cmd, flag.ContinueOnError)
		file := fs.String("file", "", "Blogger Atom/XML export")
		url := fs.String("url", "", "authorized blog feed URL")
		dry := fs.Bool("dry-run", false, "preview only")
		if e = fs.Parse(os.Args[2:]); e != nil {
			return e
		}
		var entries []importer.Entry
		if (*file == "") == (*url == "") {
			return errors.New("provide exactly one of --file or --url")
		}
		if *file != "" {
			f, e := os.Open(*file)
			if e != nil {
				return e
			}
			defer f.Close()
			entries, e = importer.Parse(f)
			if e != nil {
				return e
			}
		} else {
			entries, e = importer.Fetch(ctx, *url)
			if e != nil {
				return e
			}
		}
		report, e := server.ImportEntries(ctx, db, entries, *dry)
		if e != nil {
			return e
		}
		return json.NewEncoder(os.Stdout).Encode(report)
	case "serve":
		go auth.RunMailer(ctx)
		go maintain(ctx, db)
		srv := &http.Server{Addr: env("HTTP_ADDR", ":8080"), Handler: server.New(db, auth, env("ALLOWED_ORIGIN", "http://localhost:8081")).Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16384}
		go func() {
			<-ctx.Done()
			shutdown, c := context.WithTimeout(context.Background(), 10*time.Second)
			defer c()
			srv.Shutdown(shutdown)
		}()
		slog.Info("Holy Hymns API ready", "address", srv.Addr)
		e = srv.ListenAndServe()
		if errors.Is(e, http.ErrServerClosed) {
			return nil
		}
		return e
	default:
		return fmt.Errorf("unknown command %q", cmd)
	}
}
func exportLyrics(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("export-lyrics", flag.ContinueOnError)
	file := fs.String("file", "", "Blogger Atom/XML export")
	url := fs.String("url", "", "authorized Grejo Lyrics feed URL")
	out := fs.String("out", "", "new or empty lyric bundle directory")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 || (*file == "") == (*url == "") || *out == "" {
		return errors.New("provide exactly one of --file or --url, and --out DIRECTORY")
	}
	var entries []importer.Entry
	var err error
	if *file != "" {
		input, openErr := os.Open(*file)
		if openErr != nil {
			return openErr
		}
		defer input.Close()
		entries, err = importer.Parse(input)
	} else {
		entries, err = importer.Fetch(ctx, *url)
	}
	if err != nil {
		return err
	}
	summary, err := importer.ExportBundle(entries, *out)
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(summary)
}
func maintain(ctx context.Context, db *pgxpool.Pool) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		if _, e := db.Exec(ctx, `DELETE FROM analytics_daily WHERE day<CURRENT_DATE-90; DELETE FROM content_changes WHERE created_at<now()-interval '7 days'; DELETE FROM admin_audit WHERE created_at<now()-interval '365 days'; DELETE FROM import_runs WHERE created_at<now()-interval '90 days'`); e != nil && ctx.Err() == nil {
			slog.Error("retention cleanup", "error", e)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
