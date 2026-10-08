package clientresources

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

type lifecycleRepositoryStub struct {
	updates    []UpdateMutation
	lifecycles []LifecycleMutation
	preflights []LifecyclePreflightMutation
	method     string
	result     ResourceDetail
	preflight  LifecyclePreflight
	err        error
}

func (r *lifecycleRepositoryStub) CreateAtomic(context.Context, CreateMutation) error { return nil }
func (r *lifecycleRepositoryStub) Preflight(_ context.Context, value LifecyclePreflightMutation) (LifecyclePreflight, error) {
	r.preflights = append(r.preflights, value)
	return r.preflight, r.err
}

func (r *lifecycleRepositoryStub) recordUpdate(method string, mutation UpdateMutation) (ResourceDetail, error) {
	r.method = method
	r.updates = append(r.updates, mutation)
	return r.result, r.err
}

func (r *lifecycleRepositoryStub) recordLifecycle(method string, mutation LifecycleMutation) (ResourceDetail, error) {
	r.method = method
	r.lifecycles = append(r.lifecycles, mutation)
	return r.result, r.err
}

func (r *lifecycleRepositoryStub) UpdateLocation(_ context.Context, value UpdateMutation) (ResourceDetail, error) {
	return r.recordUpdate("update_location", value)
}
func (r *lifecycleRepositoryStub) UpdateContact(_ context.Context, value UpdateMutation) (ResourceDetail, error) {
	return r.recordUpdate("update_contact", value)
}
func (r *lifecycleRepositoryStub) UpdateAsset(_ context.Context, value UpdateMutation) (ResourceDetail, error) {
	return r.recordUpdate("update_asset", value)
}
func (r *lifecycleRepositoryStub) UpdateService(_ context.Context, value UpdateMutation) (ResourceDetail, error) {
	return r.recordUpdate("update_service", value)
}
func (r *lifecycleRepositoryStub) UpdateContract(_ context.Context, value UpdateMutation) (ResourceDetail, error) {
	return r.recordUpdate("update_contract", value)
}
func (r *lifecycleRepositoryStub) DeactivateLocation(_ context.Context, value LifecycleMutation) (ResourceDetail, error) {
	return r.recordLifecycle("deactivate_location", value)
}
func (r *lifecycleRepositoryStub) DeactivateContact(_ context.Context, value LifecycleMutation) (ResourceDetail, error) {
	return r.recordLifecycle("deactivate_contact", value)
}
func (r *lifecycleRepositoryStub) DeactivateAsset(_ context.Context, value LifecycleMutation) (ResourceDetail, error) {
	return r.recordLifecycle("deactivate_asset", value)
}
func (r *lifecycleRepositoryStub) DeactivateService(_ context.Context, value LifecycleMutation) (ResourceDetail, error) {
	return r.recordLifecycle("deactivate_service", value)
}
func (r *lifecycleRepositoryStub) DeactivateContract(_ context.Context, value LifecycleMutation) (ResourceDetail, error) {
	return r.recordLifecycle("deactivate_contract", value)
}
func (r *lifecycleRepositoryStub) ReactivateLocation(_ context.Context, value LifecycleMutation) (ResourceDetail, error) {
	return r.recordLifecycle("reactivate_location", value)
}
func (r *lifecycleRepositoryStub) ReactivateContact(_ context.Context, value LifecycleMutation) (ResourceDetail, error) {
	return r.recordLifecycle("reactivate_contact", value)
}
func (r *lifecycleRepositoryStub) ReactivateAsset(_ context.Context, value LifecycleMutation) (ResourceDetail, error) {
	return r.recordLifecycle("reactivate_asset", value)
}
func (r *lifecycleRepositoryStub) ReactivateService(_ context.Context, value LifecycleMutation) (ResourceDetail, error) {
	return r.recordLifecycle("reactivate_service", value)
}
func (r *lifecycleRepositoryStub) ReactivateContract(_ context.Context, value LifecycleMutation) (ResourceDetail, error) {
	return r.recordLifecycle("reactivate_contract", value)
}

func TestLifecyclePreflightUsesAuthorizedTypedReadOnlyBoundary(t *testing.T) {
	email := " new@example.com "
	repository := &lifecycleRepositoryStub{preflight: LifecyclePreflight{
		Resource:           ResourceDetail{Summary: Summary{ID: "contact-id", Kind: "contact", Version: 4}},
		RelationshipStatus: "active",
	}}
	result, err := testLifecycleService(repository).Preflight(context.Background(), LifecyclePreflightCommand{
		Principal: resourcePrincipal("contact.update"), Target: lifecycleTarget(),
		Kind: ContactKind, ResourceID: " contact-id ", ExpectedVersion: 3,
		Patch: UpdatePatch{Email: &email}, Operation: PreflightUpdate,
	})
	if err != nil || result.RelationshipStatus != "active" || len(repository.preflights) != 1 {
		t.Fatalf("Preflight() result=%+v error=%v repository=%+v", result, err, repository)
	}
	accepted := repository.preflights[0]
	if accepted.Kind != ContactKind || accepted.ResourceID != "contact-id" ||
		accepted.ExpectedVersion != 3 || accepted.Operation != PreflightUpdate ||
		accepted.Patch.Email == nil || *accepted.Patch.Email != "new@example.com" {
		t.Fatalf("typed preflight lost validation: %+v", accepted)
	}
}

