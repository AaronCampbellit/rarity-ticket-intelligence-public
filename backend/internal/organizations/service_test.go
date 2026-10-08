package organizations

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

type captureRepository struct {
	mutation               CreateClientMutation
	calls                  int
	err                    error
	identityConflict       bool
	identityConflictErr    error
	identityConflictCalls  int
	identityConflictTarget scope.Target
	identityConflictName   string
	identityConflictID     string
}

func (r *captureRepository) CreateClientAtomic(_ context.Context, mutation CreateClientMutation) error {
	r.calls++
	r.mutation = mutation
	return r.err
}

func (r *captureRepository) ClientIdentityConflict(
	_ context.Context,
	target scope.Target,
	name string,
	displayID string,
) (bool, error) {
	r.identityConflictCalls++
	r.identityConflictTarget = target
	r.identityConflictName = name
	r.identityConflictID = displayID
	return r.identityConflict, r.identityConflictErr
}

func TestClientIdentityConflictNormalizesAndAuthorizesMSPGlobalLookup(t *testing.T) {
	repository := &captureRepository{identityConflict: true}
	service := NewService(repository, time.Now, func() string { return "unused" })
	principal := authorization.Principal{
		ID:           "actor-id",
		Scope:        scope.Principal{MSPID: "msp-id"},
		Capabilities: authorization.NewCapabilitySet("client.create"),
	}

	conflict, err := service.HasClientIdentityConflict(
		context.Background(),
		principal,
		"  Alpha   Managed\tServices  ",
		" client-100 ",
	)
	if err != nil {
		t.Fatalf("HasClientIdentityConflict() error = %v", err)
	}
	if !conflict {
		t.Fatal("HasClientIdentityConflict() = false, want true")
	}
	if repository.identityConflictCalls != 1 ||
		repository.identityConflictTarget != (scope.Target{MSPID: "msp-id"}) ||
		repository.identityConflictName != "alpha managed services" ||
		repository.identityConflictID != "client-100" {
		t.Fatalf(
			"identity conflict lookup calls=%d target=%+v name=%q displayID=%q",
			repository.identityConflictCalls,
			repository.identityConflictTarget,
			repository.identityConflictName,
			repository.identityConflictID,
		)
	}
}

func TestClientIdentityConflictRejectsInvalidOrUnauthorizedLookup(t *testing.T) {
	tests := []struct {
		name      string
		principal authorization.Principal
		client    string
		displayID string
		wantErr   error
	}{
		{
			name: "client scoped",
			principal: authorization.Principal{
				Scope:        scope.Principal{MSPID: "msp-id", ClientID: "client-id"},
				Capabilities: authorization.NewCapabilitySet("client.create"),
			},
			client: "Alpha", displayID: "CLIENT-100", wantErr: ErrForbidden,
		},
		{
			name: "missing capability",
			principal: authorization.Principal{
				Scope: scope.Principal{MSPID: "msp-id"},
			},
			client: "Alpha", displayID: "CLIENT-100",
			wantErr: authorization.ErrForbidden,
		},
		{
			name: "empty name",
			principal: authorization.Principal{
				Scope:        scope.Principal{MSPID: "msp-id"},
				Capabilities: authorization.NewCapabilitySet("client.create"),
			},
			client: " ", displayID: "CLIENT-100", wantErr: ErrInvalid,
		},
		{
			name: "empty display ID",
			principal: authorization.Principal{
				Scope:        scope.Principal{MSPID: "msp-id"},
				Capabilities: authorization.NewCapabilitySet("client.create"),
			},
			client: "Alpha", displayID: " ", wantErr: ErrInvalid,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repository := &captureRepository{}
			service := NewService(repository, time.Now, func() string { return "unused" })

			_, err := service.HasClientIdentityConflict(
				context.Background(), tt.principal, tt.client, tt.displayID,
			)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("HasClientIdentityConflict() error = %v, want %v", err, tt.wantErr)
			}
			if repository.identityConflictCalls != 0 {
				t.Fatal("rejected identity lookup reached repository")
			}
		})
	}
}

