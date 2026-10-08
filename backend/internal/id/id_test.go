package id

import (
	"regexp"
	"testing"
)

func TestNewReturnsUniqueRFC4122Version4UUIDs(t *testing.T) {
	first := New()
	second := New()
	pattern := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

	if !pattern.MatchString(first) || !pattern.MatchString(second) {
		t.Fatalf("invalid UUIDs: %q %q", first, second)
	}
	if first == second {
		t.Fatalf("New() returned duplicate UUID %q", first)
	}
}

func TestValidCanonicalRejectsAlternateUUIDForms(t *testing.T) {
	valid := New()
	if !ValidCanonical(valid) {
		t.Fatalf("generated UUID rejected: %q", valid)
	}
	for _, invalid := range []string{
		"name", "with_underscore", "urn:uuid:" + valid,
		"00000000000040008000000000000001", "{" + valid + "}",
		"00000000-0000-4000-8000-00000000000A",
		"00000000-0000-0000-0000-000000000000",
	} {
		if ValidCanonical(invalid) {
			t.Fatalf("accepted noncanonical UUID %q", invalid)
		}
	}
}
