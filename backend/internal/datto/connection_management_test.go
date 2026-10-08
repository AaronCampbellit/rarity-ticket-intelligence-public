package datto

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

type connectionManagementRepositoryStub struct {
	item     ManagedConnection
	accepted ConnectionMutation
}

func (r *connectionManagementRepositoryStub) ListManagedConnections(
	context.Context, string,
) ([]ManagedConnection, error) {
	return []ManagedConnection{r.item}, nil
}
func (r *connectionManagementRepositoryStub) GetManagedConnection(
	context.Context, string, string,
) (ManagedConnection, error) {
	return r.item, nil
}
func (r *connectionManagementRepositoryStub) CreateManagedConnection(
	_ context.Context, accepted ConnectionMutation,
) error {
	accepted.ProtectedCredential = append(
		[]byte(nil), accepted.ProtectedCredential...,
	)
	r.accepted = accepted
	return nil
}
func (r *connectionManagementRepositoryStub) UpdateManagedConnection(
	_ context.Context, accepted ConnectionMutation,
) error {
	r.accepted = accepted
	return nil
}
func (r *connectionManagementRepositoryStub) ReplaceManagedCredential(
	_ context.Context, accepted ConnectionMutation,
) error {
	r.accepted = accepted
	return nil
}

func TestConnectionManagementCreatesNormalizedWriteOnlyCredential(t *testing.T) {
	at := time.Date(2026, time.July, 30, 17, 0, 0, 0, time.UTC)
	repository := &connectionManagementRepositoryStub{}
	ids := []string{"connection-id", "audit-id", "event-id", "correlation-id"}
	service := NewConnectionManagementService(
		repository, func() time.Time { return at },
		func() string {
			value := ids[0]
			ids = ids[1:]
			return value
		},
	)
	secret := []byte("top-secret")
	item, err := service.Create(context.Background(), CreateConnectionCommand{
		Principal:    dattoMSPPrincipal("integration.manage"),
		Name:         " Primary RMM ",
		APIURL:       "https://example.centrastage.net/",
		APIKey:       []byte(" api-key "),
		APISecret:    secret,
		SyncInterval: 15 * time.Minute,
		Reason:       "onboard primary Datto account",
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if item.ID != "connection-id" || item.Name != "Primary RMM" ||
		item.APIURL != "https://example.centrastage.net" ||
		item.SyncIntervalSeconds != 900 || !item.Enabled {
		t.Fatalf("item=%+v", item)
	}
	var credential map[string]string
	if err := json.Unmarshal(repository.accepted.ProtectedCredential, &credential); err != nil {
		t.Fatal(err)
	}
	if credential["api_key"] != "api-key" ||
		credential["api_secret"] != "top-secret" ||
		repository.accepted.Audit.Reason != "onboard primary Datto account" {
		t.Fatalf("accepted=%+v credential=%+v", repository.accepted, credential)
	}
}

func TestConnectionManagementRejectsNonDattoEndpoint(t *testing.T) {
	service := NewConnectionManagementService(
		&connectionManagementRepositoryStub{}, time.Now,
		func() string { return "id" },
	)
	_, err := service.Create(context.Background(), CreateConnectionCommand{
		Principal: dattoMSPPrincipal("integration.manage"),
		Name:      "RMM", APIURL: "https://attacker.example",
		APIKey: []byte("key"), APISecret: []byte("secret"),
		SyncInterval: 15 * time.Minute, Reason: "test",
	})
	if err != ErrInvalidConnectionManagement {
		t.Fatalf("Create() error = %v", err)
	}
}
