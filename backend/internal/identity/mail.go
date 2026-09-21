package identity

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/smtp"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

var errMailQuota = errors.New("email sending quota reached")

type mailPayload struct {
	Subject string `json:"subject"`
	Body    string `json:"body"`
}
type MailAlert struct {
	Kind        string    `json:"kind"`
	Message     string    `json:"message"`
	Occurrences int64     `json:"occurrences"`
	LastSeenAt  time.Time `json:"lastSeenAt"`
}

func (s *Service) mailCipher() (cipher.AEAD, error) {
	key, err := base64.StdEncoding.DecodeString(s.cfg.MailEncryptionKey)
	if err != nil || len(key) != 32 {
		return nil, errors.New("MAIL_ENCRYPTION_KEY must be base64 encoding of 32 random bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}
func (s *Service) mailConfigured() bool {
	u, err := url.Parse(s.cfg.PublicURL)
	if err != nil || u.Host == "" || (u.Scheme != "https" && !(s.cfg.AllowInsecureSMTP && u.Scheme == "http")) {
		return false
	}
	_, err = normalizeEmail(s.cfg.SMTPFrom)
	if err != nil || s.cfg.SMTPHost == "" || s.cfg.SMTPPort < 1 || s.cfg.SMTPPort > 65535 {
		return false
	}
	_, err = s.mailCipher()
	return err == nil
}
func (s *Service) mailReady(w http.ResponseWriter) bool {
	if !s.mailConfigured() {
		writeError(w, 503, "email_unavailable", "Email delivery is not configured. Please contact the app owner.")
		return false
	}
	return true
}
func (s *Service) mailError(w http.ResponseWriter, err error) {
	if errors.Is(err, errMailQuota) {
		s.recordMailAlert(context.Background(), "quota", "Email quota or recipient limit reached. Review SMTP limits before retrying.")
		w.Header().Set("Retry-After", "3600")
		writeError(w, 429, "email_quota", "Email sending limit reached. Please retry later or contact the owner.")
		return
	}
	internal(w, err)
}
func (s *Service) encryptMail(recipient string, p mailPayload) ([]byte, error) {
	aead, err := s.mailCipher()
	if err != nil {
		return nil, err
	}
	plain, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return nil, err
	}
	return aead.Seal(nonce, nonce, plain, []byte(recipient)), nil
}
func (s *Service) decryptMail(recipient string, encrypted []byte) (mailPayload, error) {
	var p mailPayload
	aead, err := s.mailCipher()
	if err != nil {
		return p, err
	}
	if len(encrypted) < aead.NonceSize() {
		return p, errors.New("invalid encrypted email")
	}
	plain, err := aead.Open(nil, encrypted[:aead.NonceSize()], encrypted[aead.NonceSize():], []byte(recipient))
	if err != nil {
		return p, err
	}
	err = json.Unmarshal(plain, &p)
	return p, err
}

// quota uses a database transaction lock so multiple API/worker processes cannot
// overshoot the allowance. Reservations survive account deletion as anonymous hashes.
func (s *Service) reserveMail(ctx context.Context, tx pgx.Tx, email, stage string) error {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(802012)`); err != nil {
		return err
	}
	var day, month, burst, recipientHour, recipientDay int
	err := tx.QueryRow(ctx, `SELECT count(*) FILTER(WHERE created_at>now()-interval '24 hours'),count(*) FILTER(WHERE created_at>=date_trunc('month',now() AT TIME ZONE 'UTC') AT TIME ZONE 'UTC'),count(*) FILTER(WHERE created_at>now()-interval '1 minute'),count(*) FILTER(WHERE recipient_hash=$2 AND created_at>now()-interval '1 hour'),count(*) FILTER(WHERE recipient_hash=$2 AND created_at>now()-interval '24 hours') FROM mail_usage WHERE stage=$1 AND created_at>now()-interval '35 days'`, stage, tokenHash(email)).Scan(&day, &month, &burst, &recipientHour, &recipientDay)
	if err != nil {
		return err
	}
	if day >= s.cfg.MailDailyLimit || month >= s.cfg.MailMonthlyLimit || burst >= 5 || recipientHour >= 3 || recipientDay >= 5 {
		return errMailQuota
	}
	_, err = tx.Exec(ctx, `INSERT INTO mail_usage(recipient_hash,stage) VALUES($1,$2)`, tokenHash(email), stage)
	return err
}
func (s *Service) queueAction(ctx context.Context, tx pgx.Tx, userID, email, purpose string, lifetime time.Duration) error {
	if !s.mailConfigured() {
		return errors.New("SMTP is not configured")
	}
	if err := s.reserveMail(ctx, tx, email, "queued"); err != nil {
		return err
	}
	token, err := randomToken()
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `DELETE FROM auth_tokens WHERE email=$1 AND purpose=$2`, email, purpose); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE mail_outbox SET status='failed',encrypted_body=''::bytea WHERE recipient=$1 AND purpose=$2 AND status='pending'`, email, purpose); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO auth_tokens(token_hash,purpose,user_id,email,expires_at) VALUES($1,$2,nullif($3,'')::uuid,$4,now()+make_interval(secs => $5))`, tokenHash(token), purpose, userID, email, lifetime.Seconds()); err != nil {
		return err
	}
	path := "verify"
	subject := "Verify your Holy Hymns email"
	explanation := "Verify your email address to sign in to Holy Hymns."
	if purpose == "reset" {
		path = "reset-password"
		subject = "Reset your Holy Hymns password"
		explanation = "Use this one-time link to reset your password."
	}
	if purpose == "invite" {
		path = "accept-invitation"
		subject = "Holy Hymns administrator invitation"
		explanation = "The owner invited you to administer Holy Hymns. If you already have an account, sign in before accepting."
	}
	link := "holyhymns://auth/" + path + "?token=" + url.QueryEscape(token)
	body := fmt.Sprintf("%s\n\nOpen this link on the device with Holy Hymns installed:\n%s\n\nYou can also paste this token into the app's %s screen:\n%s\n\nThis link expires in %d hours and can be used once. If you did not request this, ignore this email.\n\nHoly Hymns — %s\n", explanation, link, path, token, int(lifetime.Hours()), s.cfg.PublicURL)
	encrypted, err := s.encryptMail(email, mailPayload{Subject: subject, Body: body})
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO mail_outbox(recipient,purpose,encrypted_body,expires_at) VALUES($1,$2,$3,now()+make_interval(secs=>$4))`, email, purpose, encrypted, lifetime.Seconds())
	return err
}

