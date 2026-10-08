package mentions

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

func TestResolveAuthorizesBeforeLoadingAnyTargetAccess(t *testing.T) {
	repository := &candidateRepository{candidateErr: authorization.ErrForbidden}
	_, err := NewResolver(repository).Resolve(context.Background(), ResolveCommand{
		AuthorID: "author", Source: workSource(), Tokens: []Token{staffToken("token-1", "tech-1")},
	})
	if !errors.Is(err, authorization.ErrForbidden) {
		t.Fatalf("Resolve() error = %v, want ErrForbidden", err)
	}
	if !reflect.DeepEqual(repository.calls, []string{"authorize"}) || repository.directCalls != 0 || repository.teamCalls != 0 {
		t.Fatalf("calls=%v direct=%d team=%d", repository.calls, repository.directCalls, repository.teamCalls)
	}
	if !repository.lastQuery.AuthorizationOnly || repository.lastQuery.Limit != 0 || repository.lastQuery.Search != "" {
		t.Fatalf("authorization query = %+v", repository.lastQuery)
	}

	leaky := &candidateRepository{candidates: []Candidate{staffCandidate("tech-1", "Secret", true, true, true, true)}}
	if _, err := NewResolver(leaky).Resolve(context.Background(), ResolveCommand{
		AuthorID: "author", Source: workSource(), Tokens: []Token{staffToken("token-1", "tech-1")},
	}); !errors.Is(err, ErrInvalidAccessData) {
		t.Fatalf("authorization-only data error = %v, want ErrInvalidAccessData", err)
	}
	if leaky.directCalls != 0 || leaky.teamCalls != 0 {
		t.Fatalf("leaky authorization response reached access loads")
	}
}

func TestResolveTeamSnapshotsOnlyEligibleActiveMembers(t *testing.T) {
	repository := &candidateRepository{teams: []TeamAccess{{
		TeamID: "team-1", MSPID: "msp-a", Version: 3,
		Members: []MemberAccess{
			eligibleMember("tech-1"),
			{StaffID: "tech-2", MSPID: "msp-a", Active: true, Internal: true, HasMentionRead: true},
			{StaffID: "tech-3", MSPID: "msp-a", Active: false, Internal: true, HasMentionRead: true, CanRead: true},
		},
	}}}
	confirmation := map[string]TeamConfirmation{
		"team-1": {TeamVersion: 3, EligibleMemberIDs: []string{"tech-1"}},
	}
	result, err := NewResolver(repository).Resolve(context.Background(), ResolveCommand{
		AuthorID: "author", Source: workSource(), Tokens: []Token{teamToken("token-team", "team-1")},
		ConfirmedTeamSnapshots: confirmation,
	})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	wantEligible := []RecipientResolution{{
		StaffID: "tech-1", Path: ResolutionTeam,
		TeamIDs: []string{"team-1"}, TokenIDs: []string{"token-team"},
	}}
	wantExcluded := []ExcludedResolution{
		{StaffID: "tech-2", ReasonCode: ExcludedSourceRead, TeamIDs: []string{"team-1"}, TokenIDs: []string{"token-team"}},
		{StaffID: "tech-3", ReasonCode: ExcludedInactive, TeamIDs: []string{"team-1"}, TokenIDs: []string{"token-team"}},
	}
	if !reflect.DeepEqual(result.Eligible, wantEligible) || !reflect.DeepEqual(result.Excluded, wantExcluded) {
		t.Fatalf("Resolve() = %+v", result)
	}
	if !reflect.DeepEqual(confirmation["team-1"].EligibleMemberIDs, []string{"tech-1"}) {
		t.Fatalf("Resolve mutated confirmation: %+v", confirmation)
	}
	result.Eligible[0].TeamIDs[0] = "mutated"
	if repository.teams[0].TeamID != "team-1" {
		t.Fatalf("resolution aliases repository team")
	}
}

