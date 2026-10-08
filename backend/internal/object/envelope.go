// Package object defines the common metadata carried by first-class Rarity
// entities. Domain packages embed Envelope rather than redefining scope,
// lifecycle, concurrency, and actor metadata.
package object

import (
	"errors"
	"time"
)

var (
	ErrInvalidEnvelope = errors.New("invalid object envelope")
	ErrVersionConflict = errors.New("object version conflict")
)

type Envelope struct {
	ID             string
	ObjectType     string
	MSPID          string
	ClientID       string
	DisplayID      string
	LifecycleState string
	Version        int64
	CreatedAt      time.Time
	CreatedBy      string
	UpdatedAt      time.Time
	UpdatedBy      string
}

func (e Envelope) Validate() error {
	if e.ID == "" ||
		e.ObjectType == "" ||
		e.MSPID == "" ||
		e.LifecycleState == "" ||
		e.Version < 1 ||
		e.CreatedBy == "" ||
		e.UpdatedBy == "" {
		return ErrInvalidEnvelope
	}
	return nil
}

func RequireVersion(current, expected int64) error {
	if current != expected {
		return ErrVersionConflict
	}
	return nil
}
