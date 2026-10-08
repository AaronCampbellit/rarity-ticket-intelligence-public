package servicekeys

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

type memoryRepository struct {
	records  map[string]Record
	issued   IssueMutation
	revoked  RevokeMutation
	rotation RotateMutation
}

func (r *memoryRepository) Create(_ context.Context, mutation IssueMutation) error {
	if r.records == nil {
		r.records = map[string]Record{}
	}
	r.issued = mutation
	r.records[mutation.Record.Prefix] = mutation.Record
	return nil
}

func (r *memoryRepository) List(_ context.Context, target scope.Target) ([]Record, error) {
	records := make([]Record, 0)
	for _, record := range r.records {
		if record.MSPID == target.MSPID &&
			(target.ClientID == "" || record.ClientID == target.ClientID) {
			records = append(records, record)
		}
	}
	return records, nil
}

func (r *memoryRepository) FindByPrefix(_ context.Context, prefix string) (Record, error) {
	record, ok := r.records[prefix]
	if !ok {
		return Record{}, ErrInvalidKey
	}
	return record, nil
}

func (r *memoryRepository) FindByID(
	_ context.Context,
	target scope.Target,
	id string,
) (Record, error) {
	for _, record := range r.records {
		if record.ID == id && record.MSPID == target.MSPID &&
			(target.ClientID == "" || record.ClientID == target.ClientID) {
			return record, nil
		}
	}
	return Record{}, ErrInvalidKey
}

func (r *memoryRepository) Revoke(_ context.Context, mutation RevokeMutation) error {
	record := r.records[mutation.Prefix]
	record.RevokedAt = &mutation.RevokedAt
	r.records[mutation.Prefix] = record
	r.revoked = mutation
	return nil
}

func (r *memoryRepository) Rotate(_ context.Context, mutation RotateMutation) error {
	old := r.records[mutation.OldPrefix]
	old.RevokedAt = &mutation.RotatedAt
	r.records[mutation.OldPrefix] = old
	r.records[mutation.Replacement.Prefix] = mutation.Replacement
	r.rotation = mutation
	return nil
}

func TestIssueStoresOnlyHashAndReturnsSecretOnce(t *testing.T) {
	repository := &memoryRepository{}
	now := time.Date(2026, time.July, 29, 15, 0, 0, 0, time.UTC)
	service := NewService(repository, func() time.Time { return now }, deterministicBytes, sequenceIDs(
		"key-id", "audit-id", "event-id", "correlation-id",
	))

	issued, err := service.Issue(context.Background(), IssueCommand{
		Principal:    adminPrincipal("client-id"),
		Name:         "Monitoring intake",
		Capabilities: []string{"work_record.create", "attachment.read"},
		DataScopes:   []string{"work_records", "attachments"},
		TTL:          90 * 24 * time.Hour, ActorID: "admin-id", Source: "api",
	})
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	if issued.Token == "" || issued.Record.Prefix == "" {
		t.Fatalf("issued key lacks token or prefix: %+v", issued)
	}
	if issued.Record.TokenHash != HashToken(issued.Token) {
		t.Fatal("stored digest does not authenticate returned token")
	}
	if issued.Record.Name != "Monitoring intake" || issued.Record.ExpiresAt != now.Add(90*24*time.Hour) {
		t.Fatalf("unexpected stored record: %+v", issued.Record)
	}
	if repository.issued.Audit.Action != "service_key.created" ||
		repository.issued.Event.EventType != "service_key.created" {
		t.Fatalf("issue lacks audit/event evidence: %+v", repository.issued)
	}
}

func TestListReturnsOnlyAuthorizedClientRecords(t *testing.T) {
	repository := &memoryRepository{records: map[string]Record{
		"client-a": {ID: "a", MSPID: "msp-id", ClientID: "client-id", Name: "A"},
		"client-b": {ID: "b", MSPID: "msp-id", ClientID: "other-client", Name: "B"},
	}}
	service := NewService(repository, time.Now, nil, func() string { return "id" })

	records, err := service.List(
		context.Background(), adminPrincipal("client-id"), scope.Target{},
	)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(records) != 1 || records[0].ID != "a" {
		t.Fatalf("List() records = %+v", records)
	}

	principal := adminPrincipal("client-id")
	principal.Capabilities = authorization.NewCapabilitySet()
	if _, err := service.List(context.Background(), principal, scope.Target{}); err == nil {
		t.Fatal("List() allowed a principal without service_key.manage")
	}
}

