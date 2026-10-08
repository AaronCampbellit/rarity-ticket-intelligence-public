package mentions

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/observability"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

func TestPrepareMutationCreatesDeterministicCollapsedMentionFacts(t *testing.T) {
	access := &candidateRepository{
		direct: []MemberAccess{eligibleMember("tech-2")},
		teams: []TeamAccess{{
			TeamID: "team-1", MSPID: "msp-a", Version: 3,
			Members: []MemberAccess{eligibleMember("tech-3"), eligibleMember("tech-2")},
		}},
	}
	prepared, err := NewService(fixedNow, sequentialMentionIDs()).PrepareMutation(
		context.Background(), access, PrepareCommand{
			Author:        mentionAuthor(),
			Source:        workSource(),
			SourceID:      "source-1",
			SubmittedBody: "@Mira @Tier",
			SubmittedTokens: []Token{
				mentionStaffToken("direct-1", "tech-2", "@Mira", 0, 5),
				mentionTeamToken("team-token", "team-1", "@Tier", 6, 11),
			},
			SourceRevision: 2,
			CorrelationID:  "correlation-1",
			SourceSystem:   "work.comment",
		},
	)
	if err != nil {
		t.Fatalf("PrepareMutation() error = %v", err)
	}
	if len(prepared.Occurrences) != 2 || len(prepared.Resolutions) != 3 ||
		len(prepared.Items) != 2 || len(prepared.Audits) != 2 || prepared.Event == nil {
		t.Fatalf("prepared=%+v", prepared)
	}
	if prepared.Occurrences[0].TargetType != TargetStaff ||
		prepared.Occurrences[1].TargetType != TargetTeam ||
		prepared.Occurrences[0].SourceRevision != 2 ||
		prepared.Occurrences[0].TokenID != "direct-1" {
		t.Fatalf("occurrences=%+v", prepared.Occurrences)
	}
	itemByRecipient := map[string]Item{}
	for _, item := range prepared.Items {
		itemByRecipient[item.RecipientID] = item
		if item.State != Unread || item.Version != 1 || !item.LastMentionedAt.Equal(fixedNow().UTC()) {
			t.Fatalf("item=%+v", item)
		}
	}
	teamOccurrence := prepared.Occurrences[1].ID
	if itemByRecipient["tech-2"].LatestOccurrenceID != teamOccurrence ||
		itemByRecipient["tech-3"].LatestOccurrenceID != teamOccurrence {
		t.Fatalf("items=%+v team occurrence=%s", itemByRecipient, teamOccurrence)
	}
	wantRecipients := []string{"tech-2", "tech-3"}
	if !reflect.DeepEqual(prepared.Event.Data["recipient_ids"], wantRecipients) {
		t.Fatalf("event recipients=%#v", prepared.Event.Data["recipient_ids"])
	}
	latest, ok := prepared.Event.Data["latest_occurrence_by_recipient"].(map[string]string)
	if !ok || latest["tech-2"] != teamOccurrence || latest["tech-3"] != teamOccurrence {
		t.Fatalf("event latest=%#v", prepared.Event.Data["latest_occurrence_by_recipient"])
	}
	encoded, err := json.Marshal(prepared.Event)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"@Mira", "@Tier", "please", "label", "body"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("event leaked %q: %s", forbidden, encoded)
		}
	}
}

