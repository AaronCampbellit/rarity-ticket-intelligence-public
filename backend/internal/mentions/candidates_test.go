package mentions

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
)

func TestCandidateListFiltersAndSortsPermissionSafeMatches(t *testing.T) {
	repository := &candidateRepository{candidates: []Candidate{
		staffCandidate("staff-z", "Zalman", true, true, true, true),
		teamCandidate("team-a", "Alpha", 2, 1, 7),
		staffCandidate("staff-a", "alice", true, true, true, true),
		staffCandidate("author", "Al Author", true, true, true, true),
		staffCandidate("inactive", "Al Inactive", false, true, true, true),
		staffCandidate("external", "Al External", true, false, true, true),
		staffCandidate("no-cap", "Al Missing Capability", true, true, false, true),
		staffCandidate("no-read", "Al Hidden", true, true, true, false),
		{TargetType: TargetStaff, ID: "foreign", Label: "Al Foreign", Version: 1,
			MSPID: "msp-b", Active: true, Internal: true, HasMentionRead: true, CanRead: true},
		teamCandidate("empty-team", "Al Empty", 0, 3, 9),
		staffCandidate("unmatched", "Mira", true, true, true, true),
	}}

	got, err := NewCandidateService(repository).List(context.Background(), CandidateQuery{
		AuthorID: "author", Source: workSource(), Search: "  AL  ",
	})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	want := []Candidate{
		staffCandidate("staff-a", "alice", true, true, true, true),
		teamCandidate("team-a", "Alpha", 2, 1, 7),
		staffCandidate("staff-z", "Zalman", true, true, true, true),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("List() = %#v, want %#v", got, want)
	}
	if repository.candidateCalls != 1 || repository.lastQuery.Search != "AL" ||
		repository.lastQuery.Limit != maximumCandidates || repository.lastQuery.AuthorizationOnly {
		t.Fatalf("query=%+v calls=%d", repository.lastQuery, repository.candidateCalls)
	}

	got[0].Label = "mutated"
	if repository.candidates[2].Label != "alice" {
		t.Fatalf("result aliases repository candidate: %+v", repository.candidates[2])
	}
}

func TestCandidateListUsesStableTieBreakersAndCapsAtFifty(t *testing.T) {
	repository := &candidateRepository{}
	for index := 59; index >= 0; index-- {
		repository.candidates = append(repository.candidates, staffCandidate(
			fmt.Sprintf("staff-%02d", index), fmt.Sprintf("Person %02d", index), true, true, true, true,
		))
	}

	got, err := NewCandidateService(repository).List(context.Background(), CandidateQuery{
		AuthorID: "author", Source: workSource(), Search: "person",
	})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(got) != 50 || got[0].ID != "staff-00" || got[49].ID != "staff-49" {
		t.Fatalf("bounded candidates len=%d first=%+v last=%+v", len(got), got[0], got[len(got)-1])
	}

	repository.candidates = []Candidate{
		teamCandidate("z-team", "Same", 1, 0, 2),
		staffCandidate("z-staff", "Same", true, true, true, true),
		staffCandidate("a-staff", "Same", true, true, true, true),
	}
	got, err = NewCandidateService(repository).List(context.Background(), CandidateQuery{
		AuthorID: "author", Source: workSource(),
	})
	if err != nil {
		t.Fatalf("List() ties error = %v", err)
	}
	if gotIDs := candidateIDs(got); gotIDs != "a-staff,z-staff,z-team" {
		t.Fatalf("stable tie order = %q", gotIDs)
	}
}

func TestCandidateListRefreshesExactRenamedTeamByStableIdentityBeforeCap(t *testing.T) {
	repository := &candidateRepository{}
	for index := 0; index < 60; index++ {
		repository.candidates = append(repository.candidates, teamCandidate(fmt.Sprintf("other-%02d", index), fmt.Sprintf("Other %02d", index), 1, 0, 1))
	}
	renamed := teamCandidate("team-renamed", "Network Operations", 2, 1, 9)
	repository.candidates = append(repository.candidates, renamed)

	got, err := NewCandidateService(repository).List(context.Background(), CandidateQuery{
		AuthorID: "author", Source: workSource(), ExactTargetType: TargetTeam, ExactTargetID: "team-renamed",
	})
	if err != nil || !reflect.DeepEqual(got, []Candidate{renamed}) {
		t.Fatalf("exact team=%+v err=%v", got, err)
	}
	if repository.lastQuery.ExactTargetType != TargetTeam || repository.lastQuery.ExactTargetID != "team-renamed" || repository.lastQuery.Limit != maximumCandidates {
		t.Fatalf("repository exact query=%+v", repository.lastQuery)
	}

	got, err = NewCandidateService(repository).List(context.Background(), CandidateQuery{
		AuthorID: "author", Source: workSource(), ExactTargetType: TargetTeam, ExactTargetID: "team-hidden",
	})
	if err != nil || len(got) != 0 {
		t.Fatalf("missing exact team must be non-disclosing: got=%+v err=%v", got, err)
	}
}

