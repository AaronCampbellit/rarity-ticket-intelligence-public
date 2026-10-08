// Package scope contains the trusted tenant-isolation boundary shared by
// application services. Callers must construct Principal from authenticated
// state, never from request payload scope fields.
package scope

import "errors"

var (
	// ErrNotFound intentionally hides whether an inaccessible object exists.
	ErrNotFound = errors.New("object not found")
	// ErrInvalid indicates that trusted scope construction was incomplete.
	ErrInvalid = errors.New("invalid trusted scope")
)

type Principal struct {
	MSPID    string
	ClientID string
}

type Target struct {
	MSPID    string
	ClientID string
}

// Authorize applies tenant isolation only. Capability and domain authorization
// remain separate checks performed by the owning application service.
func Authorize(principal Principal, target Target) error {
	if principal.MSPID == "" || target.MSPID == "" {
		return ErrInvalid
	}
	if principal.MSPID != target.MSPID {
		return ErrNotFound
	}
	if principal.ClientID == "" {
		return nil
	}
	if target.ClientID == "" || principal.ClientID != target.ClientID {
		return ErrNotFound
	}
	return nil
}
