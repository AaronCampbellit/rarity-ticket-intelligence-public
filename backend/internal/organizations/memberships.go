package organizations

import (
	"context"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mutation"
)

const maximumTeamMembers = 500

type ReplaceTeamMembersCommand struct {
	Principal       authorization.Principal
	TeamID          string
	TechnicianIDs   []string
	ExpectedVersion int64
	Reason          string
	Source          string
}

type TeamMembershipMutation struct {
	Team          Team
	TechnicianIDs []string
	ChangedAt     time.Time
	ChangedBy     string
	Audit         mutation.AuditRecord
	Event         mutation.EventRecord
}

type TeamMembershipRepository interface {
	ReplaceTeamMembersAtomic(context.Context, TeamMembershipMutation) error
}

func (s *DirectoryService) ReplaceTeamMembers(
	ctx context.Context,
	command ReplaceTeamMembersCommand,
) (Team, error) {
	if s == nil || s.membershipRepository == nil || s.now == nil || s.newID == nil {
		return Team{}, ErrInvalid
	}
	if err := authorizeDirectoryMutation(command.Principal); err != nil {
		return Team{}, err
	}
	teamID := strings.TrimSpace(command.TeamID)
	reason := strings.TrimSpace(command.Reason)
	source := strings.TrimSpace(command.Source)
	if teamID == "" || reason == "" || source == "" ||
		command.ExpectedVersion < 1 || command.ExpectedVersion == math.MaxInt64 {
		return Team{}, ErrInvalid
	}
	members, ok := normalizeTeamMemberIDs(command.TechnicianIDs)
	if !ok {
		return Team{}, ErrInvalid
	}
	now := s.now().UTC()
	version := command.ExpectedVersion + 1
	team := Team{
		ID: teamID, MSPID: command.Principal.Scope.MSPID,
		Version: version, MemberIDs: slices.Clone(members),
	}
	auditID, eventID, correlationID := s.newID(), s.newID(), s.newID()
	accepted := TeamMembershipMutation{
		Team: team, TechnicianIDs: slices.Clone(members),
		ChangedAt: now, ChangedBy: command.Principal.ID,
		Audit: mutation.AuditRecord{
			ID: auditID, OccurredAt: now, MSPID: team.MSPID,
			ActorType: "technician", ActorID: command.Principal.ID,
			Action: "team.members.replaced", SubjectType: "team", SubjectID: team.ID,
			SubjectVersion: version, Source: source, Reason: reason,
			CorrelationID: correlationID,
		},
		Event: mutation.EventRecord{
			EventID: eventID, EventType: "team.members.replaced", SchemaVersion: 1,
			OccurredAt: now, MSPID: team.MSPID,
			ActorType: "technician", ActorID: command.Principal.ID,
			SubjectType: "team", SubjectID: team.ID, SubjectVersion: version,
			Source: source, CorrelationID: correlationID,
			Data: map[string]any{
				"member_ids": slices.Clone(members), "member_count": len(members),
			},
		},
	}
	if err := s.membershipRepository.ReplaceTeamMembersAtomic(ctx, accepted); err != nil {
		return Team{}, err
	}
	return team, nil
}

func normalizeTeamMemberIDs(values []string) ([]string, bool) {
	unique := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			return nil, false
		}
		unique[value] = struct{}{}
		if len(unique) > maximumTeamMembers {
			return nil, false
		}
	}
	result := make([]string, 0, len(unique))
	for value := range unique {
		result = append(result, value)
	}
	slices.Sort(result)
	return result, true
}
