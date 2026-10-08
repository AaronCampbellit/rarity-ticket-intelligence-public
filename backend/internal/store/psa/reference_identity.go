package psa

import "github.com/rarity-ticket-intelligence/rarity/backend/internal/objectidentity"

// PostgreSQL's regular-expression \s class does not cover the complete Unicode
// White_Space set used by strings.Fields. Translate the non-ASCII members to
// ordinary spaces before the SQL query collapses whitespace.
const unicodeReferenceWhitespace = "\u0085\u00a0\u1680\u2000\u2001\u2002\u2003\u2004\u2005\u2006\u2007\u2008\u2009\u200a\u2028\u2029\u202f\u205f\u3000"

func normalizedReference(value string) string {
	return objectidentity.Normalize(value)
}

func exactReferenceMatches[T any](
	candidates []T,
	reference string,
	displayID func(T) string,
	name func(T) string,
) []T {
	normalized := normalizedReference(reference)
	displayMatches := make([]T, 0, len(candidates))
	nameMatches := make([]T, 0, len(candidates))
	for _, candidate := range candidates {
		if normalizedReference(displayID(candidate)) == normalized {
			displayMatches = append(displayMatches, candidate)
			continue
		}
		if normalizedReference(name(candidate)) == normalized {
			nameMatches = append(nameMatches, candidate)
		}
	}
	if len(displayMatches) != 0 {
		return displayMatches
	}
	return nameMatches
}