func TestAuthenticateReturnsAttributedDenyByDefaultPrincipal(t *testing.T) {
	repository := &memoryRepository{}
	now := time.Date(2026, time.July, 29, 15, 0, 0, 0, time.UTC)
	service := NewService(repository, func() time.Time { return now }, deterministicBytes, sequenceIDs(
		"key-id", "audit-id", "event-id", "correlation-id",
	))
	issued, err := service.Issue(context.Background(), IssueCommand{
		Principal:    adminPrincipal("client-id"),
		Name:         "Scoped intake",
		Capabilities: []string{"work_record.create"}, DataScopes: []string{"work_records"},
		TTL: time.Hour, ActorID: "admin-id", Source: "api",
	})
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}

	authenticated, err := service.Authenticate(context.Background(), issued.Token)
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}
	if authenticated.KeyID != "key-id" || authenticated.Name != "Scoped intake" {
		t.Fatalf("key attribution missing: %+v", authenticated)
	}
	if !authenticated.AllowsData("work_records") || authenticated.AllowsData("attachments") {
		t.Fatalf("data scope is not deny by default: %+v", authenticated.DataScopes)
	}
	principal := authenticated.Principal()
	if err := authorization.AuthorizeAt(
		principal,
		"work_record.create",
		scope.Target{MSPID: "msp-id", ClientID: "client-id"},
		now,
	); err != nil {
		t.Fatalf("scoped principal rejected: %v", err)
	}
	if err := authorization.AuthorizeAt(
		principal,
		"attachment.read",
		scope.Target{MSPID: "msp-id", ClientID: "client-id"},
		now,
	); !errors.Is(err, authorization.ErrForbidden) {
		t.Fatalf("ungranted capability error = %v", err)
	}
	if !principal.AllowsData("work_records") || principal.AllowsData("attachments") {
		t.Fatal("trusted principal lost deny-by-default data scopes")
	}
}

func TestAuthenticateRejectsMalformedUnknownExpiredAndRevokedKeys(t *testing.T) {
	repository := &memoryRepository{records: map[string]Record{}}
	now := time.Date(2026, time.July, 29, 15, 0, 0, 0, time.UTC)
	service := NewService(repository, func() time.Time { return now }, nil, nil)

	for _, token := range []string{"", "not-a-service-key", "rsk_unknown_secret"} {
		if _, err := service.Authenticate(context.Background(), token); !errors.Is(err, ErrInvalidKey) {
			t.Fatalf("Authenticate(%q) error = %v", token, err)
		}
	}

	expiredToken := "rsk_expired_secret"
	repository.records["expired"] = Record{
		ID: "expired-id", Prefix: "expired", TokenHash: HashToken(expiredToken),
		MSPID: "msp-id", ExpiresAt: now,
	}
	if _, err := service.Authenticate(context.Background(), expiredToken); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("expired key error = %v", err)
	}

	revokedAt := now.Add(-time.Minute)
	revokedToken := "rsk_revoked_secret"
	repository.records["revoked"] = Record{
		ID: "revoked-id", Prefix: "revoked", TokenHash: HashToken(revokedToken),
		MSPID: "msp-id", ExpiresAt: now.Add(time.Hour), RevokedAt: &revokedAt,
	}
	if _, err := service.Authenticate(context.Background(), revokedToken); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("revoked key error = %v", err)
	}
}

