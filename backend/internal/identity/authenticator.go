// Package identity implements the trusted boundary between verified workforce
// identity claims and Rarity technicians.
package identity

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mutation"
)

var (
	ErrUntrustedIdentity = errors.New("untrusted identity")
	ErrIdentityNotFound  = errors.New("identity not found")
	ErrJITDisabled       = errors.New("just-in-time provisioning disabled")
)

type Config struct {
	MSPID          string
	TenantID       string
	Issuer         string
	Audience       string
	DefaultRoleKey string
	JITEnabled     bool
}

type Claims struct {
	Issuer      string
	TenantID    string
	Audience    string
	Subject     string
	Email       string
	DisplayName string
}

type Technician struct {
	ID          string
	MSPID       string
	Email       string
	DisplayName string
}

type ProvisionCommand struct {
	MSPID              string
	TechnicianID       string
	ExternalIdentityID string
	RoleAssignmentID   string
	DefaultRoleKey     string
	Provider           string
	Issuer             string
	TenantID           string
	Subject            string
	Email              string
	DisplayName        string
	OccurredAt         time.Time
	Audit              mutation.AuditRecord
	Event              mutation.EventRecord
}

type TokenVerifier interface {
	// Verify performs signature, nonce/state, time-claim, and token-format
	// validation before returning claims.
	Verify(context.Context, string, string) (Claims, error)
}

type Repository interface {
	FindByExternalIdentity(context.Context, string, string, string) (Technician, error)
	ProvisionAtomic(context.Context, ProvisionCommand) (Technician, error)
}

type Authenticator struct {
	config     Config
	verifier   TokenVerifier
	repository Repository
	newID      func() string
	now        func() time.Time
}

func NewAuthenticator(
	config Config,
	verifier TokenVerifier,
	repository Repository,
	newID func() string,
	clocks ...func() time.Time,
) *Authenticator {
	now := time.Now
	if len(clocks) > 0 && clocks[0] != nil {
		now = clocks[0]
	}
	return &Authenticator{
		config: config, verifier: verifier, repository: repository,
		newID: newID, now: now,
	}
}

func (a *Authenticator) Authenticate(
	ctx context.Context,
	rawToken string,
	expectedNonce ...string,
) (Technician, error) {
	nonce := ""
	if len(expectedNonce) > 0 {
		nonce = expectedNonce[0]
	}
	claims, err := a.verifier.Verify(ctx, rawToken, nonce)
	if err != nil {
		return Technician{}, ErrUntrustedIdentity
	}
	if claims.Issuer != a.config.Issuer ||
		claims.TenantID != a.config.TenantID ||
		claims.Audience != a.config.Audience ||
		strings.TrimSpace(claims.Subject) == "" {
		return Technician{}, ErrUntrustedIdentity
	}

	technician, err := a.repository.FindByExternalIdentity(
		ctx, a.config.MSPID, claims.Issuer, claims.Subject,
	)
	if err == nil {
		return technician, nil
	}
	if !errors.Is(err, ErrIdentityNotFound) {
		return Technician{}, err
	}
	if !a.config.JITEnabled {
		return Technician{}, ErrJITDisabled
	}
	if strings.TrimSpace(claims.Email) == "" || a.newID == nil ||
		strings.TrimSpace(a.config.DefaultRoleKey) == "" {
		return Technician{}, ErrUntrustedIdentity
	}
	now := a.now().UTC()
	technicianID, identityID, assignmentID :=
		a.newID(), a.newID(), a.newID()
	auditID, eventID, correlationID := a.newID(), a.newID(), a.newID()
	return a.repository.ProvisionAtomic(ctx, ProvisionCommand{
		MSPID:              a.config.MSPID,
		TechnicianID:       technicianID,
		ExternalIdentityID: identityID,
		RoleAssignmentID:   assignmentID,
		DefaultRoleKey:     strings.TrimSpace(a.config.DefaultRoleKey),
		Provider:           "entra",
		Issuer:             claims.Issuer,
		TenantID:           claims.TenantID,
		Subject:            claims.Subject,
		Email:              strings.TrimSpace(claims.Email),
		DisplayName:        strings.TrimSpace(claims.DisplayName),
		OccurredAt:         now,
		Audit: mutation.AuditRecord{
			ID: auditID, OccurredAt: now, MSPID: a.config.MSPID,
			ActorType: "entra_identity", ActorID: technicianID,
			Action: "technician.jit_provisioned", SubjectType: "technician",
			SubjectID: technicianID, SubjectVersion: 1,
			Source: "entra", CorrelationID: correlationID,
		},
		Event: mutation.EventRecord{
			EventID: eventID, EventType: "technician.jit_provisioned",
			SchemaVersion: 1, OccurredAt: now, MSPID: a.config.MSPID,
			ActorType: "entra_identity", ActorID: technicianID,
			SubjectType: "technician", SubjectID: technicianID,
			SubjectVersion: 1, Source: "entra", CorrelationID: correlationID,
		},
	})
}
