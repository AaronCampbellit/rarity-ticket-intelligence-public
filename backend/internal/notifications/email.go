package notifications

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/mail"
	"net/smtp"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"
)

const maxEmailSubjectBytes = 240

var ErrEmailDeliveryFailed = errors.New("email delivery failed")

type EmailRequest struct{ EventID, IdempotencyKey, Recipient, Subject, Body, AuthenticatedURL string }
type EmailTransport interface {
	Send(context.Context, EmailRequest) error
}

type MentionEmail struct {
	EventID, Recipient, AuthorLabel, ObjectDisplayID, ObjectSubject, AuthenticatedURL string
}

type CalendarEmail struct {
	EventID, DeduplicationKey, Recipient, Title, Body, AuthenticatedURL string
}

type EmailService struct{ transport EmailTransport }

func NewEmailService(transport EmailTransport) *EmailService {
	return &EmailService{transport: transport}
}

func (s *EmailService) Deliver(ctx context.Context, email MentionEmail) error {
	if s == nil || s.transport == nil || strings.TrimSpace(email.EventID) == "" || !ValidEmailRecipient(email.Recipient) || !validAuthenticatedURL(email.AuthenticatedURL) {
		return ErrEmailDeliveryFailed
	}
	author := boundedHeader(email.AuthorLabel, 64)
	display := boundedHeader(email.ObjectDisplayID, 48)
	subject := boundedHeader(email.ObjectSubject, 112)
	if author == "" || display == "" || subject == "" {
		return ErrEmailDeliveryFailed
	}
	header := boundedHeader(fmt.Sprintf("%s mentioned you on %s: %s", author, display, subject), maxEmailSubjectBytes)
	if header == "" {
		return ErrEmailDeliveryFailed
	}
	return s.transport.Send(ctx, EmailRequest{EventID: email.EventID, Recipient: strings.TrimSpace(email.Recipient), Subject: header, AuthenticatedURL: email.AuthenticatedURL})
}

func (s *EmailService) DeliverCalendar(ctx context.Context, email CalendarEmail) error {
	if s == nil || s.transport == nil || !validCalendarEmail(email) {
		return ErrEmailDeliveryFailed
	}
	title := boundedHeader(email.Title, maxEmailSubjectBytes)
	body := strings.TrimSpace(strings.ReplaceAll(email.Body, "\r", ""))
	if title == "" || body == "" {
		return ErrEmailDeliveryFailed
	}
	if len(body) > 4000 {
		body = body[:4000]
		for !utf8.ValidString(body) {
			body = body[:len(body)-1]
		}
	}
	return s.transport.Send(ctx, EmailRequest{
		EventID: email.EventID, IdempotencyKey: email.DeduplicationKey,
		Recipient: strings.TrimSpace(email.Recipient), Subject: title,
		Body: body, AuthenticatedURL: email.AuthenticatedURL,
	})
}

func validCalendarEmail(email CalendarEmail) bool {
	link, err := url.Parse(email.AuthenticatedURL)
	return err == nil && link.RawQuery == "" && link.Fragment == "" && link.User == nil &&
		strings.TrimSpace(email.EventID) != "" && strings.TrimSpace(email.DeduplicationKey) != "" &&
		ValidEmailRecipient(email.Recipient) && validAuthenticatedURL(email.AuthenticatedURL) &&
		boundedHeader(email.Title, maxEmailSubjectBytes) != "" && strings.TrimSpace(email.Body) != ""
}

// ValidEmailRecipient reports whether value is a single transport-safe mailbox.
func ValidEmailRecipient(value string) bool {
	if strings.ContainsAny(value, "\r\n") || len(value) > 254 {
		return false
	}
	address, err := mail.ParseAddress(strings.TrimSpace(value))
	return err == nil && address.Address == strings.TrimSpace(value)
}

func validAuthenticatedURL(value string) bool {
	parsed, err := url.Parse(value)
	return err == nil && parsed.IsAbs() && parsed.Scheme == "https" && parsed.Host != "" && parsed.User == nil && parsed.Fragment == ""
}

func boundedHeader(value string, maximum int) string {
	value = strings.Join(strings.Fields(strings.ReplaceAll(strings.ReplaceAll(value, "\r", " "), "\n", " ")), " ")
	if len(value) <= maximum {
		return value
	}
	for len(value) > maximum || !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return strings.TrimSpace(value)
}

type SMTPConfig struct {
	Host                     string
	Port                     int
	Username, Password, From string
}
type SMTPTransport struct{ config SMTPConfig }

func NewSMTPTransport(config SMTPConfig) (*SMTPTransport, error) {
	if strings.TrimSpace(config.Host) == "" || config.Port < 1 || config.Port > 65535 || !ValidEmailRecipient(config.From) || strings.TrimSpace(config.Username) == "" || config.Password == "" {
		return nil, ErrEmailDeliveryFailed
	}
	return &SMTPTransport{config: config}, nil
}

func (t *SMTPTransport) Send(ctx context.Context, request EmailRequest) error {
	if t == nil || !ValidEmailRecipient(request.Recipient) || strings.ContainsAny(request.Subject, "\r\n") || !validAuthenticatedURL(request.AuthenticatedURL) {
		return ErrEmailDeliveryFailed
	}
	address := net.JoinHostPort(t.config.Host, strconv.Itoa(t.config.Port))
	dialer := net.Dialer{}
	connection, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return ErrEmailDeliveryFailed
	}
	client, err := smtp.NewClient(connection, t.config.Host)
	if err != nil {
		_ = connection.Close()
		return ErrEmailDeliveryFailed
	}
	defer client.Close()
	if ok, _ := client.Extension("STARTTLS"); !ok {
		return ErrEmailDeliveryFailed
	}
	if err := client.StartTLS(&tls.Config{ServerName: t.config.Host, MinVersion: tls.VersionTLS12}); err != nil {
		return ErrEmailDeliveryFailed
	}
	if err := client.Auth(smtp.PlainAuth("", t.config.Username, t.config.Password, t.config.Host)); err != nil {
		return ErrEmailDeliveryFailed
	}
	if err := client.Mail(t.config.From); err != nil {
		return ErrEmailDeliveryFailed
	}
	if err := client.Rcpt(request.Recipient); err != nil {
		return ErrEmailDeliveryFailed
	}
	w, err := client.Data()
	if err != nil {
		return ErrEmailDeliveryFailed
	}
	body := request.Body
	if body == "" {
		body = "You have a new internal mention.\r\n\r\n" + request.Subject
	}
	body += "\r\n" + request.AuthenticatedURL + "\r\n"
	message := "From: " + t.config.From + "\r\nTo: " + request.Recipient + "\r\nSubject: " + request.Subject + "\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n" + body
	if _, err = io.WriteString(w, message); err != nil {
		_ = w.Close()
		return ErrEmailDeliveryFailed
	}
	if err = w.Close(); err != nil {
		return ErrEmailDeliveryFailed
	}
	if err = client.Quit(); err != nil {
		return ErrEmailDeliveryFailed
	}
	return nil
}
