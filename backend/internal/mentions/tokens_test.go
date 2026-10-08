package mentions

import (
	"errors"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func TestDiffTokensReturnsOnlyNewStableTokenIDs(t *testing.T) {
	before := []Token{{ID: "keep", TargetType: TargetStaff, TargetID: "tech-1"}}
	after := []Token{
		{ID: "keep", TargetType: TargetStaff, TargetID: "tech-1"},
		{ID: "new", TargetType: TargetTeam, TargetID: "team-1"},
	}
	diff, err := DiffTokens(before, after)
	if err != nil || !reflect.DeepEqual(diff.Added, after[1:]) {
		t.Fatalf("diff=%+v error=%v", diff, err)
	}
}

func TestDiffTokensRejectsMalformedOrDuplicateStableIDs(t *testing.T) {
	tests := []struct {
		name   string
		before []Token
		after  []Token
	}{
		{name: "empty token ID", after: []Token{{TargetType: TargetStaff, TargetID: "tech-1"}}},
		{name: "malformed token ID", after: []Token{{ID: "bad id", TargetType: TargetStaff, TargetID: "tech-1"}}},
		{name: "overlong token ID", after: []Token{{ID: strings.Repeat("a", 129), TargetType: TargetStaff, TargetID: "tech-1"}}},
		{name: "empty target ID", after: []Token{{ID: "token-1", TargetType: TargetStaff}}},
		{name: "malformed target ID", after: []Token{{ID: "token-1", TargetType: TargetStaff, TargetID: "bad/id"}}},
		{name: "overlong target ID", after: []Token{{ID: "token-1", TargetType: TargetStaff, TargetID: strings.Repeat("a", 129)}}},
		{name: "unsupported target", after: []Token{{ID: "token-1", TargetType: "ai", TargetID: "ai-1"}}},
		{name: "duplicate token ID", after: []Token{
			{ID: "token-1", TargetType: TargetStaff, TargetID: "tech-1"},
			{ID: "token-1", TargetType: TargetStaff, TargetID: "tech-1"},
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := DiffTokens(test.before, test.after); !errors.Is(err, ErrInvalidTokens) {
				t.Fatalf("DiffTokens() error = %v, want ErrInvalidTokens", err)
			}
		})
	}
}

func TestDiffTokensRejectsRetainedTargetMutationButAllowsTextEdits(t *testing.T) {
	before := []Token{{
		ID: "token-1", TargetType: TargetStaff, TargetID: "tech-1",
		Label: "@Mira", Start: 0, End: 5,
	}}

	for _, changed := range []Token{
		{ID: "token-1", TargetType: TargetTeam, TargetID: "tech-1", Label: "@Mira", Start: 0, End: 5},
		{ID: "token-1", TargetType: TargetStaff, TargetID: "tech-2", Label: "@Mira", Start: 0, End: 5},
	} {
		if _, err := DiffTokens(before, []Token{changed}); !errors.Is(err, ErrInvalidTokens) {
			t.Fatalf("target mutation error = %v, want ErrInvalidTokens", err)
		}
	}

	// A retained stable token may move or adopt the exact current display label
	// after an edit. ValidateTokens validates those fields against the new body.
	after := []Token{{
		ID: "token-1", TargetType: TargetStaff, TargetID: "tech-1",
		Label: "@Mira Chen", Start: 8, End: 18,
	}}
	diff, err := DiffTokens(before, after)
	if err != nil || len(diff.Added) != 0 {
		t.Fatalf("retained text edit diff=%+v error=%v", diff, err)
	}
}

func TestValidateTokensUsesUTF16CodeUnitOffsets(t *testing.T) {
	body := "😀 @Mira e\u0301"
	tokens := []Token{
		{ID: "emoji", TargetType: TargetStaff, TargetID: "tech-1", Label: "😀", Start: 0, End: 2},
		{ID: "staff", TargetType: TargetStaff, TargetID: "tech-2", Label: "@Mira", Start: 3, End: 8},
		{ID: "combining", TargetType: TargetTeam, TargetID: "team-1", Label: "e\u0301", Start: 9, End: 11},
	}
	if err := ValidateTokens(body, tokens); err != nil {
		t.Fatalf("ValidateTokens() error = %v", err)
	}
}

func TestValidateTokensRejectsInvalidSpansAndLabels(t *testing.T) {
	tests := []struct {
		name  string
		body  string
		token Token
	}{
		{name: "negative start", body: "@Mira", token: validStaffToken(-1, 5, "@Mira")},
		{name: "empty span", body: "@Mira", token: validStaffToken(1, 1, "@Mira")},
		{name: "reversed span", body: "@Mira", token: validStaffToken(5, 1, "@Mira")},
		{name: "past body", body: "@Mira", token: validStaffToken(0, 6, "@Mira")},
		{name: "splits surrogate pair", body: "😀", token: validStaffToken(1, 2, "😀")},
		{name: "label mismatch", body: "@Mira", token: validStaffToken(0, 5, "@Nora")},
		{name: "empty label", body: "@Mira", token: validStaffToken(0, 5, "")},
		{name: "control label", body: "@Mi\nra", token: validStaffToken(0, 6, "@Mi\nra")},
		{name: "overlong label", body: strings.Repeat("a", 257), token: validStaffToken(0, 257, strings.Repeat("a", 257))},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := ValidateTokens(test.body, []Token{test.token}); !errors.Is(err, ErrInvalidTokens) {
				t.Fatalf("ValidateTokens() error = %v, want ErrInvalidTokens", err)
			}
		})
	}
}

