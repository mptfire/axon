package mail

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"mime"
	"net"
	"net/smtp"
	"strings"
	"sync"
	"time"

	"heckel.io/ntfy/v2/log"
)

const (
	verifyCodeExpiry  = 10 * time.Minute
	verifyCodeLength  = 6
	verifyCodeSubject = "ntfy email verification"
)

// Config holds the SMTP configuration for the mail sender
type Config struct {
	SMTPAddr string // SMTP server address (host:port)
	SMTPUser string // SMTP auth username
	SMTPPass string // SMTP auth password
	From     string // Sender email address
}

// Sender sends emails and manages email verification codes
type Sender struct {
	config      *Config
	verifyCodes map[string]verifyCode // keyed by email
	mu          sync.Mutex
}

type verifyCode struct {
	code    string
	expires time.Time
}

// NewSender creates a new mail Sender with the given SMTP config
func NewSender(config *Config) *Sender {
	return &Sender{
		config:      config,
		verifyCodes: make(map[string]verifyCode),
	}
}

// Send sends a plain text email via SMTP
func (s *Sender) Send(to, subject, body string) error {
	host, _, err := net.SplitHostPort(s.config.SMTPAddr)
	if err != nil {
		return err
	}
	var auth smtp.Auth
	if s.config.SMTPUser != "" {
		auth = smtp.PlainAuth("", s.config.SMTPUser, s.config.SMTPPass, host)
	}
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
	return smtp.SendMail(s.config.SMTPAddr, auth, s.config.From, []string{to}, []byte(message))
}

// SendVerification generates a 6-digit code, stores it in-memory, and sends a verification email
func (s *Sender) SendVerification(to string) error {
	code, err := generateCode()
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.verifyCodes[to] = verifyCode{
		code:    code,
		expires: time.Now().Add(verifyCodeExpiry),
	}
	s.mu.Unlock()
	body := fmt.Sprintf("Your ntfy email verification code is: %s\n\nThis code expires in 10 minutes.", code)
	return s.Send(to, verifyCodeSubject, body)
}

// CheckVerification checks if the code matches and hasn't expired. Removes the entry on success.
func (s *Sender) CheckVerification(email, code string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	vc, ok := s.verifyCodes[email]
	if !ok || time.Now().After(vc.expires) || vc.code != code {
		return false
	}
	delete(s.verifyCodes, email)
	return true
}

// ExpireVerificationCodes removes expired entries from the in-memory map
func (s *Sender) ExpireVerificationCodes() {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for email, vc := range s.verifyCodes {
		if now.After(vc.expires) {
			delete(s.verifyCodes, email)
		}
	}
}

func generateCode() (string, error) {
	max := big.NewInt(1000000) // 0-999999
	n, err := rand.Int(rand.Reader, max)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%06d", n.Int64()), nil
}
