package mail

import (
	"fmt"
	"mime"
	"net"
	"net/smtp"
	"strings"
	"sync"
	"time"

	"heckel.io/ntfy/v2/log"
	"heckel.io/ntfy/v2/model"
)

const (
	tagMail = "mail"

	emailVerificationSubject = "Verify your email for ntfy"
	passwordResetSubject     = "Reset your ntfy password"
)

// Config holds the SMTP configuration for the mail sender
type Config struct {
	BaseURL  string // ntfy base URL, used to build topic URLs in notification emails
	SMTPAddr string // SMTP server address (host:port)
	SMTPUser string // SMTP auth username
	SMTPPass string // SMTP auth password
	From     string // Sender email address
}

// Sender sends all of ntfy's outgoing email: notification emails (the email-on-publish feature)
// as well as the magic-link emails for email verification and password reset. realSender is the
// SMTP-backed implementation; tests inject a fake.
type Sender interface {
	SendNotification(to string, m *model.Message, senderIP string) error
	Counts() (total int64, success int64, failure int64)
	SendEmailVerification(to, link string) error
	SendPasswordReset(to, link string) error
}

// realSender is the SMTP-backed implementation of Sender. Pending verification/reset state lives
// in the database (see user.Manager), not in this struct.
type realSender struct {
	config  *Config
	success int64
	failure int64
	mu      sync.Mutex
}

// NewSender creates a new mail Sender with the given SMTP config
func NewSender(config *Config) Sender {
	return &realSender{config: config}
}

// SendNotification formats a ntfy message into a notification email and sends it via SMTP. It
// tracks success/failure counts, exposed via Counts (used for the server stats).
func (s *realSender) SendNotification(to string, m *model.Message, senderIP string) error {
	message, err := formatMail(s.config.BaseURL, senderIP, s.config.From, to, m)
	if err != nil {
		s.count(false)
		return err
	}
	log.Tag(tagMail).Field("email_to", to).Debug("Sending notification email")
	err = s.sendRaw(to, []byte(message))
	s.count(err == nil)
	return err
}

// Counts returns the number of notification emails sent, broken down into total, success and failure
func (s *realSender) Counts() (total int64, success int64, failure int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.success + s.failure, s.success, s.failure
}

// SendEmailVerification sends an email containing a magic link to verify ownership of the
// recipient address. The link carries a one-time token validated against the database.
func (s *realSender) SendEmailVerification(to, link string) error {
	body := fmt.Sprintf(`Click the link below to verify this email address for your ntfy account:

%s

This link expires in 24 hours. If you did not request this, you can safely ignore this email.`, link)
	return s.send(to, emailVerificationSubject, body)
}

// SendPasswordReset sends an email containing a magic link to set a new password. The link
// carries a one-time token validated against the database.
func (s *realSender) SendPasswordReset(to, link string) error {
	body := fmt.Sprintf(`Click the link below to set a new password for your ntfy account:

%s

This link expires in 1 hour. If you did not request this, you can safely ignore this email -- your password will not change.`, link)
	return s.send(to, passwordResetSubject, body)
}

// send sends a plain text email via SMTP
func (s *realSender) send(to, subject, body string) error {
	date := time.Now().UTC().Format(time.RFC1123Z)
	encodedSubject := mime.BEncoding.Encode("utf-8", subject)
	message := `From: ntfy <{from}>
To: {to}
Date: {date}
Subject: {subject}
Content-Type: text/plain; charset="utf-8"

{body}`
	message = strings.ReplaceAll(message, "{from}", s.config.From)
	message = strings.ReplaceAll(message, "{to}", to)
	message = strings.ReplaceAll(message, "{date}", date)
	message = strings.ReplaceAll(message, "{subject}", encodedSubject)
	message = strings.ReplaceAll(message, "{body}", body)
	log.Tag(tagMail).Field("email_to", to).Debug("Sending email")
	return s.sendRaw(to, []byte(message))
}

// sendRaw sends a raw email message via SMTP
func (s *realSender) sendRaw(to string, message []byte) error {
	host, _, err := net.SplitHostPort(s.config.SMTPAddr)
	if err != nil {
		return err
	}
	var auth smtp.Auth
	if s.config.SMTPUser != "" {
		auth = smtp.PlainAuth("", s.config.SMTPUser, s.config.SMTPPass, host)
	}
	return smtp.SendMail(s.config.SMTPAddr, auth, s.config.From, []string{to}, message)
}

func (s *realSender) count(ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ok {
		s.success++
	} else {
		s.failure++
	}
}
