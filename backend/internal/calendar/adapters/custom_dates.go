package adapters

import (
	"context"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/calendar"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/customfields"
)

type CustomDateSource interface {
	LoadCustomDate(context.Context, calendar.SourceRef) (customfields.DateDefinition, customfields.DateValue, error)
}
type CustomCapacitySource interface {
	LoadCustomCapacity(context.Context, customfields.DateValue) (string, int64, error)
}
type CustomDateAdapter struct {
	source CustomDateSource
	roles  map[string]calendar.EventRoleDefinition
}

func NewCustomDateAdapter(s CustomDateSource, r map[string]calendar.EventRoleDefinition) *CustomDateAdapter {
	return &CustomDateAdapter{s, r}
}
func (*CustomDateAdapter) SourceType() string { return "custom_date" }
func (a *CustomDateAdapter) Project(ctx context.Context, ref calendar.SourceRef) ([]calendar.Projection, error) {
	if a == nil || a.source == nil || ref.Type != "custom_date" {
		return nil, calendar.ErrInvalidAdapter
	}
	d, v, e := a.source.LoadCustomDate(ctx, ref)
	if e != nil {
		return nil, e
	}
	roles := a.roles
	if source, ok := a.source.(calendar.CustomRoleDefinitionSource); ok {
		roles, e = source.LoadCustomRoleDefinitions(ctx, ref.MSPID)
		if e != nil {
			return nil, e
		}
	}
	role, ok := roles[d.ID]
	if !ok || d.LifecycleState != "active" || v.ID != ref.ID || v.MSPID != ref.MSPID || v.ClientID != ref.ClientID || v.SourceRevision < 1 || v.FieldID != d.ID {
		return nil, calendar.ErrInvalidAdapter
	}
	revision, e := resolveProjectionRevision(ctx, a.source, ref, v.SourceRevision)
	if e != nil {
		return nil, e
	}
	if revision < 1 {
		return nil, calendar.ErrInvalidAdapter
	}
	var p calendar.Projection
	if v.DateValue != nil {
		p = allDayProjection(ref, revision, d.Label, role.Role, *v.DateValue)
	} else {
		p = timedProjection(ref, revision, d.Label, role.Role, v.TimestampValue, nil, v.Timezone, role.SchedulingMode, nil)
	}
	p.ID = projectionID(ref, role.Role, role.SourceRoleKey)
	p.SourceRoleKey = role.SourceRoleKey
	if role.CapacityBearing {
		if resolver, ok := a.source.(CustomCapacitySource); ok {
			assignee, minutes, capacityErr := resolver.LoadCustomCapacity(ctx, v)
			if capacityErr != nil {
				return nil, capacityErr
			}
			p.AssigneeID = assignee
			p.PlannedMinutes = minutes
			p.CapacityBearing = assignee != "" && minutes > 0
		}
	}
	p.Dimensions = calendar.FilterDimensions{ClientIDs: compact(v.ClientID)}
	return []calendar.Projection{p}, nil
}

type Dependencies struct {
	Work        WorkSource
	Projects    ProjectSource
	Workforce   WorkforceSource
	Commitments CommitmentSource
	CustomDates CustomDateSource
	CustomRoles map[string]calendar.EventRoleDefinition
}

func NewProductionAdapterRegistry(d Dependencies) (*calendar.AdapterRegistry, error) {
	r := calendar.NewAdapterRegistry()
	for _, a := range []calendar.SourceAdapter{NewWorkRecordAdapter(d.Work), NewTaskAdapter(d.Work), NewProjectAdapter(d.Projects), NewPhaseAdapter(d.Projects), NewMilestoneAdapter(d.Projects), NewResourcePlanAdapter(d.Projects), NewScheduleAdapter(d.Workforce), NewPTOAdapter(d.Workforce), NewMaintenanceAdapter(d.Commitments), NewCommercialAdapter(d.Commitments), NewCustomDateAdapter(d.CustomDates, d.CustomRoles)} {
		if e := r.Register(a); e != nil {
			return nil, e
		}
	}
	return r, nil
}
