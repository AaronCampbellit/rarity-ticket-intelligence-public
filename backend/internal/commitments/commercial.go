package commitments

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/calendar"
	internalid "github.com/rarity-ticket-intelligence/rarity/backend/internal/id"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mutation"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

type CommercialType string

const (
	Renewal CommercialType = "renewal"
	License CommercialType = "license"
)

var (
	ErrInvalidCommercial           = errors.New("invalid commercial commitment")
	ErrInvalidCommercialTransition = errors.New("invalid commercial transition")
	currencyPattern                = regexp.MustCompile(`^[A-Z]{3}$`)
)

// QuantityUnits stores quantity in ten-thousandths. For example, 1.2500 is 12500.
const CommercialQuantityScale int64 = 10_000
const maxCommercialNumericUnits int64 = 999_999_999_999_999_999

type CommercialLinks struct{ ServiceID, AssetID, ContractID string }

type CommercialCommitment struct {
	ID, MSPID, ClientID, Title, Description, Vendor, OwnerID, ExternalReference, Currency, Status, CreatedBy, UpdatedBy string
	Type                                                                                                                CommercialType
	EffectiveOn, NoticeOn, RenewalOn, ExpirationOn                                                                      time.Time
	QuantityUnits                                                                                                       int64
	CostMinor                                                                                                           int64
	HasCost                                                                                                             bool
	Recurrence                                                                                                          *calendar.RecurrenceRule
	CommercialLinks
	Version              int64
	CreatedAt, UpdatedAt time.Time
}

type CreateCommercialCommand struct {
	Principal                                                                                         authorization.Principal
	ClientID                                                                                          string
	Type                                                                                              CommercialType
	Title, Description, Vendor, OwnerID, ExternalReference, Currency, ActorID, Source, IdempotencyKey string
	EffectiveOn, NoticeOn, RenewalOn, ExpirationOn                                                    time.Time
	QuantityUnits                                                                                     int64
	CostMinor                                                                                         int64
	HasCost                                                                                           bool
	Recurrence                                                                                        *calendar.RecurrenceRule
	ServiceID, AssetID, ContractID                                                                    string
}

type UpdateCommercialCommand struct {
	Principal                                                                                         authorization.Principal
	CommitmentID, ClientID                                                                            string
	ExpectedVersion                                                                                   int64
	Type                                                                                              CommercialType
	Title, Description, Vendor, OwnerID, ExternalReference, Currency, ActorID, Source, IdempotencyKey string
	EffectiveOn, NoticeOn, RenewalOn, ExpirationOn                                                    time.Time
	QuantityUnits                                                                                     int64
	CostMinor                                                                                         int64
	HasCost                                                                                           bool
	Recurrence                                                                                        *calendar.RecurrenceRule
	ServiceID, AssetID, ContractID                                                                    string
}

type TransitionCommercialCommand struct {
	Principal                                                         authorization.Principal
	CommitmentID, ClientID, ToStatus, ActorID, Source, IdempotencyKey string
	ExpectedVersion                                                   int64
}

type CommercialMutation struct {
	Commitment                                    CommercialCommitment
	Audit                                         mutation.AuditRecord
	Event                                         mutation.EventRecord
	IdempotencyKey, RequestFingerprint, RequestID string
}

type CommercialRepository interface {
	CommercialRelationshipsBelongToClient(context.Context, scope.Target, CommercialLinks) (bool, error)
	FindCommercial(context.Context, scope.Target, string) (CommercialCommitment, error)
	CreateCommercialAtomic(context.Context, CommercialMutation) error
	UpdateCommercialAtomic(context.Context, CommercialMutation, int64) error
}

type CommercialService struct {
	repository CommercialRepository
	now        func() time.Time
	newID      func() string
}

func NewCommercialService(r CommercialRepository, n func() time.Time, i func() string) *CommercialService {
	return &CommercialService{r, n, i}
}