func TestCandidateListFailsClosedForUnauthorizedOrInvalidQueries(t *testing.T) {
	denied := &candidateRepository{candidateErr: authorization.ErrForbidden}
	if _, err := NewCandidateService(denied).List(context.Background(), CandidateQuery{
		AuthorID: "author", Source: workSource(),
	}); !errors.Is(err, authorization.ErrForbidden) {
		t.Fatalf("unauthorized List() error = %v", err)
	}

	tests := []CandidateQuery{
		{Source: workSource()},
		{AuthorID: "bad/id", Source: workSource()},
		{AuthorID: "author", Source: SourceRef{}},
		{AuthorID: "author", Source: workSource(), AuthorizationOnly: true},
		{AuthorID: "author", Source: workSource(), ExactTargetType: TargetTeam},
		{AuthorID: "author", Source: workSource(), ExactTargetID: "team-1"},
		{AuthorID: "author", Source: workSource(), ExactTargetType: TargetStaff, ExactTargetID: "staff-1"},
		{AuthorID: "author", Source: workSource(), Search: "old name", ExactTargetType: TargetTeam, ExactTargetID: "team-1"},
	}
	for _, query := range tests {
		repository := &candidateRepository{}
		if _, err := NewCandidateService(repository).List(context.Background(), query); !errors.Is(err, ErrInvalidCandidateQuery) {
			t.Fatalf("List(%+v) error = %v, want ErrInvalidCandidateQuery", query, err)
		}
		if repository.candidateCalls != 0 {
			t.Fatalf("invalid query reached repository: %+v", query)
		}
	}

	if _, err := NewCandidateService(nil).List(context.Background(), CandidateQuery{
		AuthorID: "author", Source: workSource(),
	}); !errors.Is(err, ErrInvalidCandidateQuery) {
		t.Fatalf("nil repository error = %v", err)
	}
}

func TestCandidateListRejectsConflictingDuplicateRepositoryRows(t *testing.T) {
	inactive := staffCandidate("tech-1", "Alex", false, true, true, true)
	active := staffCandidate("tech-1", "Alex", true, true, true, true)
	repository := &candidateRepository{candidates: []Candidate{inactive, active}}
	if _, err := NewCandidateService(repository).List(context.Background(), CandidateQuery{
		AuthorID: "author", Source: workSource(), Search: "alex",
	}); !errors.Is(err, ErrInvalidAccessData) {
		t.Fatalf("conflicting duplicate error = %v, want ErrInvalidAccessData", err)
	}
}

func TestCandidateListReturnsExactSortedTeamSnapshotWithoutLeakingStaffSnapshots(t *testing.T) {
	repository := &candidateRepository{candidates: []Candidate{
		{
			TargetType: TargetTeam, ID: "team-1", Label: "NOC", EligibleCount: 2,
			ExcludedCount: 1, EligibleMemberIDs: []string{"staff-b", "staff-a", "staff-a"},
			Version: 4, MSPID: "msp-a", Active: true, Internal: true,
		},
		{
			TargetType: TargetStaff, ID: "staff-c", Label: "Casey", Version: 2,
			MSPID: "msp-a", Active: true, Internal: true, HasMentionRead: true, CanRead: true,
		},
	}}

	got, err := NewCandidateService(repository).List(context.Background(), CandidateQuery{
		AuthorID: "author", Source: workSource(),
	})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(got) != 2 || got[0].TargetType != TargetStaff || got[1].TargetType != TargetTeam {
		t.Fatalf("unexpected candidates: %+v", got)
	}
	if !reflect.DeepEqual(got[1].EligibleMemberIDs, []string{"staff-a", "staff-b"}) {
		t.Fatalf("team eligible IDs = %v", got[1].EligibleMemberIDs)
	}
	if got[0].EligibleMemberIDs != nil {
		t.Fatalf("staff candidate leaked eligible IDs: %+v", got[0])
	}
	got[1].EligibleMemberIDs[0] = "mutated"
	if repository.candidates[0].EligibleMemberIDs[0] != "staff-b" {
		t.Fatalf("result aliases repository snapshot: %+v", repository.candidates[0])
	}
}

func TestCandidateListRejectsIncompleteTeamSnapshotAndStaffSnapshotLeak(t *testing.T) {
	tests := []Candidate{
		{
			TargetType: TargetTeam, ID: "team-1", Label: "NOC", EligibleCount: 2,
			EligibleMemberIDs: []string{"staff-a"}, Version: 1, MSPID: "msp-a",
			Active: true, Internal: true,
		},
		{
			TargetType: TargetStaff, ID: "staff-a", Label: "Alex", Version: 1,
			EligibleMemberIDs: []string{"staff-b"}, MSPID: "msp-a", Active: true,
			Internal: true, HasMentionRead: true, CanRead: true,
		},
	}
	for _, candidate := range tests {
		repository := &candidateRepository{candidates: []Candidate{candidate}}
		if _, err := NewCandidateService(repository).List(context.Background(), CandidateQuery{
			AuthorID: "author", Source: workSource(),
		}); !errors.Is(err, ErrInvalidAccessData) {
			t.Fatalf("List(%+v) error = %v, want ErrInvalidAccessData", candidate, err)
		}
	}
}

func staffCandidate(id, label string, active, internal, mentionRead, canRead bool) Candidate {
	return Candidate{
		TargetType: TargetStaff, ID: id, Label: label, Version: 1, MSPID: "msp-a",
		Active: active, Internal: internal, HasMentionRead: mentionRead, CanRead: canRead,
	}
}

func teamCandidate(id, label string, eligible, excluded int, version int64) Candidate {
	eligibleIDs := make([]string, eligible)
	for index := range eligibleIDs {
		eligibleIDs[index] = fmt.Sprintf("member-%03d", index)
	}
	return Candidate{
		TargetType: TargetTeam, ID: id, Label: label, EligibleCount: eligible,
		ExcludedCount: excluded, EligibleMemberIDs: eligibleIDs, Version: version,
		MSPID: "msp-a", Active: true, Internal: true,
	}
}

func candidateIDs(candidates []Candidate) string {
	result := ""
	for _, candidate := range candidates {
		if result != "" {
			result += ","
		}
		result += candidate.ID
	}
	return result
}