func TestRotateAtomicallyRevokesOldKeyAndReturnsReplacement(t *testing.T) {
	repository := &memoryRepository{}
	now := time.Date(2026, time.July, 29, 15, 0, 0, 0, time.UTC)
	ids := sequenceIDs(
		"old-id", "issue-audit", "issue-event", "issue-correlation",
		"new-id", "rotate-audit", "rotate-event", "rotate-correlation",
	)
	service := NewService(repository, func() time.Time { return now }, deterministicBytes, ids)
	old, err := service.Issue(context.Background(), IssueCommand{
		Principal: adminPrincipal(""), Name: "Automation",
		Capabilities: []string{"work_record.create"},
		DataScopes:   []string{"work_records"}, TTL: time.Hour,
		ActorID: "admin-id", Source: "api",
	})
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}

	replacement, err := service.Rotate(context.Background(), RotateCommand{
		Principal: adminPrincipal(""), KeyID: old.Record.ID,
		TTL: 2 * time.Hour, ActorID: "admin-id",
		Reason: "scheduled rotation", Source: "api",
	})
	if err != nil {
		t.Fatalf("Rotate() error = %v", err)
	}
	if replacement.Token == old.Token ||
		replacement.Record.Name != "Automation" ||
		repository.rotation.Audit.Reason != "scheduled rotation" {
		t.Fatalf("rotation evidence invalid: %+v", repository.rotation)
	}
	if _, err := service.Authenticate(context.Background(), old.Token); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("old key remains valid: %v", err)
	}
	if _, err := service.Authenticate(context.Background(), replacement.Token); err != nil {
		t.Fatalf("replacement rejected: %v", err)
	}
}

func TestRevokeUsesKeyIDAndRequiresReason(t *testing.T) {
	repository := &memoryRepository{}
	now := time.Date(2026, time.July, 29, 15, 0, 0, 0, time.UTC)
	service := NewService(repository, func() time.Time { return now }, deterministicBytes, sequenceIDs(
		"key-id", "issue-audit", "issue-event", "issue-correlation",
		"revoke-audit", "revoke-event", "revoke-correlation",
	))
	issued, err := service.Issue(context.Background(), IssueCommand{
		Principal: adminPrincipal("client-id"), Name: "Webhook",
		Capabilities: []string{"work_record.create"},
		DataScopes:   []string{"work_records"}, TTL: time.Hour,
		ActorID: "admin-id", Source: "api",
	})
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	err = service.Revoke(context.Background(), RevokeCommand{
		Principal: adminPrincipal("client-id"), KeyID: issued.Record.ID,
		ActorID: "admin-id", Reason: "integration retired", Source: "api",
	})
	if err != nil {
		t.Fatalf("Revoke() error = %v", err)
	}
	if repository.revoked.KeyID != "key-id" ||
		repository.revoked.Audit.Reason != "integration retired" {
		t.Fatalf("unexpected revoke mutation: %+v", repository.revoked)
	}
}

func TestLifecycleRequiresServiceKeyManageCapability(t *testing.T) {
	repository := &memoryRepository{}
	principal := adminPrincipal("client-id")
	principal.Capabilities = authorization.NewCapabilitySet("connection.secret.rotate")
	service := NewService(repository, time.Now, deterministicBytes, func() string { return "unused" })
	_, err := service.Issue(context.Background(), IssueCommand{
		Principal: principal, Name: "Denied",
		Capabilities: []string{"work_record.create"},
		DataScopes:   []string{"work_records"}, TTL: time.Hour,
		ActorID: "admin-id", Source: "api",
	})
	if !errors.Is(err, authorization.ErrForbidden) {
		t.Fatalf("Issue() error = %v, want ErrForbidden", err)
	}
	if len(repository.records) != 0 {
		t.Fatal("forbidden issue reached repository")
	}
}

func adminPrincipal(clientID string) authorization.Principal {
	return authorization.Principal{
		ID: "admin-id",
		Scope: scope.Principal{
			MSPID: "msp-id", ClientID: clientID,
		},
		Capabilities: authorization.NewCapabilitySet("service_key.manage"),
	}
}

func deterministicBytes(size int) ([]byte, error) {
	value := make([]byte, size)
	for index := range value {
		value[index] = byte(index + 1)
	}
	return value, nil
}

func sequenceIDs(values ...string) func() string {
	index := 0
	return func() string {
		value := values[index]
		index++
		return value
	}
}
