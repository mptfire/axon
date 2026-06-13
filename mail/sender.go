package mail

import (
	"fmt"
	"mime"
	"net"
	"net/smtp"
	"strings"
	"time"

	"heckel.io/ntfy/v2/log"
)

const (
	emailVerificationSubject = "Verify your email for ntfy"
	passwordResetSubject     = "Reset your ntfy password"
)

// Config holds the SMTP configuration for the mail sender
type Config struct {
	SMTPAddr string // SMTP server address (host:port)
	SMTPUser string // SMTP auth username
	SMTPPass string // SMTP auth password
	From     string // Sender email address
}

// Sender sends emails via SMTP, including the magic-link emails for email verification and
// password reset. Pending verification/reset state lives in the database (see user.Manager),
// not in this struct.
type Sender struct {
	config *Config
}

// NewSender creates a new mail Sender with the given SMTP config
func NewSender(config *Config) *Sender {
	return &Sender{config: config}
}

// Addr returns the SMTP server address
func (s *Sender) Addr() string {
	return s.config.SMTPAddr
}

// User returns the SMTP username
func (s *Sender) User() string {
	return s.config.SMTPUser
}

// From returns the sender email address
func (s *Sender) From() string {
	return s.config.From
}

// SendRaw sends a raw email message via SMTP
func (s *Sender) SendRaw(to string, message []byte) error {
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

// Send sends a plain text email via SMTP
func (s *Sender) Send(to, subject, body string) error {
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
	log.Tag("mail").Field("email_to", to).Debug("Sending email")
	return s.SendRaw(to, []byte(message))
}

// SendEmailVerification sends an email containing a magic link to verify ownership of the
// recipient address. The link carries a one-time token validated against the database.
func (s *Sender) SendEmailVerification(to, link string) error {
	body := fmt.Sprintf(`Click the link below to verify this email address for your ntfy account:

%s

This link expires in 24 hours. If you did not request this, you can safely ignore this email.`, link)
	return s.Send(to, emailVerificationSubject, body)
}

// SendPasswordReset sends an email containing a magic link to set a new password. The link
// carries a one-time token validated against the database.
func (s *Sender) SendPasswordReset(to, link string) error {
	body := fmt.Sprintf(`Click the link below to set a new password for your ntfy account:

%s

This link expires in 1 hour. If you did not request this, you can safely ignore this email -- your password will not change.`, link)
	return s.Send(to, passwordResetSubject, body)
}