func (s *Service) recordMailAlert(ctx context.Context, kind, message string) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	_, err := s.db.Exec(ctx, `INSERT INTO mail_alerts(kind,message) VALUES($1,$2) ON CONFLICT(kind) DO UPDATE SET message=excluded.message,occurrences=mail_alerts.occurrences+1,last_seen_at=now()`, kind, message)
	if err != nil {
		slog.Error("mail alert could not be recorded", "kind", kind)
	}
}
func (s *Service) MailAlerts(ctx context.Context) ([]MailAlert, error) {
	rows, err := s.db.Query(ctx, `SELECT kind,message,occurrences,last_seen_at FROM mail_alerts ORDER BY last_seen_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []MailAlert{}
	for rows.Next() {
		var a MailAlert
		if err = rows.Scan(&a.Kind, &a.Message, &a.Occurrences, &a.LastSeenAt); err != nil {
			return nil, err
		}
		items = append(items, a)
	}
	return items, rows.Err()
}

func (s *Service) RunMailer(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	cleanup := time.NewTicker(time.Hour)
	defer cleanup.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if s.mailConfigured() {
				if err := s.deliverOne(ctx); err != nil && !errors.Is(err, pgx.ErrNoRows) {
					slog.Error("mail worker operation failed", "error", err)
				}
			}
		case <-cleanup.C:
			s.cleanup(ctx)
		}
	}
}
func (s *Service) cleanup(ctx context.Context) {
	for _, q := range []string{`DELETE FROM auth_sessions WHERE expires_at<now()`, `DELETE FROM auth_tokens WHERE expires_at<now()`, `DELETE FROM auth_challenges WHERE expires_at<now()`, `DELETE FROM auth_rate_limits WHERE reset_at<now()-interval '1 day'`, `DELETE FROM mail_usage WHERE created_at<now()-interval '35 days'`, `UPDATE mail_outbox SET status='failed',encrypted_body=''::bytea WHERE status='pending' AND expires_at<=now()`, `DELETE FROM mail_outbox WHERE created_at<now()-interval '7 days'`, `DELETE FROM security_audit WHERE created_at<now()-interval '90 days'`} {
		if _, err := s.db.Exec(ctx, q); err != nil {
			slog.Error("identity retention cleanup failed", "error", err)
		}
	}
}
func (s *Service) deliverOne(ctx context.Context) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var id, email string
	var encrypted []byte
	var attempts int
	err = tx.QueryRow(ctx, `SELECT id::text,recipient,encrypted_body,attempts FROM mail_outbox WHERE status='pending' AND expires_at>now() AND next_attempt_at<=now() AND (claimed_until IS NULL OR claimed_until<now()) ORDER BY created_at FOR UPDATE SKIP LOCKED LIMIT 1`).Scan(&id, &email, &encrypted, &attempts)
	if err != nil {
		return err
	}
	err = s.reserveMail(ctx, tx, email, "attempted")
	if errors.Is(err, errMailQuota) {
		_, err = tx.Exec(ctx, `UPDATE mail_outbox SET next_attempt_at=now()+interval '1 hour' WHERE id=$1`, id)
		if err != nil {
			return err
		}
		if err = tx.Commit(ctx); err != nil {
			return err
		}
		s.recordMailAlert(ctx, "quota", "Email delivery paused at configured free allowance. Pending mail will retry later.")
		return nil
	}
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE mail_outbox SET claimed_until=now()+interval '2 minutes',attempts=attempts+1 WHERE id=$1`, id)
	if err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	p, err := s.decryptMail(email, encrypted)
	if err == nil {
		err = s.sendMail(ctx, email, p)
	}
	if err != nil {
		status := "pending"
		if attempts >= 2 {
			status = "failed"
		}
		_, dbErr := s.db.Exec(ctx, `UPDATE mail_outbox SET status=$2,claimed_until=NULL,next_attempt_at=now()+interval '15 minutes' WHERE id=$1`, id, status)
		s.recordMailAlert(ctx, "delivery", "An email could not be delivered. Check SMTP connectivity and credentials; no paid fallback is used.")
		return dbErr
	}
	_, err = s.db.Exec(ctx, `UPDATE mail_outbox SET status='sent',sent_at=now(),claimed_until=NULL,encrypted_body=''::bytea WHERE id=$1`, id)
	return err
}

