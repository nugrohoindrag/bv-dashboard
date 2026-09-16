// Package mailer: pengiriman email transaksional (verifikasi signup, notifikasi trial, Book a Demo).
// SMTP polos/STARTTLS lewat net/smtp; tanpa host → LogMailer (dev/test) yang hanya mencatat ke log.
package mailer

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"mime"
	"net"
	"net/smtp"
	"strings"
	"time"
)

type Message struct {
	To      []string
	Subject string
	Text    string // plain text (wajib)
	HTML    string // opsional
}

type Mailer interface {
	Send(ctx context.Context, m Message) error
}

// LogMailer: tidak mengirim apa pun; mencatat subjek + penerima (dan isi pada level debug).
type LogMailer struct{ Log *slog.Logger }

func (l LogMailer) Send(_ context.Context, m Message) error {
	if l.Log != nil {
		l.Log.Info("mail (log-only)", "to", strings.Join(m.To, ","), "subject", m.Subject)
		l.Log.Debug("mail body", "text", m.Text)
	}
	return nil
}

// Recorder: menyimpan pesan di memori (integration test).
type Recorder struct{ Sent []Message }

func (r *Recorder) Send(_ context.Context, m Message) error { r.Sent = append(r.Sent, m); return nil }

type SMTP struct {
	Host, User, Password, From string
	Port                       int
	StartTLS                   bool
	Timeout                    time.Duration
}

func (s SMTP) Send(ctx context.Context, m Message) error {
	if len(m.To) == 0 {
		return fmt.Errorf("mailer: penerima kosong")
	}
	addr := fmt.Sprintf("%s:%d", s.Host, s.Port)
	timeout := s.Timeout
	if timeout == 0 {
		timeout = 15 * time.Second
	}
	d := net.Dialer{Timeout: timeout}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("mailer dial: %w", err)
	}
	c, err := smtp.NewClient(conn, s.Host)
	if err != nil {
		return fmt.Errorf("mailer client: %w", err)
	}
	defer func() { _ = c.Close() }()
	if s.StartTLS {
		if err := c.StartTLS(&tls.Config{ServerName: s.Host, MinVersion: tls.VersionTLS12}); err != nil {
			return fmt.Errorf("mailer starttls: %w", err)
		}
	}
	if s.User != "" {
		if err := c.Auth(smtp.PlainAuth("", s.User, s.Password, s.Host)); err != nil {
			return fmt.Errorf("mailer auth: %w", err)
		}
	}
	fromAddr := s.From
	if i := strings.Index(fromAddr, "<"); i >= 0 {
		fromAddr = strings.Trim(fromAddr[i:], "<>")
	}
	if err := c.Mail(fromAddr); err != nil {
		return err
	}
	for _, to := range m.To {
		if err := c.Rcpt(to); err != nil {
			return err
		}
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write([]byte(build(s.From, m))); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return c.Quit()
}

func build(from string, m Message) string {
	var b strings.Builder
	boundary := fmt.Sprintf("bv-%d", time.Now().UnixNano())
	b.WriteString("From: " + from + "\r\n")
	b.WriteString("To: " + strings.Join(m.To, ", ") + "\r\n")
	b.WriteString("Subject: " + mime.QEncoding.Encode("utf-8", m.Subject) + "\r\n")
	b.WriteString("Date: " + time.Now().Format(time.RFC1123Z) + "\r\n")
	b.WriteString("MIME-Version: 1.0\r\n")
	if m.HTML == "" {
		b.WriteString("Content-Type: text/plain; charset=utf-8\r\n\r\n")
		b.WriteString(m.Text)
		return b.String()
	}
	b.WriteString("Content-Type: multipart/alternative; boundary=" + boundary + "\r\n\r\n")
	b.WriteString("--" + boundary + "\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n" + m.Text + "\r\n")
	b.WriteString("--" + boundary + "\r\nContent-Type: text/html; charset=utf-8\r\n\r\n" + m.HTML + "\r\n")
	b.WriteString("--" + boundary + "--\r\n")
	return b.String()
}