func (s *CommercialService) Create(ctx context.Context, c CreateCommercialCommand) (CommercialCommitment, error) {
	actor := c.Principal.ID
	if s == nil || s.repository == nil || !validCommitmentPrincipal(c.Principal) || !internalid.ValidCanonical(c.ClientID) || !validCommercialType(c.Type) ||
		strings.TrimSpace(c.Title) == "" || strings.TrimSpace(c.Vendor) == "" || c.OwnerID == "" ||
		c.EffectiveOn.IsZero() || c.ExpirationOn.IsZero() || c.ExpirationOn.Before(c.EffectiveOn) ||
		c.QuantityUnits < 0 || c.QuantityUnits > maxCommercialNumericUnits || c.CostMinor < 0 || c.CostMinor > maxCommercialNumericUnits || actor == "" || (c.ActorID != "" && c.ActorID != actor) ||
		c.Source == "" || !mutation.ValidIdempotencyKey(c.IdempotencyKey) || !validCommercialMoney(c.HasCost, c.Currency) ||
		!internalid.ValidCanonical(c.OwnerID) || !validCommercialLinks(CommercialLinks{c.ServiceID, c.AssetID, c.ContractID}) || !validOptionalCommercialDates(c) || (c.Recurrence != nil && c.Recurrence.Validate() != nil) {
		return CommercialCommitment{}, ErrInvalidCommercial
	}
	target := scope.Target{MSPID: c.Principal.Scope.MSPID, ClientID: c.ClientID}
	if err := authorization.Authorize(c.Principal, "calendar.commitment.manage", target); err != nil {
		return CommercialCommitment{}, err
	}
	links := CommercialLinks{c.ServiceID, c.AssetID, c.ContractID}
	if err := s.validateLinks(ctx, target, links); err != nil {
		return CommercialCommitment{}, err
	}

	now := s.now().UTC()
	found := CommercialCommitment{
		ID: s.newID(), MSPID: target.MSPID, ClientID: target.ClientID, Type: c.Type,
		Title: strings.TrimSpace(c.Title), Description: strings.TrimSpace(c.Description),
		Vendor: strings.TrimSpace(c.Vendor), OwnerID: c.OwnerID,
		ExternalReference: strings.TrimSpace(c.ExternalReference), Currency: c.Currency, Status: "active",
		EffectiveOn: day(c.EffectiveOn), NoticeOn: optionalDay(c.NoticeOn), RenewalOn: optionalDay(c.RenewalOn),
		ExpirationOn: day(c.ExpirationOn), QuantityUnits: c.QuantityUnits, CostMinor: c.CostMinor,
		HasCost: c.HasCost, Recurrence: c.Recurrence, CommercialLinks: links, Version: 1,
		CreatedAt: now, UpdatedAt: now, CreatedBy: actor, UpdatedBy: actor,
	}
	fingerprint, err := mutation.Fingerprint(struct {
		ClientID                                                         string
		Type                                                             CommercialType
		Title, Description, Vendor, OwnerID, ExternalReference, Currency string
		EffectiveOn, NoticeOn, RenewalOn, ExpirationOn                   time.Time
		QuantityUnits, CostMinor                                         int64
		HasCost                                                          bool
		Recurrence                                                       *calendar.RecurrenceRule
		Links                                                            CommercialLinks
	}{found.ClientID, found.Type, found.Title, found.Description, found.Vendor, found.OwnerID, found.ExternalReference, found.Currency, found.EffectiveOn, found.NoticeOn, found.RenewalOn, found.ExpirationOn, found.QuantityUnits, found.CostMinor, found.HasCost, found.Recurrence, links})
	if err != nil {
		return CommercialCommitment{}, err
	}
	mutationRecord := s.commercialMutation(found, actor, c.Source, "created", c.IdempotencyKey, fingerprint)
	if err = s.repository.CreateCommercialAtomic(ctx, mutationRecord); err != nil {
		if replay, decodeErr := mutation.DecodeReplay(err, &found); replay {
			return found, decodeErr
		}
		return CommercialCommitment{}, err
	}
	return found, nil
}

