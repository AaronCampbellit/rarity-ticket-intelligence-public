package projects

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mutation"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

var (
	ErrInvalidAvailability = errors.New("invalid technician availability")
	ErrAvailabilityOverlap = errors.New("technician availability overlaps an existing window")
)

type TechnicianAvailability struct {
	ID               string    `json:"id"`
	MSPID            string    `json:"msp_id"`
	TechnicianID     string    `json:"technician_id"`
	StartsAt         time.Time `json:"starts_at"`
	EndsAt           time.Time `json:"ends_at"`
	AvailableMinutes int64     `json:"available_minutes"`
	Version          int64     `json:"version"`
}

type CreateAvailabilityCommand struct {
	Principal        authorization.Principal
	TechnicianID     string
	StartsAt         time.Time
	EndsAt           time.Time
	AvailableMinutes int64
	ActorID          string
	Source           string
}

type AvailabilityMutation struct {
	Availability TechnicianAvailability
	Audit        mutation.AuditRecord
	Event        mutation.EventRecord
}

type AvailabilityRepository interface {
	CreateAvailabilityAtomic(context.Context, AvailabilityMutation) error
}

type AvailabilityService struct {
	repository AvailabilityRepository
	now        func() time.Time
	newID      func() string
}

func NewAvailabilityService(
	repository AvailabilityRepository,
	now func() time.Time,
	newID func() string,
) *AvailabilityService {
	return &AvailabilityService{repository: repository, now: now, newID: newID}
}

func (s *AvailabilityService) Create(
	ctx context.Context,
	command CreateAvailabilityCommand,
) (TechnicianAvailability, error) {
	target := scope.Target{MSPID: command.Principal.Scope.MSPID}
	technicianID := strings.TrimSpace(command.TechnicianID)
	actorID := strings.TrimSpace(command.ActorID)
	source := strings.TrimSpace(command.Source)
	windowMinutes := int64(command.EndsAt.Sub(command.StartsAt) / time.Minute)
	if target.MSPID == "" || technicianID == "" || actorID == "" || source == "" ||
		command.StartsAt.IsZero() || !command.EndsAt.After(command.StartsAt) ||
		command.AvailableMinutes < 0 || command.AvailableMinutes > windowMinutes {
		return TechnicianAvailability{}, ErrInvalidAvailability
	}
	if err := authorization.Authorize(
		command.Principal, "organization.manage", target,
	); err != nil {
		return TechnicianAvailability{}, err
	}
	availability := TechnicianAvailability{
		ID: s.newID(), MSPID: target.MSPID, TechnicianID: technicianID,
		StartsAt: command.StartsAt.UTC(), EndsAt: command.EndsAt.UTC(),
		AvailableMinutes: command.AvailableMinutes, Version: 1,
	}
	now := s.now().UTC()
	auditID, eventID, correlationID := s.newID(), s.newID(), s.newID()
	accepted := AvailabilityMutation{
		Availability: availability,
		Audit: mutation.AuditRecord{
			ID: auditID, OccurredAt: now, MSPID: target.MSPID,
			ActorType: "technician", ActorID: actorID,
			Action:      "technician.availability.created",
			SubjectType: "technician_availability",
			SubjectID:   availability.ID, SubjectVersion: availability.Version,
			Source: source, CorrelationID: correlationID,
		},
		Event: mutation.EventRecord{
			EventID: eventID, EventType: "technician.availability.created",
			SchemaVersion: 1, OccurredAt: now, MSPID: target.MSPID,
			ActorType: "technician", ActorID: actorID,
			SubjectType: "technician_availability",
			SubjectID:   availability.ID, SubjectVersion: availability.Version,
			Source: source, CorrelationID: correlationID,
		},
	}
	if err := s.repository.CreateAvailabilityAtomic(ctx, accepted); err != nil {
		return TechnicianAvailability{}, err
	}
	return availability, nil
}
