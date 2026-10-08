package tagging

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

func TestClassificationSuggestionSerializesPublicContractNames(t *testing.T) {
	payload, err := json.Marshal(ClassificationSuggestion{TagID: "tag-id", Confidence: .9, Rationale: "reason", Active: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{`"tag_id":"tag-id"`, `"confidence":0.9`, `"rationale":"reason"`, `"active":true`} {
		if !bytes.Contains(payload, []byte(field)) {
			t.Fatalf("missing public field %s in %s", field, payload)
		}
	}
}

type classificationRepositoryStub struct {
	policy  ClassificationPolicy
	updated ClassificationPolicy
}

func (s *classificationRepositoryStub) GetClassificationPolicy(context.Context, string) (ClassificationPolicy, error) {
	return s.policy, nil
}
func (s *classificationRepositoryStub) SaveClassificationPolicy(_ context.Context, value ClassificationPolicy) (ClassificationPolicy, error) {
	s.updated = value
	return value, nil
}

func TestClassificationContextOnlyIncludesStandardFields(t *testing.T) {
	context, err := BuildClassificationContext([]ClassificationField{
		{Name: "title", Value: "VPN is down", Visibility: ClassificationStandard},
		{Name: "secret", Value: "never send", Visibility: ClassificationSecret},
		{Name: "attachment", Value: "binary", Visibility: ClassificationAttachment},
	})
	if err != nil {
		t.Fatalf("BuildClassificationContext() error = %v", err)
	}
	if len(context) != 1 || context[0].Name != "title" {
		t.Fatalf("context = %+v", context)
	}
}

func TestClassificationPolicyIsDisabledByDefaultAndRequiresAIManage(t *testing.T) {
	repository := &classificationRepositoryStub{policy: ClassificationPolicy{MSPID: "msp", Threshold: .95, Version: 1}}
	service := NewClassificationService(repository, nil, func() string { return "id" })
	clientPrincipal := authorization.Principal{ID: "client-admin", Scope: scope.Principal{MSPID: "msp", ClientID: "client"}, Capabilities: authorization.NewCapabilitySet("classification.ai.manage")}
	if _, err := service.Policy(context.Background(), clientPrincipal); !errors.Is(err, scope.ErrNotFound) {
		t.Fatalf("Client-scoped Policy() error = %v, want MSP-global scope denial", err)
	}
	if _, err := service.UpdatePolicy(context.Background(), UpdateClassificationPolicyCommand{Principal: clientPrincipal, Enabled: true, Threshold: .975, ModelProfileID: "model", ExpectedVersion: 1}); !errors.Is(err, scope.ErrNotFound) {
		t.Fatalf("Client-scoped UpdatePolicy() error = %v, want MSP-global scope denial", err)
	}
	if repository.updated.MSPID != "" {
		t.Fatalf("Client-scoped update reached repository: %+v", repository.updated)
	}
	principal := authorization.Principal{ID: "admin", Scope: scope.Principal{MSPID: "msp"}, Capabilities: authorization.NewCapabilitySet("classification.ai.manage")}
	policy, err := service.UpdatePolicy(context.Background(), UpdateClassificationPolicyCommand{Principal: principal, Enabled: true, Threshold: .975, ModelProfileID: "model", ExpectedVersion: 1})
	if err != nil {
		t.Fatalf("UpdatePolicy() error = %v", err)
	}
	if !policy.Enabled || policy.Threshold != .975 || repository.updated.ModelProfileID != "model" {
		t.Fatalf("updated policy = %+v", policy)
	}
}

func TestAutomaticClassificationAddsOnlyEligibleSuggestions(t *testing.T) {
	result := EligibleAutomaticTagIDs(0.950, []ClassificationSuggestion{
		{TagID: "vpn", Confidence: 0.950, Active: true},
		{TagID: "archived", Confidence: 1, Active: false},
		{TagID: "below", Confidence: .949, Active: true},
	})
	if len(result) != 1 || result[0] != "vpn" {
		t.Fatalf("eligible = %v", result)
	}
}
