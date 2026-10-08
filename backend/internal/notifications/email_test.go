package notifications

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"testing"
)

type emailTransport struct {
	request EmailRequest
	err     error
}

func (t *emailTransport) Send(_ context.Context, request EmailRequest) error {
	t.request = request
	return t.err
}

func TestEmailServiceSendsBoundedHeaderSafeMentionTemplate(t *testing.T) {
	transport := &emailTransport{}
	service := NewEmailService(transport)
	err := service.Deliver(context.Background(), MentionEmail{
		EventID: "event", Recipient: "tech@example.test", AuthorLabel: "Ada\r\nBcc: attacker@example.test",
		ObjectDisplayID: "INC-42", ObjectSubject: strings.Repeat("Service unavailable ", 40),
		AuthenticatedURL: "https://rarity.example/mentions?occurrence=occurrence",
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.ContainsAny(transport.request.Subject, "\r\n") || len(transport.request.Subject) > maxEmailSubjectBytes || strings.Contains(transport.request.Subject, "internal body") {
		t.Fatalf("unsafe subject=%q", transport.request.Subject)
	}
	if transport.request.AuthenticatedURL == "" || transport.request.Recipient != "tech@example.test" {
		t.Fatalf("request=%+v", transport.request)
	}
}

func TestEmailServiceRejectsNonHTTPSLinksAndInvalidRecipients(t *testing.T) {
	service := NewEmailService(&emailTransport{})
	for _, email := range []MentionEmail{
		{EventID: "event", Recipient: "bad\r\nBcc:x", AuthorLabel: "Ada", ObjectDisplayID: "INC-1", ObjectSubject: "Safe", AuthenticatedURL: "https://rarity.example/x"},
		{EventID: "event", Recipient: "tech@example.test", AuthorLabel: "Ada", ObjectDisplayID: "INC-1", ObjectSubject: "Safe", AuthenticatedURL: "http://rarity.example/x"},
	} {
		if service.Deliver(context.Background(), email) == nil {
			t.Fatalf("accepted unsafe email=%+v", email)
		}
	}
}

func TestCalendarEmailUsesAuthorizedCopyAndDeliveryDeduplicationKey(t *testing.T) {
	transport := &emailTransport{}
	err := NewEmailService(transport).DeliverCalendar(context.Background(), CalendarEmail{
		EventID: "event", DeduplicationKey: "calendar-dedupe", Recipient: "tech@example.test",
		Title: "Calendar schedule changed", Body: "Contoso: Install router",
		AuthenticatedURL: "https://rarity.example/calendar",
	})
	if err != nil {
		t.Fatal(err)
	}
	if transport.request.Subject != "Calendar schedule changed" || transport.request.Body != "Contoso: Install router" || transport.request.IdempotencyKey != "calendar-dedupe" {
		t.Fatalf("request=%+v", transport.request)
	}
}

func TestCalendarEmailRejectsUnsafeOrIncompleteRenderedDelivery(t *testing.T) {
	service := NewEmailService(&emailTransport{})
	for _, email := range []CalendarEmail{
		{EventID: "event", DeduplicationKey: "dedupe", Recipient: "tech@example.test", Title: "Calendar changed", Body: "safe", AuthenticatedURL: "http://rarity.example/calendar"},
		{EventID: "event", DeduplicationKey: "dedupe", Recipient: "tech@example.test", Title: "Calendar changed", Body: "safe", AuthenticatedURL: "https://rarity.example/calendar?private=1"},
		{EventID: "event", DeduplicationKey: "dedupe", Recipient: "tech@example.test", Title: "Calendar changed", Body: "safe", AuthenticatedURL: "https://rarity.example/calendar#private"},
		{EventID: "event", Recipient: "tech@example.test", Title: "Calendar changed", Body: "safe", AuthenticatedURL: "https://rarity.example/calendar"},
		{EventID: "event", DeduplicationKey: "dedupe", Recipient: "bad\r\nBcc:x", Title: "Calendar changed", Body: "safe", AuthenticatedURL: "https://rarity.example/calendar"},
	} {
		if service.DeliverCalendar(context.Background(), email) == nil {
			t.Fatalf("accepted unsafe calendar email=%+v", email)
		}
	}
}

func TestSMTPTransportRefusesServerWithoutSTARTTLS(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	done := make(chan error, 1)
	go func() {
		connection, acceptErr := listener.Accept()
		if acceptErr != nil {
			done <- acceptErr
			return
		}
		defer connection.Close()
		reader := bufio.NewReader(connection)
		if _, writeErr := fmt.Fprint(connection, "220 smtp.example.test ESMTP\r\n"); writeErr != nil {
			done <- writeErr
			return
		}
		if _, readErr := reader.ReadString('\n'); readErr != nil {
			done <- readErr
			return
		}
		_, writeErr := fmt.Fprint(connection, "250-smtp.example.test\r\n250 AUTH PLAIN\r\n")
		done <- writeErr
	}()
	address := listener.Addr().(*net.TCPAddr)
	transport, err := NewSMTPTransport(SMTPConfig{Host: "127.0.0.1", Port: address.Port, Username: "mailer", Password: "secret", From: "rarity@example.test"})
	if err != nil {
		t.Fatal(err)
	}
	err = transport.Send(context.Background(), EmailRequest{EventID: "event", Recipient: "tech@example.test", Subject: "Ada mentioned you on INC-1: VPN", AuthenticatedURL: "https://rarity.example/mentions"})
	if !errors.Is(err, ErrEmailDeliveryFailed) {
		t.Fatalf("plaintext SMTP accepted: %v", err)
	}
	if serverErr := <-done; serverErr != nil {
		t.Fatal(serverErr)
	}
}