func TestCreateClientDerivesScopeAndBuildsAtomicMutation(t *testing.T) {
	repository := &captureRepository{}
	now := time.Date(2026, time.July, 29, 12, 30, 0, 0, time.UTC)
	ids := []string{"client-id", "audit-id", "event-id", "correlation-id"}
	service := NewService(repository, func() time.Time { return now }, func() string {
		id := ids[0]
		ids = ids[1:]
		return id
	})

	client, err := service.CreateClient(context.Background(), CreateClientCommand{
		Principal: authorization.Principal{
			Scope:        scope.Principal{MSPID: "msp-id"},
			Capabilities: authorization.NewCapabilitySet("client.create"),
		},
		Actor:     Actor{Type: "technician", ID: "actor-id", Source: "web"},
		DisplayID: "CLIENT-100",
		Name:      "Alpha",
	})
	if err != nil {
		t.Fatalf("CreateClient() error = %v", err)
	}

	if repository.calls != 1 {
		t.Fatalf("atomic repository calls = %d, want 1", repository.calls)
	}
	if client.ID != "client-id" || client.MSPID != "msp-id" || client.Version != 1 {
		t.Fatalf("unexpected client envelope: %+v", client)
	}
	if client.ObjectType != "client_organization" || client.LifecycleState != "active" {
		t.Fatalf("unexpected object contract: %+v", client)
	}
	if repository.mutation.Audit.MSPID != client.MSPID ||
		repository.mutation.Event.MSPID != client.MSPID {
		t.Fatal("audit and event must inherit the trusted client scope")
	}
	if repository.mutation.Audit.ClientID != client.ID ||
		repository.mutation.Audit.SubjectID != client.ID ||
		repository.mutation.Event.ClientID != client.ID ||
		repository.mutation.Event.SubjectID != client.ID {
		t.Fatal("audit and event must use the generated client identity")
	}
	if repository.mutation.Audit.ID != "audit-id" ||
		repository.mutation.Event.EventID != "event-id" {
		t.Fatal("omitted identities must preserve ordinary generation order")
	}
	if repository.mutation.Audit.CorrelationID != "correlation-id" ||
		repository.mutation.Event.CorrelationID != "correlation-id" {
		t.Fatal("audit and event must share the mutation correlation ID")
	}
	if repository.mutation.Audit.Action != "client.created" ||
		repository.mutation.Event.EventType != "client.created" {
		t.Fatal("mutation records must describe the accepted domain fact")
	}
	if repository.mutation.Event.SubjectVersion != client.Version {
		t.Fatal("event subject version must match the accepted entity version")
	}
	if len(ids) != 0 {
		t.Fatalf("unused generated IDs = %v", ids)
	}
}

func TestCreateClientUsesPreparedClientAndCorrelationIdentities(t *testing.T) {
	repository := &captureRepository{}
	now := time.Date(2026, time.August, 4, 10, 15, 0, 0, time.UTC)
	ids := []string{
		"019fb3c2-0000-7000-8000-000000000303",
		"019fb3c2-0000-7000-8000-000000000304",
	}
	service := NewService(repository, func() time.Time { return now }, func() string {
		id := ids[0]
		ids = ids[1:]
		return id
	})

	client, err := service.CreateClient(context.Background(), CreateClientCommand{
		Principal: authorization.Principal{
			Scope:        scope.Principal{MSPID: "msp-id"},
			Capabilities: authorization.NewCapabilitySet("client.create"),
		},
		Actor:         Actor{Type: "technician", ID: "actor-id", Source: "ai"},
		ClientID:      " 019fb3c2-0000-7000-8000-000000000301 ",
		CorrelationID: " 019fb3c2-0000-7000-8000-000000000302 ",
		DisplayID:     "CLIENT-301",
		Name:          "Prepared Client",
	})
	if err != nil {
		t.Fatalf("CreateClient() error = %v", err)
	}

	if client.ID != "019fb3c2-0000-7000-8000-000000000301" ||
		client.ClientID != client.ID {
		t.Fatalf("client identity = ID %q ClientID %q", client.ID, client.ClientID)
	}
	if repository.mutation.Client.ID != client.ID ||
		repository.mutation.Client.ClientID != client.ID {
		t.Fatalf("persisted client identity = %+v", repository.mutation.Client.Envelope)
	}
	if repository.mutation.Audit.ID != "019fb3c2-0000-7000-8000-000000000303" ||
		repository.mutation.Event.EventID != "019fb3c2-0000-7000-8000-000000000304" {
		t.Fatal("only audit and event identities must be generated")
	}
	if repository.mutation.Audit.ClientID != client.ID ||
		repository.mutation.Audit.SubjectID != client.ID ||
		repository.mutation.Event.ClientID != client.ID ||
		repository.mutation.Event.SubjectID != client.ID {
		t.Fatal("audit and event must use the prepared client identity")
	}
	if repository.mutation.Audit.CorrelationID != "019fb3c2-0000-7000-8000-000000000302" ||
		repository.mutation.Event.CorrelationID != "019fb3c2-0000-7000-8000-000000000302" {
		t.Fatal("audit and event must share the prepared correlation identity")
	}
	if repository.calls != 1 {
		t.Fatalf("atomic repository calls = %d, want 1", repository.calls)
	}
	if len(ids) != 0 {
		t.Fatalf("unused generated IDs = %v", ids)
	}
}

