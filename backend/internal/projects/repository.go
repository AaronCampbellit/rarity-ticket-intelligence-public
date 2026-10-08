package projects

import (
	"context"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mutation"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/tagging"
)

type CreateMutation struct {
	Project     Project
	Audit       mutation.AuditRecord
	Event       mutation.EventRecord
	InitialTags tagging.InitialAssignmentSet
}

type PhaseMutation struct {
	Phase Phase
	Audit mutation.AuditRecord
	Event mutation.EventRecord
}

type Repository interface {
	CreateAtomic(context.Context, CreateMutation) error
	FindProject(context.Context, scope.Target, ProjectID) (Project, error)
	FindPhase(context.Context, scope.Target, PhaseID) (Phase, error)
	UpdatePhaseAtomic(context.Context, PhaseMutation) error
}

type CapacityRepository interface {
	LoadCapacity(
		context.Context,
		scope.Target,
		CapacityWindow,
		[]string,
	) ([]ResourceCapacity, error)
}

type CapacityWindow struct {
	Start time.Time
	End   time.Time
}

type ResourceCapacity struct {
	ResourceID string
	Name       string
	Available  time.Duration
	Scheduled  time.Duration
	Actual     time.Duration
}
