package identity

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestEncryptedMailCannotBeMovedToAnotherRecipientOrTamperedWith(t *testing.T) {
	s := New(nil, testMailConfig())
	p := mailPayload{Subject: "Verify", Body: "secret-token"}
	encrypted, err := s.encryptMail("reader@example.com", p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.decryptMail("other@example.com", encrypted); err == nil {
		t.Fatal("recipient substitution accepted")
	}
	encrypted[len(encrypted)-1] ^= 1
	if _, err = s.decryptMail("reader@example.com", encrypted); err == nil {
		t.Fatal("tampered message accepted")
	}
}

func TestConcurrentMailReservationsCannotExceedFreeQuota(t *testing.T) {
	cfg := testMailConfig()
	cfg.MailDailyLimit = 1
	s := testIdentity(t, cfg)
	var successes atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ctx := context.Background()
			tx, err := s.db.Begin(ctx)
			if err != nil {
				t.Error(err)
				return
			}
			defer tx.Rollback(ctx)
			err = s.reserveMail(ctx, tx, fmt.Sprintf("reader%d@example.com", i), "queued")
			if err == nil {
				if err = tx.Commit(ctx); err != nil {
					t.Error(err)
					return
				}
				successes.Add(1)
			} else if !errors.Is(err, errMailQuota) {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	if successes.Load() != 1 {
		t.Fatalf("quota admitted %d requests", successes.Load())
	}
}

func TestOutboxWorkerDeliversAndErasesActionPayload(t *testing.T) {
	s := testIdentity(t, testMailConfig())
	ctx := context.Background()
	id, _ := seedAccount(t, s, "reader@example.com", "reader")
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	_, port, _ := net.SplitHostPort(listener.Addr().String())
	s.cfg.SMTPHost = "127.0.0.1"
	s.cfg.SMTPPort, _ = strconv.Atoi(port)
	received := make(chan string, 1)
	serverError := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			serverError <- err
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
		reader := bufio.NewReader(conn)
		fmt.Fprint(conn, "220 localhost test SMTP\r\n")
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				serverError <- err
				return
			}
			switch {
			case strings.HasPrefix(line, "EHLO"), strings.HasPrefix(line, "HELO"), strings.HasPrefix(line, "MAIL"), strings.HasPrefix(line, "RCPT"):
				fmt.Fprint(conn, "250 OK\r\n")
			case strings.HasPrefix(line, "DATA"):
				fmt.Fprint(conn, "354 Send data\r\n")
				var body strings.Builder
				for {
					data, err := reader.ReadString('\n')
					if err != nil {
						serverError <- err
						return
					}
					if data == ".\r\n" {
						break
					}
					body.WriteString(data)
				}
				received <- body.String()
				fmt.Fprint(conn, "250 Accepted\r\n")
			case strings.HasPrefix(line, "QUIT"):
				fmt.Fprint(conn, "221 Bye\r\n")
				return
			default:
				fmt.Fprint(conn, "500 Unsupported\r\n")
			}
		}
	}()
	tx, err := s.db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.queueAction(ctx, tx, id, "reader@example.com", "verify", time.Hour); err != nil {
		tx.Rollback(ctx)
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err = s.deliverOne(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case body := <-received:
		if !strings.Contains(body, "https://hymns.example/auth/verify#token=") {
			t.Fatal("missing action URL")
		}
	case err := <-serverError:
		t.Fatal(err)
	case <-time.After(time.Second):
		t.Fatal("mail did not arrive")
	}
	var status string
	var payloadSize, attempted int
	if err = s.db.QueryRow(ctx, `SELECT status,octet_length(encrypted_body) FROM mail_outbox`).Scan(&status, &payloadSize); err != nil {
		t.Fatal(err)
	}
	if status != "sent" || payloadSize != 0 {
		t.Fatal("mail payload retained after sending", status, payloadSize)
	}
	if err = s.db.QueryRow(ctx, `SELECT count(*) FROM mail_usage WHERE stage='attempted'`).Scan(&attempted); err != nil || attempted != 1 {
		t.Fatal("SMTP attempt quota not recorded", attempted, err)
	}
}
