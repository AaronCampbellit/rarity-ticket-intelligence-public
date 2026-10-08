// Package id creates opaque UUID identifiers for durable records.
package id

import (
	"crypto/rand"
	"fmt"

	"github.com/google/uuid"
)

// ValidCanonical accepts only lowercase, hyphenated, non-nil UUIDs. uuid.Parse
// intentionally accepts several alternate encodings, so equality with String
// is required at trust boundaries before a PostgreSQL uuid cast.
func ValidCanonical(value string) bool {
	parsed, err := uuid.Parse(value)
	return err == nil && parsed != uuid.Nil && parsed.String() == value
}

func New() string {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		panic(fmt.Errorf("generate identifier: %w", err))
	}
	value[6] = (value[6] & 0x0f) | 0x40
	value[8] = (value[8] & 0x3f) | 0x80
	return fmt.Sprintf(
		"%08x-%04x-%04x-%04x-%012x",
		value[0:4], value[4:6], value[6:8], value[8:10], value[10:16],
	)
}
