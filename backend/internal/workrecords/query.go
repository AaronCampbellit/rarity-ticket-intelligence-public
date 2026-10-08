package workrecords

import (
	"context"
	"strings"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

const defaultWorklistLimit = 50
const maxWorklistLimit = 100

type QueryRepository interface {
	Find(context.Context, scope.Target, string) (Record, error)
	List(context.Context, ListFilter) ([]Record, error)
}

type GetCommand struct {
	Principal authorization.Principal
	Target    scope.Target
	ID        string
}

type ListCommand struct {
	Principal                 authorization.Principal
	Target                    scope.Target
	Status                    string
	Text, Priority, Ownership string
	QueueID                   string
	PrimaryOwnerID            string
	BeforeUpdatedAt           time.Time
	BeforeID                  string
	Limit                     int
}

type ListFilter struct {
	Target                    scope.Target
	Status                    string
	Text, Priority, Ownership string
	QueueID                   string
	PrimaryOwnerID            string
	BeforeUpdatedAt           time.Time
	BeforeID                  string
	Limit                     int
}

type QueryService struct {
	repository QueryRepository
}

func NewQueryService(repository QueryRepository) *QueryService {
	return &QueryService{repository: repository}
}

func (s *QueryService) Get(
	ctx context.Context,
	command GetCommand,
) (Record, error) {
	target, err := authorizeRead(command.Principal, command.Target)
	if err != nil {
		return Record{}, err
	}
	id := strings.TrimSpace(command.ID)
	if id == "" {
		return Record{}, ErrInvalid
	}
	return s.repository.Find(ctx, target, id)
}

func (s *QueryService) List(
	ctx context.Context,
	command ListCommand,
) ([]Record, error) {
	target, err := authorizeRead(command.Principal, command.Target)
	if err != nil {
		return nil, err
	}
	limit := command.Limit
	if limit == 0 {
		limit = defaultWorklistLimit
	}
	beforeID := strings.TrimSpace(command.BeforeID)
	text := strings.TrimSpace(command.Text)
	ownership := strings.TrimSpace(command.Ownership)
	if len(text) > 500 || (ownership != "" && ownership != "all" && ownership != "assigned" && ownership != "unassigned") {
		return nil, ErrInvalid
	}
	if limit < 1 || limit > maxWorklistLimit ||
		command.BeforeUpdatedAt.IsZero() != (beforeID == "") {
		return nil, ErrInvalid
	}
	return s.repository.List(ctx, ListFilter{
		Target: target, Status: strings.TrimSpace(command.Status),
		Text: text, Priority: strings.TrimSpace(command.Priority), Ownership: ownership,
		QueueID:         strings.TrimSpace(command.QueueID),
		PrimaryOwnerID:  strings.TrimSpace(command.PrimaryOwnerID),
		BeforeUpdatedAt: command.BeforeUpdatedAt.UTC(),
		BeforeID:        beforeID,
		Limit:           limit,
	})
}

func authorizeRead(
	principal authorization.Principal,
	requested scope.Target,
) (scope.Target, error) {
	target := requested
	if target.MSPID == "" {
		target = scope.Target{
			MSPID: principal.Scope.MSPID, ClientID: principal.Scope.ClientID,
		}
	}
	if target.ClientID == "" {
		return scope.Target{}, ErrInvalid
	}
	if err := authorization.Authorize(
		principal,
		"work_record.read",
		target,
	); err != nil {
		return scope.Target{}, err
	}
	return target, nil
}