func (s *CommercialService) Update(ctx context.Context, c UpdateCommercialCommand) (CommercialCommitment, error) {
	actor := c.Principal.ID
	if s == nil || s.repository == nil || !validCommitmentPrincipal(c.Principal) || !internalid.ValidCanonical(c.CommitmentID) || c.ExpectedVersion < 1 || !internalid.ValidCanonical(c.ClientID) ||
		strings.TrimSpace(c.Title) == "" || strings.TrimSpace(c.Vendor) == "" || c.OwnerID == "" ||
		c.EffectiveOn.IsZero() || c.ExpirationOn.IsZero() || c.ExpirationOn.Before(c.EffectiveOn) ||
		c.QuantityUnits < 0 || c.QuantityUnits > maxCommercialNumericUnits || c.CostMinor < 0 || c.CostMinor > maxCommercialNumericUnits || actor == "" || (c.ActorID != "" && c.ActorID != actor) ||
		c.Source == "" || !mutation.ValidIdempotencyKey(c.IdempotencyKey) || !validCommercialMoney(c.HasCost, c.Currency) ||
		!internalid.ValidCanonical(c.OwnerID) || !validCommercialLinks(CommercialLinks{c.ServiceID, c.AssetID, c.ContractID}) || !validOptionalCommercialDates(c) || (c.Recurrence != nil && c.Recurrence.Validate() != nil) {
		return CommercialCommitment{}, ErrInvalidCommercial
	}
	target := scope.Target{MSPID: c.Principal.Scope.MSPID, ClientID: c.ClientID}
	if err := authorization.Authorize(c.Principal, "calendar.commitment.manage", target); err != nil {
		return CommercialCommitment{}, err
	}
	current, err := s.repository.FindCommercial(ctx, target, c.CommitmentID)
	if err != nil {
		return CommercialCommitment{}, err
	}
	if c.Type != "" && c.Type != current.Type {
		return CommercialCommitment{}, ErrInvalidCommercial
	}
	links := CommercialLinks{c.ServiceID, c.AssetID, c.ContractID}
	if err = s.validateLinks(ctx, target, links); err != nil {
		return CommercialCommitment{}, err
	}

	found := current
	found.Title = strings.TrimSpace(c.Title)
	found.Description = strings.TrimSpace(c.Description)
	found.Vendor = strings.TrimSpace(c.Vendor)
	found.OwnerID = c.OwnerID
	found.ExternalReference = strings.TrimSpace(c.ExternalReference)
	found.Currency = c.Currency
	found.EffectiveOn = day(c.EffectiveOn)
	found.NoticeOn = optionalDay(c.NoticeOn)
	found.RenewalOn = optionalDay(c.RenewalOn)
	found.ExpirationOn = day(c.ExpirationOn)
	found.QuantityUnits = c.QuantityUnits
	found.CostMinor = c.CostMinor
	found.HasCost = c.HasCost
	found.Recurrence = c.Recurrence
	found.CommercialLinks = links
	found.Version = c.ExpectedVersion + 1
	found.UpdatedAt = s.now().UTC()
	found.UpdatedBy = actor
	fingerprint, err := mutation.Fingerprint(struct {
		ID              string
		ExpectedVersion int64
		Value           CommercialCommitment
	}{found.ID, c.ExpectedVersion, found})
	if err != nil {
		return CommercialCommitment{}, err
	}
	mutationRecord := s.commercialMutation(found, actor, c.Source, "updated", c.IdempotencyKey, fingerprint)
	if err = s.repository.UpdateCommercialAtomic(ctx, mutationRecord, c.ExpectedVersion); err != nil {
		if replay, decodeErr := mutation.DecodeReplay(err, &found); replay {
			return found, decodeErr
		}
		return CommercialCommitment{}, err
	}
	return found, nil
}

func (s *CommercialService) Transition(ctx context.Context, c TransitionCommercialCommand) (CommercialCommitment, error) {
	actor := c.Principal.ID
	if s == nil || s.repository == nil || !validCommitmentPrincipal(c.Principal) || !internalid.ValidCanonical(c.CommitmentID) || !internalid.ValidCanonical(c.ClientID) || c.ExpectedVersion < 1 ||
		actor == "" || (c.ActorID != "" && c.ActorID != actor) || c.Source == "" || !mutation.ValidIdempotencyKey(c.IdempotencyKey) {
		return CommercialCommitment{}, ErrInvalidCommercial
	}
	target := scope.Target{MSPID: c.Principal.Scope.MSPID, ClientID: c.ClientID}
	if err := authorization.Authorize(c.Principal, "calendar.commitment.manage", target); err != nil {
		return CommercialCommitment{}, err
	}
	found, err := s.repository.FindCommercial(ctx, target, c.CommitmentID)
	if err != nil {
		return CommercialCommitment{}, err
	}
	if found.Version == c.ExpectedVersion && !validCommercialTransition(found.Status, c.ToStatus) {
		return CommercialCommitment{}, ErrInvalidCommercialTransition
	}
	found.Status = c.ToStatus
	found.Version = c.ExpectedVersion + 1
	found.UpdatedAt = s.now().UTC()
	found.UpdatedBy = actor
	fingerprint, err := mutation.Fingerprint(struct {
		ID, ToStatus    string
		ExpectedVersion int64
	}{found.ID, c.ToStatus, c.ExpectedVersion})
	if err != nil {
		return CommercialCommitment{}, err
	}
	mutationRecord := s.commercialMutation(found, actor, c.Source, "transitioned", c.IdempotencyKey, fingerprint)
	if err = s.repository.UpdateCommercialAtomic(ctx, mutationRecord, c.ExpectedVersion); err != nil {
		if replay, decodeErr := mutation.DecodeReplay(err, &found); replay {
			return found, decodeErr
		}
		return CommercialCommitment{}, err
	}
	return found, nil
}