func TestCreateClientRejectsClientScopedPrincipalBeforeRepository(t *testing.T) {
	repository := &captureRepository{}
	service := NewService(repository, time.Now, func() string { return "unused" })

	_, err := service.CreateClient(context.Background(), CreateClientCommand{
		Principal: authorization.Principal{
			Scope:        scope.Principal{MSPID: "msp-id", ClientID: "client-id"},
			Capabilities: authorization.NewCapabilitySet("client.create"),
		},
		Actor:     Actor{Type: "technician", ID: "actor-id", Source: "web"},
		DisplayID: "CLIENT-200",
		Name:      "Bravo",
	})
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("CreateClient() error = %v, want ErrForbidden", err)
	}
	if repository.calls != 0 {
		t.Fatal("denied mutation reached repository")
	}
}

func TestCreateClientRejectsInvalidInputBeforeGeneratingRecords(t *testing.T) {
	repository := &captureRepository{}
	service := NewService(repository, time.Now, func() string { return "unused" })

	_, err := service.CreateClient(context.Background(), CreateClientCommand{
		Principal: authorization.Principal{
			Scope:        scope.Principal{MSPID: "msp-id"},
			Capabilities: authorization.NewCapabilitySet("client.create"),
		},
		Actor: Actor{Type: "technician", ID: "actor-id", Source: "web"},
	})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("CreateClient() error = %v, want ErrInvalid", err)
	}
	if repository.calls != 0 {
		t.Fatal("invalid mutation reached repository")
	}
}

func TestCreateClientRejectsMalformedPreparedIdentitiesBeforeGeneratingRecords(t *testing.T) {
	tests := []struct {
		name          string
		clientID      string
		correlationID string
	}{
		{name: "client ID", clientID: "not-a-uuid"},
		{name: "correlation ID", correlationID: "not-a-uuid"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repository := &captureRepository{}
			generated := 0
			service := NewService(repository, time.Now, func() string {
				generated++
				return "unused"
			})

			_, err := service.CreateClient(context.Background(), CreateClientCommand{
				Principal: authorization.Principal{
					Scope:        scope.Principal{MSPID: "msp-id"},
					Capabilities: authorization.NewCapabilitySet("client.create"),
				},
				Actor:         Actor{Type: "technician", ID: "actor-id", Source: "ai"},
				ClientID:      test.clientID,
				CorrelationID: test.correlationID,
				DisplayID:     "CLIENT-INVALID",
				Name:          "Invalid Prepared Identity",
			})
			if !errors.Is(err, ErrInvalid) {
				t.Fatalf("CreateClient() error = %v, want ErrInvalid", err)
			}
			if repository.calls != 0 {
				t.Fatal("invalid prepared identity reached repository")
			}
			if generated != 0 {
				t.Fatalf("generated IDs = %d, want 0", generated)
			}
		})
	}
}

func TestCreateClientRequiresCapabilityBeforeRepository(t *testing.T) {
	repository := &captureRepository{}
	service := NewService(repository, time.Now, func() string { return "unused" })

	_, err := service.CreateClient(context.Background(), CreateClientCommand{
		Principal: authorization.Principal{Scope: scope.Principal{MSPID: "msp-id"}},
		Actor:     Actor{Type: "technician", ID: "actor-id", Source: "web"},
		DisplayID: "CLIENT-300",
		Name:      "Charlie",
	})
	if !errors.Is(err, authorization.ErrForbidden) {
		t.Fatalf("CreateClient() error = %v, want authorization ErrForbidden", err)
	}
	if repository.calls != 0 {
		t.Fatal("unauthorized mutation reached repository")
	}
}