func TestResourceUpdateBuildsVersionedReasonedMutationForExactKind(t *testing.T) {
	repository := &lifecycleRepositoryStub{result: ResourceDetail{
		Summary: Summary{ID: "location-id", Kind: "location", Name: "Branch", Version: 8, LifecycleState: "active"},
	}}
	at := time.Date(2026, time.August, 5, 12, 0, 0, 0, time.UTC)
	service := NewService(repository, func() time.Time { return at }, sequenceLifecycleIDs())
	name := "  Branch  "
	result, err := service.Update(context.Background(), UpdateCommand{
		Principal: resourcePrincipal("location.update"), Target: lifecycleTarget(),
		Kind: LocationKind, ResourceID: " location-id ", ExpectedVersion: 7,
		Patch: UpdatePatch{Name: &name}, Reason: "  office rename  ",
		ActorID: "actor-id", Source: "api", CorrelationID: "correlation-id",
	})
	if err != nil || result.Version != 8 {
		t.Fatalf("Update() result=%+v error=%v", result, err)
	}
	if repository.method != "update_location" || len(repository.updates) != 1 {
		t.Fatalf("Update() escaped typed Location repository: %+v", repository)
	}
	accepted := repository.updates[0]
	if accepted.Target != lifecycleTarget() || accepted.ResourceID != "location-id" ||
		accepted.ExpectedVersion != 7 || accepted.Patch.Name == nil || *accepted.Patch.Name != "Branch" ||
		accepted.Audit.Action != "location.updated" || accepted.Audit.Reason != "office rename" ||
		accepted.Audit.SubjectVersion != 8 || accepted.Event.EventType != "location.updated" ||
		accepted.Event.SubjectVersion != 8 || accepted.Audit.CorrelationID != "correlation-id" ||
		accepted.Event.CorrelationID != "correlation-id" || !accepted.UpdatedAt.Equal(at) {
		t.Fatalf("Update() lost mutation contract: %+v", accepted)
	}
}

func TestResourceUpdateRejectsInvalidOrWrongKindPatchesBeforeRepository(t *testing.T) {
	validName := "Name"
	badEmail := "not-an-email"
	badPhone := "call-me"
	badCriticality := "urgent"
	zeroDate := time.Time{}
	start := time.Date(2026, time.August, 8, 0, 0, 0, 0, time.UTC)
	end := start.Add(-24 * time.Hour)
	tests := []struct {
		name  string
		kind  Kind
		patch UpdatePatch
	}{
		{"no patch", LocationKind, UpdatePatch{}},
		{"wrong field", LocationKind, UpdatePatch{AssetType: &validName}},
		{"invalid email", ContactKind, UpdatePatch{Email: &badEmail}},
		{"invalid phone", ContactKind, UpdatePatch{Phone: &badPhone}},
		{"invalid criticality", ServiceKind, UpdatePatch{Criticality: &badCriticality}},
		{"zero start", ContractKind, UpdatePatch{StartsOn: &zeroDate}},
		{"inverted contract", ContractKind, UpdatePatch{StartsOn: &start, EndsOn: &end}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repository := &lifecycleRepositoryStub{}
			_, err := testLifecycleService(repository).Update(context.Background(), validUpdateCommand(test.kind, test.patch))
			if !errors.Is(err, ErrInvalid) || len(repository.updates) != 0 {
				t.Fatalf("Update() error=%v repository=%+v, want validation before repository", err, repository)
			}
		})
	}
}

func TestResourceUpdatePreservesExplicitClears(t *testing.T) {
	repository := &lifecycleRepositoryStub{}
	service := testLifecycleService(repository)
	empty := ""
	_, err := service.Update(context.Background(), validUpdateCommand(ContactKind, UpdatePatch{
		Email: &empty, Phone: &empty, LocationID: &empty,
	}))
	if err != nil {
		t.Fatalf("contact clear error=%v", err)
	}
	contact := repository.updates[0].Patch
	if contact.Email == nil || contact.Phone == nil || contact.LocationID == nil ||
		*contact.Email != "" || *contact.Phone != "" || *contact.LocationID != "" {
		t.Fatalf("explicit Contact clear collapsed into omission: %+v", contact)
	}

	repository.updates = nil
	_, err = service.Update(context.Background(), validUpdateCommand(ContractKind, UpdatePatch{ClearEndsOn: true}))
	if err != nil || len(repository.updates) != 1 || !repository.updates[0].Patch.ClearEndsOn || repository.updates[0].Patch.EndsOn != nil {
		t.Fatalf("explicit Contract clear collapsed into omission: error=%v mutations=%+v", err, repository.updates)
	}
}