func (s *Service) sendMail(ctx context.Context, recipient string, p mailPayload) error {
	addr := net.JoinHostPort(s.cfg.SMTPHost, strconv.Itoa(s.cfg.SMTPPort))
	d := &net.Dialer{Timeout: 15 * time.Second}
	var conn net.Conn
	var err error
	if s.cfg.SMTPPort == 465 {
		conn, err = (&tls.Dialer{NetDialer: d, Config: &tls.Config{ServerName: s.cfg.SMTPHost, MinVersion: tls.VersionTLS12}}).DialContext(ctx, "tcp", addr)
	} else {
		conn, err = d.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(30 * time.Second))
	c, err := smtp.NewClient(conn, s.cfg.SMTPHost)
	if err != nil {
		return err
	}
	defer c.Close()
	if s.cfg.SMTPPort != 465 {
		if ok, _ := c.Extension("STARTTLS"); ok {
			if err = c.StartTLS(&tls.Config{ServerName: s.cfg.SMTPHost, MinVersion: tls.VersionTLS12}); err != nil {
				return err
			}
		} else if !s.cfg.AllowInsecureSMTP {
			return errors.New("SMTP server requires TLS")
		}
	}
	if s.cfg.SMTPUser != "" {
		if err = c.Auth(smtp.PlainAuth("", s.cfg.SMTPUser, s.cfg.SMTPPassword, s.cfg.SMTPHost)); err != nil {
			return err
		}
	}
	if err = c.Mail(s.cfg.SMTPFrom); err != nil {
		return err
	}
	if err = c.Rcpt(recipient); err != nil {
		return err
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	body := strings.ReplaceAll(strings.ReplaceAll(p.Body, "\r\n", "\n"), "\n", "\r\n")
	_, err = io.WriteString(w, "From: "+s.cfg.SMTPFrom+"\r\nTo: "+recipient+"\r\nSubject: "+p.Subject+"\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n"+body)
	if err != nil {
		return err
	}
	if err = w.Close(); err != nil {
		return err
	}
	return c.Quit()
}
