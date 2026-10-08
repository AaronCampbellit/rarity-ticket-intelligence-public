package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type bootstrapIssuerStub struct {
	token     string
	expiresAt time.Time
	err       error
	calls     int
}

func (s *bootstrapIssuerStub) IssueToken(context.Context) (string, time.Time, error) {
	s.calls++
	return s.token, s.expiresAt, s.err
}

func TestIssueFirstRunSetupPrintsRemoteURLOnce(t *testing.T) {
	output := new(bytes.Buffer)
	issuer := &bootstrapIssuerStub{
		token:     "bootstrap-secret",
		expiresAt: time.Date(2026, 8, 3, 16, 15, 0, 0, time.UTC),
	}

	err := issueFirstRunSetup(
		context.Background(),
		false,
		"https://rarity.example/support/rti",
		issuer,
		output,
	)
	if err != nil {
		t.Fatal(err)
	}
	if issuer.calls != 1 {
		t.Fatalf("issuer calls=%d want=1", issuer.calls)
	}
	for _, fragment := range []string{
		"FIRST-RUN SETUP AUTHORITY",
		"https://rarity.example/support/rti/#/setup?bootstrap_token=bootstrap-secret",
		"2026-08-03T16:15:00Z",
	} {
		if !strings.Contains(output.String(), fragment) {
			t.Fatalf("output %q missing %q", output.String(), fragment)
		}
	}
	if strings.Count(output.String(), "bootstrap-secret") != 1 {
		t.Fatalf("token printed more than once: %q", output.String())
	}
}

func TestIssueFirstRunSetupDoesNothingAfterCompletion(t *testing.T) {
	output := new(bytes.Buffer)
	issuer := &bootstrapIssuerStub{err: errors.New("must not be called")}

	if err := issueFirstRunSetup(
		context.Background(), true, "", issuer, output,
	); err != nil {
		t.Fatal(err)
	}
	if issuer.calls != 0 || output.Len() != 0 {
		t.Fatalf("calls=%d output=%q", issuer.calls, output.String())
	}
}

func TestIssueFirstRunSetupRequiresPublicURLBeforeIssuing(t *testing.T) {
	output := new(bytes.Buffer)
	issuer := &bootstrapIssuerStub{token: "must-not-be-issued"}

	err := issueFirstRunSetup(
		context.Background(), false, "", issuer, output,
	)
	if err == nil {
		t.Fatal("missing public URL was accepted")
	}
	if issuer.calls != 0 || output.Len() != 0 {
		t.Fatalf("calls=%d output=%q", issuer.calls, output.String())
	}
}

func TestIssueFirstRunSetupDoesNotPrintIssuerFailure(t *testing.T) {
	output := new(bytes.Buffer)
	issuer := &bootstrapIssuerStub{err: errors.New("database unavailable")}

	err := issueFirstRunSetup(
		context.Background(), false, "https://rarity.example", issuer, output,
	)
	if err == nil {
		t.Fatal("issuer failure was ignored")
	}
	if issuer.calls != 1 || output.Len() != 0 {
		t.Fatalf("calls=%d output=%q", issuer.calls, output.String())
	}
}