func TestResourceMutationsRequireExactScopeVersionReasonActorSourceAndCorrelation(t *testing.T) {
	name := "Branch"
	base := validUpdateCommand(LocationKind, UpdatePatch{Name: &name})
	tests := []struct {
		name   string
		mutate func(*UpdateCommand)
	}{
		{"target", func(c *UpdateCommand) { c.Target = scope.Target{} }},
		{"resource", func(c *UpdateCommand) { c.ResourceID = "" }},
		{"version", func(c *UpdateCommand) { c.ExpectedVersion = 0 }},
		{"reason", func(c *UpdateCommand) { c.Reason = "  " }},
		{"actor", func(c *UpdateCommand) { c.ActorID = "" }},
		{"source", func(c *UpdateCommand) { c.Source = "" }},
		{"correlation", func(c *UpdateCommand) { c.CorrelationID = "" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			command := base
			test.mutate(&command)
			repository := &lifecycleRepositoryStub{}
			if _, err := testLifecycleService(repository).Update(context.Background(), command); !errors.Is(err, ErrInvalid) || len(repository.updates) != 0 {
				t.Fatalf("Update() error=%v repository=%+v", err, repository)
			}
		})
	}
}

func TestResourceLifecycleUsesClosedStateTransitionsAndCapabilities(t *testing.T) {
	for _, test := range []struct {
		name, method, capability, from, to, action string
		kind                                       Kind
		call                                       func(*Service, LifecycleCommand) (ResourceDetail, error)
	}{
		{"deactivate contact", "deactivate_contact", "contact.lifecycle", "active", "inactive", "contact.deactivated", ContactKind, func(service *Service, command LifecycleCommand) (ResourceDetail, error) {
			return service.Deactivate(context.Background(), command)
		}},
		{"reactivate asset", "reactivate_asset", "asset.lifecycle", "inactive", "active", "asset.reactivated", AssetKind, func(service *Service, command LifecycleCommand) (ResourceDetail, error) {
			return service.Reactivate(context.Background(), command)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			repository := &lifecycleRepositoryStub{result: ResourceDetail{Summary: Summary{ID: "resource-id", Kind: string(test.kind), Version: 4, LifecycleState: test.to}}}
			command := validLifecycleCommand(test.kind)
			command.Principal = resourcePrincipal(test.capability)
			result, err := test.call(testLifecycleService(repository), command)
			if err != nil || result.LifecycleState != test.to || repository.method != test.method || len(repository.lifecycles) != 1 {
				t.Fatalf("lifecycle result=%+v error=%v repository=%+v", result, err, repository)
			}
			accepted := repository.lifecycles[0]
			if accepted.FromState != test.from || accepted.ToState != test.to ||
				accepted.Audit.Action != test.action || accepted.Event.EventType != test.action ||
				accepted.Audit.SubjectVersion != 4 || accepted.Event.SubjectVersion != 4 {
				t.Fatalf("lifecycle mutation=%+v", accepted)
			}
		})
	}

	repository := &lifecycleRepositoryStub{}
	command := validLifecycleCommand(LocationKind)
	command.Principal = resourcePrincipal("location.update")
	if _, err := testLifecycleService(repository).Deactivate(context.Background(), command); !errors.Is(err, authorization.ErrForbidden) || len(repository.lifecycles) != 0 {
		t.Fatalf("unauthorized lifecycle error=%v repository=%+v", err, repository)
	}
}

func TestResourceLifecyclePreservesTypedRepositoryConflicts(t *testing.T) {
	for _, want := range []error{object.ErrVersionConflict, ErrLifecycleConflict, ErrResourceInUse, ErrResourceAuthorityConflict, scope.ErrNotFound} {
		repository := &lifecycleRepositoryStub{err: want}
		_, err := testLifecycleService(repository).Deactivate(context.Background(), validLifecycleCommand(LocationKind))
		if !errors.Is(err, want) {
			t.Fatalf("Deactivate() error=%v, want %v", err, want)
		}
	}
}

func validUpdateCommand(kind Kind, patch UpdatePatch) UpdateCommand {
	return UpdateCommand{
		Principal: resourcePrincipal(string(kind) + ".update"), Target: lifecycleTarget(),
		Kind: kind, ResourceID: "resource-id", ExpectedVersion: 3, Patch: patch,
		Reason: "business correction", ActorID: "actor-id", Source: "api", CorrelationID: "correlation-id",
	}
}

func validLifecycleCommand(kind Kind) LifecycleCommand {
	return LifecycleCommand{
		Principal: resourcePrincipal(string(kind) + ".lifecycle"), Target: lifecycleTarget(),
		Kind: kind, ResourceID: "resource-id", ExpectedVersion: 3,
		Reason: "lifecycle correction", ActorID: "actor-id", Source: "api", CorrelationID: "correlation-id",
	}
}

func lifecycleTarget() scope.Target { return scope.Target{MSPID: "msp-id", ClientID: "client-id"} }

func testLifecycleService(repository Repository) *Service {
	return NewService(repository, time.Now, sequenceLifecycleIDs())
}

func sequenceLifecycleIDs() func() string {
	next := 0
	return func() string {
		next++
		ids := []string{"audit-id", "event-id"}
		if next <= len(ids) {
			return ids[next-1]
		}
		return "later-id"
	}
}