func TestPrepareMutationBuffersContentFreeTelemetryUntilPersistenceCommits(t *testing.T) {
	telemetry := observability.NewMentionTelemetry(nil)
	access := &candidateRepository{direct: []MemberAccess{eligibleMember("tech-2")}}
	prepared, err := NewService(fixedNow, sequentialMentionIDs()).WithTelemetry(telemetry).PrepareMutation(
		context.Background(), access, PrepareCommand{
			Author: mentionAuthor(), Source: workSource(), SourceID: "source-1",
			SubmittedBody: "@Mira and @Mira", SubmittedTokens: []Token{
				mentionStaffToken("token-1", "tech-2", "@Mira", 0, 5),
				mentionStaffToken("token-2", "tech-2", "@Mira", 10, 15),
			},
			SourceRevision: 1, CorrelationID: "correlation-1", SourceSystem: "work.comment",
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if got := telemetry.Value("occurrence_target", "work_record", "staff"); got != 0 {
		t.Fatalf("prepare emitted occurrence counter before commit: %d", got)
	}
	if got := telemetry.Value("resolution", "work_record", "eligible"); got != 0 {
		t.Fatalf("prepare emitted resolution counter before commit: %d", got)
	}
	if len(prepared.Telemetry) != 2 || prepared.Telemetry[0].Name != "occurrence_target" || prepared.Telemetry[1].Name != "resolution" {
		t.Fatalf("buffered telemetry=%+v", prepared.Telemetry)
	}
}

func TestPrepareMutationCountsDirectAndTeamRecipientCollapse(t *testing.T) {
	telemetry := observability.NewMentionTelemetry(nil)
	access := &candidateRepository{
		direct: []MemberAccess{eligibleMember("tech-2")},
		teams: []TeamAccess{{
			TeamID: "team-1", MSPID: "msp-a", Version: 3,
			Members: []MemberAccess{eligibleMember("tech-2")},
		}},
	}
	prepared, err := NewService(fixedNow, sequentialMentionIDs()).WithTelemetry(telemetry).PrepareMutation(
		context.Background(), access, PrepareCommand{
			Author: mentionAuthor(), Source: workSource(), SourceID: "source-1",
			SubmittedBody: "@Mira @Tier", SubmittedTokens: []Token{
				mentionStaffToken("direct-1", "tech-2", "@Mira", 0, 5),
				mentionTeamToken("team-1", "team-1", "@Tier", 6, 11),
			},
			SourceRevision: 1, CorrelationID: "correlation-1", SourceSystem: "work.comment",
		},
	)
	if err != nil || len(prepared.Items) != 1 || len(prepared.Resolutions) != 2 {
		t.Fatalf("prepared=%+v err=%v", prepared, err)
	}
	if got := telemetry.Value("deduplication", "work_record", "collapsed"); got != 0 {
		t.Fatalf("prepare emitted direct+Team collapse counter before commit: %d", got)
	}
	bufferedCollapse := 0
	for _, metric := range prepared.Telemetry {
		if metric.Name == "deduplication" && metric.Outcome == "collapsed" {
			bufferedCollapse++
		}
	}
	if bufferedCollapse != 1 {
		t.Fatalf("buffered direct+Team recipient collapse=%d want 1; metrics=%+v", bufferedCollapse, prepared.Telemetry)
	}
}

func TestPrepareMutationReResolvesRetainedTokensButEmitsOnlyNewStableIDs(t *testing.T) {
	token := mentionStaffToken("token-1", "tech-2", "@Mira", 0, 5)
	access := &candidateRepository{direct: []MemberAccess{eligibleMember("tech-2")}}
	service := NewService(fixedNow, sequentialMentionIDs())
	prepared, err := service.PrepareMutation(context.Background(), access, PrepareCommand{
		Author: mentionAuthor(), Source: workSource(), SourceID: "source-1",
		PriorTokens: []Token{token}, SubmittedBody: "@Mira", SubmittedTokens: []Token{token},
		SourceRevision: 2, CorrelationID: "correlation-1", SourceSystem: "work.comment",
	})
	if err != nil || len(prepared.Occurrences) != 0 || prepared.Event != nil || access.directCalls != 1 {
		t.Fatalf("prepared=%+v error=%v direct calls=%d", prepared, err, access.directCalls)
	}

	removed, err := service.PrepareMutation(context.Background(), access, PrepareCommand{
		Author: mentionAuthor(), Source: workSource(), SourceID: "source-1",
		PriorTokens: []Token{token}, SubmittedBody: "Mention removed", SourceRevision: 3,
		CorrelationID: "correlation-2", SourceSystem: "work.comment",
	})
	if err != nil || len(removed.Occurrences) != 0 {
		t.Fatalf("removed=%+v error=%v", removed, err)
	}

	readded, err := service.PrepareMutation(context.Background(), access, PrepareCommand{
		Author: mentionAuthor(), Source: workSource(), SourceID: "source-1",
		SubmittedBody: "@Mira", SubmittedTokens: []Token{token}, SourceRevision: 4,
		CorrelationID: "correlation-3", SourceSystem: "work.comment",
	})
	if err != nil || len(readded.Occurrences) != 1 || readded.Event == nil {
		t.Fatalf("readded=%+v error=%v", readded, err)
	}
}

func TestPrepareMutationCollapsesRepeatedTargetTokensToOneOccurrence(t *testing.T) {
	access := &candidateRepository{direct: []MemberAccess{eligibleMember("tech-2")}}
	prepared, err := NewService(fixedNow, sequentialMentionIDs()).PrepareMutation(
		context.Background(), access, PrepareCommand{
			Author: mentionAuthor(), Source: workSource(), SourceID: "source-1",
			SubmittedBody: "@Mira and @Mira", SubmittedTokens: []Token{
				mentionStaffToken("token-1", "tech-2", "@Mira", 0, 5),
				mentionStaffToken("token-2", "tech-2", "@Mira", 10, 15),
			},
			SourceRevision: 1, CorrelationID: "correlation-1", SourceSystem: "work.comment",
		},
	)
	if err != nil || len(prepared.Occurrences) != 1 || len(prepared.Items) != 1 ||
		len(prepared.Resolutions) != 1 {
		t.Fatalf("prepared=%+v error=%v", prepared, err)
	}
}

func TestPrepareMutationEnforcesCurrentPartialTeamConfirmation(t *testing.T) {
	access := &candidateRepository{teams: []TeamAccess{{
		TeamID: "team-1", MSPID: "msp-a", Version: 3,
		Members: []MemberAccess{
			eligibleMember("tech-2"),
			{StaffID: "tech-3", MSPID: "msp-a", Active: false, Internal: true, HasMentionRead: true, CanRead: true},
		},
	}}}
	command := PrepareCommand{
		Author: mentionAuthor(), Source: workSource(), SourceID: "source-1",
		SubmittedBody: "@Tier", SubmittedTokens: []Token{mentionTeamToken("token-1", "team-1", "@Tier", 0, 5)},
		SourceRevision: 1, CorrelationID: "correlation-1", SourceSystem: "work.note",
	}
	service := NewService(fixedNow, sequentialMentionIDs())
	if _, err := service.PrepareMutation(context.Background(), access, command); !errors.Is(err, ErrTeamConfirmationRequired) {
		t.Fatalf("missing confirmation error=%v", err)
	}
	command.ConfirmedTeams = map[string]TeamConfirmation{
		"team-1": {TeamVersion: 3, EligibleMemberIDs: []string{"tech-2"}},
	}
	if prepared, err := service.PrepareMutation(context.Background(), access, command); err != nil ||
		len(prepared.Occurrences) != 1 || len(prepared.Resolutions) != 2 ||
		prepared.Resolutions[0].Decision != DecisionEligible ||
		prepared.Resolutions[1].Decision != DecisionExcluded ||
		prepared.Resolutions[1].ReasonCode != string(ExcludedInactive) {
		t.Fatalf("prepared=%+v error=%v", prepared, err)
	}
}

func TestPrepareMutationRejectsMalformedInputAndGeneratedIDsWithoutFacts(t *testing.T) {
	valid := PrepareCommand{
		Author: mentionAuthor(), Source: workSource(), SourceID: "source-1",
		SubmittedBody: "@Mira", SubmittedTokens: []Token{mentionStaffToken("token-1", "tech-2", "@Mira", 0, 5)},
		SourceRevision: 1, CorrelationID: "correlation-1", SourceSystem: "work.comment",
	}
	access := &candidateRepository{direct: []MemberAccess{eligibleMember("tech-2")}}
	if prepared, err := NewService(fixedNow, func() string { return "bad/id" }).PrepareMutation(
		context.Background(), access, valid,
	); !errors.Is(err, ErrInvalidMutation) || len(prepared.Occurrences) != 0 {
		t.Fatalf("bad generated ID prepared=%+v error=%v", prepared, err)
	}
	if prepared, err := NewService(fixedNow, func() string { return "duplicate-id" }).PrepareMutation(
		context.Background(), access, valid,
	); !errors.Is(err, ErrInvalidMutation) || len(prepared.Occurrences) != 0 || prepared.Event != nil {
		t.Fatalf("duplicate generated ID prepared=%+v error=%v", prepared, err)
	}
	if _, err := (*Service)(nil).PrepareMutation(context.Background(), access, valid); !errors.Is(err, ErrInvalidMutation) {
		t.Fatalf("nil service error=%v", err)
	}
	valid.SubmittedTokens[0].End = 4
	if _, err := NewService(fixedNow, sequentialMentionIDs()).PrepareMutation(
		context.Background(), access, valid,
	); !errors.Is(err, ErrInvalidMutation) {
		t.Fatalf("invalid token error=%v", err)
	}
}

func TestPrepareMutationAcceptsMaximumPersistableSourceRevision(t *testing.T) {
	prepared, err := NewService(fixedNow, sequentialMentionIDs()).PrepareMutation(
		context.Background(), &candidateRepository{}, PrepareCommand{
			Author: mentionAuthor(), Source: workSource(), SourceID: "source-1",
			SubmittedBody: "Internal text", SourceRevision: math.MaxInt64,
			CorrelationID: "correlation-1", SourceSystem: "work.comment",
		},
	)
	if err != nil || len(prepared.Occurrences) != 0 {
		t.Fatalf("prepared=%+v error=%v", prepared, err)
	}
}

func mentionAuthor() authorization.Principal {
	return authorization.Principal{ID: "author", Scope: scope.Principal{MSPID: "msp-a"}}
}

func mentionStaffToken(id, target, label string, start, end int) Token {
	return Token{ID: id, TargetType: TargetStaff, TargetID: target, Label: label, Start: start, End: end}
}

func mentionTeamToken(id, target, label string, start, end int) Token {
	return Token{ID: id, TargetType: TargetTeam, TargetID: target, Label: label, Start: start, End: end}
}

func sequentialMentionIDs() func() string {
	index := 0
	return func() string {
		index++
		return "generated-" + string(rune('a'+index-1))
	}
}