func TestResolvePartialTeamRequiresExactSortedCurrentConfirmation(t *testing.T) {
	baseTeam := TeamAccess{TeamID: "team-1", MSPID: "msp-a", Version: 3, Members: []MemberAccess{
		eligibleMember("tech-2"), eligibleMember("tech-1"),
		{StaffID: "tech-3", MSPID: "msp-a", Active: false, Internal: true, HasMentionRead: true, CanRead: true},
	}}
	tests := []struct {
		name          string
		confirmations map[string]TeamConfirmation
		want          error
	}{
		{name: "missing", want: ErrTeamConfirmationRequired},
		{name: "stale version", confirmations: map[string]TeamConfirmation{"team-1": {TeamVersion: 2, EligibleMemberIDs: []string{"tech-1", "tech-2"}}}, want: ErrTeamConfirmationMismatch},
		{name: "missing member", confirmations: map[string]TeamConfirmation{"team-1": {TeamVersion: 3, EligibleMemberIDs: []string{"tech-1"}}}, want: ErrTeamConfirmationMismatch},
		{name: "extra member", confirmations: map[string]TeamConfirmation{"team-1": {TeamVersion: 3, EligibleMemberIDs: []string{"tech-1", "tech-2", "tech-9"}}}, want: ErrTeamConfirmationMismatch},
		{name: "duplicate member", confirmations: map[string]TeamConfirmation{"team-1": {TeamVersion: 3, EligibleMemberIDs: []string{"tech-1", "tech-1", "tech-2"}}}, want: ErrTeamConfirmationMismatch},
		{name: "unsorted", confirmations: map[string]TeamConfirmation{"team-1": {TeamVersion: 3, EligibleMemberIDs: []string{"tech-2", "tech-1"}}}, want: ErrTeamConfirmationMismatch},
		{name: "extra team", confirmations: map[string]TeamConfirmation{
			"team-1": {TeamVersion: 3, EligibleMemberIDs: []string{"tech-1", "tech-2"}},
			"team-2": {TeamVersion: 1, EligibleMemberIDs: []string{"tech-4"}},
		}, want: ErrTeamConfirmationMismatch},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repository := &candidateRepository{teams: []TeamAccess{baseTeam}}
			_, err := NewResolver(repository).Resolve(context.Background(), ResolveCommand{
				AuthorID: "author", Source: workSource(), Tokens: []Token{teamToken("token-1", "team-1")},
				ConfirmedTeamSnapshots: test.confirmations,
			})
			if !errors.Is(err, test.want) {
				t.Fatalf("Resolve() error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestResolveFullyEligibleTeamNeedsNoConfirmationAndEmptyTeamRejects(t *testing.T) {
	full := &candidateRepository{teams: []TeamAccess{{
		TeamID: "team-1", MSPID: "msp-a", Version: 2,
		Members: []MemberAccess{eligibleMember("tech-2"), eligibleMember("tech-1")},
	}}}
	result, err := NewResolver(full).Resolve(context.Background(), ResolveCommand{
		AuthorID: "author", Source: workSource(), Tokens: []Token{teamToken("token-1", "team-1")},
	})
	if err != nil || recipientIDs(result.Eligible) != "tech-1,tech-2" || len(result.Excluded) != 0 {
		t.Fatalf("fully eligible result=%+v error=%v", result, err)
	}

	empty := &candidateRepository{teams: []TeamAccess{{
		TeamID: "team-1", MSPID: "msp-a", Version: 2,
		Members: []MemberAccess{{StaffID: "tech-1", MSPID: "msp-a", Active: false, Internal: true, HasMentionRead: true, CanRead: true}},
	}}}
	_, err = NewResolver(empty).Resolve(context.Background(), ResolveCommand{
		AuthorID: "author", Source: workSource(), Tokens: []Token{teamToken("token-1", "team-1")},
	})
	if !errors.Is(err, ErrTeamHasNoEligibleRecipients) {
		t.Fatalf("empty team error = %v", err)
	}
}

func TestResolveDirectRejectsEveryIneligibleTargetAndCrossMSP(t *testing.T) {
	tests := []struct {
		name   string
		member MemberAccess
		want   error
	}{
		{name: "author", member: eligibleMember("author"), want: ErrDirectTargetIneligible},
		{name: "inactive", member: MemberAccess{StaffID: "tech-1", MSPID: "msp-a", Internal: true, HasMentionRead: true, CanRead: true}, want: ErrDirectTargetIneligible},
		{name: "external", member: MemberAccess{StaffID: "tech-1", MSPID: "msp-a", Active: true, HasMentionRead: true, CanRead: true}, want: ErrDirectTargetIneligible},
		{name: "missing mention read", member: MemberAccess{StaffID: "tech-1", MSPID: "msp-a", Active: true, Internal: true, CanRead: true}, want: ErrDirectTargetIneligible},
		{name: "missing source read", member: MemberAccess{StaffID: "tech-1", MSPID: "msp-a", Active: true, Internal: true, HasMentionRead: true}, want: ErrDirectTargetIneligible},
		{name: "cross MSP", member: MemberAccess{StaffID: "tech-1", MSPID: "msp-b", Active: true, Internal: true, HasMentionRead: true, CanRead: true}, want: scope.ErrNotFound},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repository := &candidateRepository{direct: []MemberAccess{test.member}}
			_, err := NewResolver(repository).Resolve(context.Background(), ResolveCommand{
				AuthorID: "author", Source: workSource(), Tokens: []Token{staffToken("token-1", test.member.StaffID)},
			})
			if !errors.Is(err, test.want) {
				t.Fatalf("Resolve() error = %v, want %v", err, test.want)
			}
		})
	}

	missing := &candidateRepository{}
	if _, err := NewResolver(missing).Resolve(context.Background(), ResolveCommand{
		AuthorID: "author", Source: workSource(), Tokens: []Token{staffToken("token-1", "tech-1")},
	}); !errors.Is(err, scope.ErrNotFound) {
		t.Fatalf("missing direct error = %v", err)
	}
}

func TestResolveDeduplicatesRepeatedTargetsAndRetainsDeterministicPaths(t *testing.T) {
	repository := &candidateRepository{
		direct: []MemberAccess{eligibleMember("tech-1")},
		teams: []TeamAccess{
			{TeamID: "team-b", MSPID: "msp-a", Version: 1, Members: []MemberAccess{eligibleMember("tech-1")}},
			{TeamID: "team-a", MSPID: "msp-a", Version: 1, Members: []MemberAccess{eligibleMember("tech-1"), eligibleMember("tech-2")}},
		},
	}
	result, err := NewResolver(repository).Resolve(context.Background(), ResolveCommand{
		AuthorID: "author", Source: workSource(), Tokens: []Token{
			staffToken("direct-2", "tech-1"), teamToken("team-b-token", "team-b"),
			staffToken("direct-1", "tech-1"), teamToken("team-a-token", "team-a"),
			staffToken("direct-1", "tech-1"),
		},
	})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	want := []RecipientResolution{
		{StaffID: "tech-1", Path: ResolutionBoth, TeamIDs: []string{"team-a", "team-b"}, TokenIDs: []string{"direct-1", "direct-2", "team-a-token", "team-b-token"}},
		{StaffID: "tech-2", Path: ResolutionTeam, TeamIDs: []string{"team-a"}, TokenIDs: []string{"team-a-token"}},
	}
	if !reflect.DeepEqual(result.Eligible, want) || len(result.Excluded) != 0 {
		t.Fatalf("Resolve() = %+v, want eligible=%+v", result, want)
	}
	if !reflect.DeepEqual(repository.directIDs, []string{"tech-1"}) ||
		!reflect.DeepEqual(repository.teamIDs, []string{"team-a", "team-b"}) {
		t.Fatalf("loads direct=%v teams=%v", repository.directIDs, repository.teamIDs)
	}
}

func TestResolveRejectsInvalidAndAmbiguousInputs(t *testing.T) {
	tests := []ResolveCommand{
		{Source: workSource(), Tokens: []Token{staffToken("token-1", "tech-1")}},
		{AuthorID: "author", Source: SourceRef{}, Tokens: []Token{staffToken("token-1", "tech-1")}},
		{AuthorID: "author", Source: workSource(), Tokens: []Token{{ID: "bad/id", TargetType: TargetStaff, TargetID: "tech-1"}}},
		{AuthorID: "author", Source: workSource(), Tokens: []Token{staffToken("same", "tech-1"), teamToken("same", "team-1")}},
	}
	for _, command := range tests {
		repository := &candidateRepository{}
		if _, err := NewResolver(repository).Resolve(context.Background(), command); !errors.Is(err, ErrInvalidResolution) {
			t.Fatalf("Resolve(%+v) error = %v, want ErrInvalidResolution", command, err)
		}
		if len(repository.calls) != 0 {
			t.Fatalf("invalid command reached repository: %v", repository.calls)
		}
	}
	if _, err := NewResolver(nil).Resolve(context.Background(), ResolveCommand{
		AuthorID: "author", Source: workSource(), Tokens: []Token{staffToken("token-1", "tech-1")},
	}); !errors.Is(err, ErrInvalidResolution) {
		t.Fatalf("nil resolver repository error = %v", err)
	}
}

func TestResolveRejectsInputsBeyondPersistedBounds(t *testing.T) {
	tokens := make([]Token, maximumTokensPerSource+1)
	for index := range tokens {
		tokens[index] = staffToken(fmt.Sprintf("token-%03d", index), "tech-1")
	}
	repository := &candidateRepository{}
	if _, err := NewResolver(repository).Resolve(context.Background(), ResolveCommand{
		AuthorID: "author", Source: workSource(), Tokens: tokens,
	}); !errors.Is(err, ErrInvalidResolution) {
		t.Fatalf("oversized token input error = %v, want ErrInvalidResolution", err)
	}
	if len(repository.calls) != 0 {
		t.Fatalf("oversized token input reached repository: %v", repository.calls)
	}

	members := make([]MemberAccess, 501)
	for index := range members {
		members[index] = eligibleMember(fmt.Sprintf("tech-%03d", index))
	}
	repository = &candidateRepository{teams: []TeamAccess{{
		TeamID: "team-1", MSPID: "msp-a", Version: 1, Members: members,
	}}}
	if _, err := NewResolver(repository).Resolve(context.Background(), ResolveCommand{
		AuthorID: "author", Source: workSource(), Tokens: []Token{teamToken("token-1", "team-1")},
	}); !errors.Is(err, ErrInvalidAccessData) {
		t.Fatalf("oversized team snapshot error = %v, want ErrInvalidAccessData", err)
	}
}

type candidateRepository struct {
	candidates []Candidate
	direct     []MemberAccess
	teams      []TeamAccess

	candidateErr error
	directErr    error
	teamErr      error

	calls          []string
	candidateCalls int
	directCalls    int
	teamCalls      int
	lastQuery      CandidateQuery
	directIDs      []string
	teamIDs        []string
}

func (r *candidateRepository) ListCandidates(_ context.Context, query CandidateQuery) ([]Candidate, error) {
	r.candidateCalls++
	r.lastQuery = query
	if query.AuthorizationOnly {
		r.calls = append(r.calls, "authorize")
	} else {
		r.calls = append(r.calls, "candidates")
	}
	return append([]Candidate(nil), r.candidates...), r.candidateErr
}

func (r *candidateRepository) LoadDirectAccess(_ context.Context, _ SourceRef, ids []string) ([]MemberAccess, error) {
	r.directCalls++
	r.calls = append(r.calls, "direct")
	r.directIDs = append([]string(nil), ids...)
	return append([]MemberAccess(nil), r.direct...), r.directErr
}

func (r *candidateRepository) LoadTeamAccess(_ context.Context, _ SourceRef, ids []string) ([]TeamAccess, error) {
	r.teamCalls++
	r.calls = append(r.calls, "team")
	r.teamIDs = append([]string(nil), ids...)
	return append([]TeamAccess(nil), r.teams...), r.teamErr
}

func workSource() SourceRef {
	return SourceRef{MSPID: "msp-a", ClientID: "client-a", ParentType: ParentWorkRecord, ParentID: "work-1", SourceKind: SourceComment}
}

func staffToken(id, staffID string) Token {
	return Token{ID: id, TargetType: TargetStaff, TargetID: staffID}
}

func teamToken(id, teamID string) Token {
	return Token{ID: id, TargetType: TargetTeam, TargetID: teamID}
}

func eligibleMember(id string) MemberAccess {
	return MemberAccess{StaffID: id, MSPID: "msp-a", Active: true, Internal: true, HasMentionRead: true, CanRead: true}
}

func recipientIDs(recipients []RecipientResolution) string {
	result := ""
	for _, recipient := range recipients {
		if result != "" {
			result += ","
		}
		result += recipient.StaffID
	}
	return result
}