func TestValidateTokensRequiresOrderedNonOverlappingUniqueTokens(t *testing.T) {
	tests := []struct {
		name   string
		tokens []Token
	}{
		{name: "out of order", tokens: []Token{
			{ID: "second", TargetType: TargetStaff, TargetID: "tech-2", Label: "@Nora", Start: 6, End: 11},
			{ID: "first", TargetType: TargetStaff, TargetID: "tech-1", Label: "@Mira", Start: 0, End: 5},
		}},
		{name: "overlap", tokens: []Token{
			{ID: "first", TargetType: TargetStaff, TargetID: "tech-1", Label: "@Mira", Start: 0, End: 5},
			{ID: "overlap", TargetType: TargetStaff, TargetID: "tech-2", Label: "ra @Nora", Start: 3, End: 11},
		}},
		{name: "duplicate", tokens: []Token{
			{ID: "same", TargetType: TargetStaff, TargetID: "tech-1", Label: "@Mira", Start: 0, End: 5},
			{ID: "same", TargetType: TargetStaff, TargetID: "tech-1", Label: "@Nora", Start: 6, End: 11},
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := ValidateTokens("@Mira @Nora", test.tokens); !errors.Is(err, ErrInvalidTokens) {
				t.Fatalf("ValidateTokens() error = %v, want ErrInvalidTokens", err)
			}
		})
	}
}

func TestValidateTokensEnforcesMaximumAndIgnoresPlainAtText(t *testing.T) {
	if err := ValidateTokens("plain @text is not a structured mention", nil); err != nil {
		t.Fatalf("plain text error = %v", err)
	}
	diff, err := DiffTokens(nil, nil)
	if err != nil || len(diff.Added) != 0 {
		t.Fatalf("plain text diff=%+v error=%v", diff, err)
	}

	tokens := make([]Token, 101)
	for index := range tokens {
		tokens[index] = Token{ID: "token-" + strconv.Itoa(index), TargetType: TargetStaff, TargetID: "tech-1"}
	}
	if _, err := DiffTokens(nil, tokens); !errors.Is(err, ErrInvalidTokens) {
		t.Fatalf("over maximum DiffTokens() error = %v, want ErrInvalidTokens", err)
	}
}

func validStaffToken(start, end int, label string) Token {
	return Token{ID: "token-1", TargetType: TargetStaff, TargetID: "tech-1", Label: label, Start: start, End: end}
}
