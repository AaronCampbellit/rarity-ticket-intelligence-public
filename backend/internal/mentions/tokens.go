package mentions

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	maximumTokensPerSource = 100
	maximumStableIDBytes   = 128
	maximumLabelUTF16Units = 256
)

// DiffTokens returns tokens whose stable IDs are newly present in after.
// Retained IDs may move or change label as source text is edited, but they may
// never change target identity. Call ValidateTokens on each submitted body and
// its complete token set before diffing source revisions.
func DiffTokens(before, after []Token) (TokenDiff, error) {
	beforeByID, ok := tokenIdentities(before)
	if !ok {
		return TokenDiff{}, ErrInvalidTokens
	}
	if _, ok := tokenIdentities(after); !ok {
		return TokenDiff{}, ErrInvalidTokens
	}

	diff := TokenDiff{Added: []Token{}}
	for _, token := range after {
		previous, retained := beforeByID[token.ID]
		if !retained {
			diff.Added = append(diff.Added, token)
			continue
		}
		if previous.TargetType != token.TargetType || previous.TargetID != token.TargetID {
			return TokenDiff{}, ErrInvalidTokens
		}
	}
	return diff, nil
}

// ValidateTokens validates one submitted body and its complete structured
// token set. It does not discover mentions in plain text.
func ValidateTokens(body string, tokens []Token) error {
	if !utf8.ValidString(body) || len(tokens) > maximumTokensPerSource {
		return ErrInvalidTokens
	}
	if _, ok := tokenIdentities(tokens); !ok {
		return ErrInvalidTokens
	}

	boundaries, bodyUnits := utf16Boundaries(body)
	previousEnd := 0
	for index, token := range tokens {
		if !validLabel(token.Label) || token.Start < 0 || token.End <= token.Start ||
			token.End > bodyUnits || (index > 0 && token.Start < previousEnd) {
			return ErrInvalidTokens
		}
		startByte, startOK := boundaries[token.Start]
		endByte, endOK := boundaries[token.End]
		if !startOK || !endOK || body[startByte:endByte] != token.Label {
			return ErrInvalidTokens
		}
		previousEnd = token.End
	}
	return nil
}

func tokenIdentities(tokens []Token) (map[string]Token, bool) {
	if len(tokens) > maximumTokensPerSource {
		return nil, false
	}
	byID := make(map[string]Token, len(tokens))
	for _, token := range tokens {
		if !validStableID(token.ID) || !validStableID(token.TargetID) ||
			(token.TargetType != TargetStaff && token.TargetType != TargetTeam) {
			return nil, false
		}
		if _, duplicate := byID[token.ID]; duplicate {
			return nil, false
		}
		byID[token.ID] = token
	}
	return byID, true
}

func validStableID(value string) bool {
	if value == "" || len(value) > maximumStableIDBytes || !utf8.ValidString(value) {
		return false
	}
	for index := 0; index < len(value); index++ {
		character := value[index]
		if (character >= 'a' && character <= 'z') ||
			(character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') ||
			strings.ContainsRune("-_.:", rune(character)) {
			continue
		}
		return false
	}
	return true
}

func validLabel(value string) bool {
	if strings.TrimSpace(value) == "" || !utf8.ValidString(value) {
		return false
	}
	units := 0
	for _, character := range value {
		if unicode.IsControl(character) {
			return false
		}
		units += utf16RuneUnits(character)
		if units > maximumLabelUTF16Units {
			return false
		}
	}
	return true
}

func utf16Boundaries(value string) (map[int]int, int) {
	boundaries := map[int]int{0: 0}
	units := 0
	for byteIndex, character := range value {
		units += utf16RuneUnits(character)
		boundaries[units] = byteIndex + utf8.RuneLen(character)
	}
	return boundaries, units
}

func utf16RuneUnits(character rune) int {
	if character > 0xffff {
		return 2
	}
	return 1
}
