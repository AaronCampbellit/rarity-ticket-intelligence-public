package object

import (
	"errors"
	"testing"
)

func TestEnvelopeValidateRequiresTrustedScopeAndPositiveVersion(t *testing.T) {
	valid := Envelope{
		ID: "id", ObjectType: "client_organization", MSPID: "msp-id",
		ClientID: "client-id", LifecycleState: "active", Version: 1,
		CreatedBy: "actor-id", UpdatedBy: "actor-id",
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid envelope rejected: %v", err)
	}

	invalid := valid
	invalid.MSPID = ""
	if err := invalid.Validate(); !errors.Is(err, ErrInvalidEnvelope) {
		t.Fatalf("missing MSP error = %v", err)
	}
	invalid = valid
	invalid.Version = 0
	if err := invalid.Validate(); !errors.Is(err, ErrInvalidEnvelope) {
		t.Fatalf("zero version error = %v", err)
	}
}

func TestRequireVersionReturnsConflictWithoutMutating(t *testing.T) {
	if err := RequireVersion(7, 7); err != nil {
		t.Fatalf("matching version rejected: %v", err)
	}
	if err := RequireVersion(7, 6); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("stale version error = %v, want ErrVersionConflict", err)
	}
}