func (s *CommercialService) validateLinks(ctx context.Context, target scope.Target, links CommercialLinks) error {
	ok, err := s.repository.CommercialRelationshipsBelongToClient(ctx, target, links)
	if err != nil {
		return err
	}
	if !ok {
		return scope.ErrNotFound
	}
	return nil
}

func (s *CommercialService) commercialMutation(found CommercialCommitment, actor, source, action, idempotencyKey, fingerprint string) CommercialMutation {
	auditID, eventID, correlationID := s.newID(), s.newID(), s.newID()
	verb := "commercial.commitment." + action
	return CommercialMutation{
		Commitment:     found,
		Audit:          mutation.AuditRecord{ID: auditID, OccurredAt: found.UpdatedAt, MSPID: found.MSPID, ClientID: found.ClientID, ActorType: "technician", ActorID: actor, Action: verb, SubjectType: "commercial_commitment", SubjectID: found.ID, SubjectVersion: found.Version, Source: source, CorrelationID: correlationID},
		Event:          mutation.EventRecord{EventID: eventID, EventType: verb, SchemaVersion: 1, OccurredAt: found.UpdatedAt, MSPID: found.MSPID, ClientID: found.ClientID, ActorType: "technician", ActorID: actor, SubjectType: "commercial_commitment", SubjectID: found.ID, SubjectVersion: found.Version, Source: source, CorrelationID: correlationID, Data: map[string]any{"commitment_type": found.Type, "status": found.Status}},
		IdempotencyKey: idempotencyKey, RequestFingerprint: fingerprint, RequestID: s.newID(),
	}
}

type optionalCommercialDates interface {
	commercialDates() (time.Time, time.Time, time.Time, time.Time)
}

func (c CreateCommercialCommand) commercialDates() (time.Time, time.Time, time.Time, time.Time) {
	return c.EffectiveOn, c.NoticeOn, c.RenewalOn, c.ExpirationOn
}
func (c UpdateCommercialCommand) commercialDates() (time.Time, time.Time, time.Time, time.Time) {
	return c.EffectiveOn, c.NoticeOn, c.RenewalOn, c.ExpirationOn
}
func validOptionalCommercialDates(c optionalCommercialDates) bool {
	effective, notice, renewal, expiration := c.commercialDates()
	return (notice.IsZero() || (!notice.Before(effective) && !notice.After(expiration))) &&
		(renewal.IsZero() || (!renewal.Before(effective) && !renewal.After(expiration)))
}
func validCommercialMoney(hasCost bool, currency string) bool {
	return hasCost && currencyPattern.MatchString(currency) || !hasCost && currency == ""
}
func validCommercialType(value CommercialType) bool { return value == Renewal || value == License }
func validCommercialLinks(links CommercialLinks) bool {
	return (links.ServiceID == "" || internalid.ValidCanonical(links.ServiceID)) &&
		(links.AssetID == "" || internalid.ValidCanonical(links.AssetID)) &&
		(links.ContractID == "" || internalid.ValidCanonical(links.ContractID))
}
func validCommercialTransition(from, to string) bool {
	return from == "active" && (to == "renewed" || to == "expired" || to == "cancelled")
}
func day(v time.Time) time.Time {
	y, m, d := v.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}
func optionalDay(v time.Time) time.Time {
	if v.IsZero() {
		return v
	}
	return day(v)
}
