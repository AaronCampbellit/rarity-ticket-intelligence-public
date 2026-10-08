package projects

import "strings"

// ProjectReferenceMatches compares a user-supplied reference with stored
// Project identity fields using one deterministic Unicode-aware normalizer.
func ProjectReferenceMatches(reference, name, displayID string) bool {
	normalizedReference := normalizeProjectReference(reference)
	return normalizedReference != "" &&
		(normalizedReference == normalizeProjectReference(name) ||
			normalizedReference == normalizeProjectReference(displayID))
}

func normalizeProjectReference(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(value), " "))
}
