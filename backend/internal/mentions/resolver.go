package mentions

import (
	"context"
	"errors"
	"reflect"
	"sort"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

const maximumTeamMembersPerSnapshot = 500

var (
	ErrInvalidResolution           = errors.New("invalid mention resolution")
	ErrDirectTargetIneligible      = errors.New("direct mention target is ineligible")
	ErrTeamHasNoEligibleRecipients = errors.New("team has no eligible mention recipients")
	ErrTeamConfirmationRequired    = errors.New("partial team confirmation required")
	ErrTeamConfirmationMismatch    = errors.New("team confirmation does not match current eligibility")
)

type ExclusionReason string

const (
	ExcludedAuthor      ExclusionReason = "author"
	ExcludedInactive    ExclusionReason = "inactive"
	ExcludedNotInternal ExclusionReason = "not_internal"
	ExcludedMentionRead ExclusionReason = "mention_read_denied"
	ExcludedSourceRead  ExclusionReason = "source_read_denied"
)

type ResolutionPath string

const (
	ResolutionDirect ResolutionPath = "direct"
	ResolutionTeam   ResolutionPath = "team"
	ResolutionBoth   ResolutionPath = "both"
)

// MemberAccess contains only authorization facts, never source or profile
// content. CanRead is effective visibility for SourceRef, including inherited
// task visibility; HasMentionRead is the independent mention.read capability.
type MemberAccess struct {
	StaffID        string
	MSPID          string
	Active         bool
	Internal       bool
	HasMentionRead bool
	CanRead        bool
}

type TeamAccess struct {
	TeamID  string
	MSPID   string
	Version int64
	Members []MemberAccess
}

type TeamConfirmation struct {
	TeamVersion       int64    `json:"team_version"`
	EligibleMemberIDs []string `json:"eligible_member_ids"`
}

type ResolveCommand struct {
	AuthorID               string
	Source                 SourceRef
	Tokens                 []Token
	ConfirmedTeamSnapshots map[string]TeamConfirmation
}

type RecipientResolution struct {
	StaffID  string         `json:"staff_id"`
	Path     ResolutionPath `json:"path"`
	TeamIDs  []string       `json:"team_ids"`
	TokenIDs []string       `json:"token_ids"`
}

type ExcludedResolution struct {
	StaffID    string          `json:"staff_id"`
	ReasonCode ExclusionReason `json:"reason_code"`
	TeamIDs    []string        `json:"team_ids"`
	TokenIDs   []string        `json:"token_ids"`
}

type Resolution struct {
	Eligible []RecipientResolution `json:"eligible"`
	Excluded []ExcludedResolution  `json:"excluded"`
}

type Resolver struct {
	repository AccessRepository
}

func NewResolver(repository AccessRepository) *Resolver {
	return &Resolver{repository: repository}
}

// Resolve must execute against the same transaction as the source write. It
// performs fresh authorization and access loads and never mutates membership
// or visibility, so mentioning a person or team cannot grant access.
func (r *Resolver) Resolve(ctx context.Context, command ResolveCommand) (Resolution, error) {
	empty := Resolution{Eligible: []RecipientResolution{}, Excluded: []ExcludedResolution{}}
	if r == nil || r.repository == nil || !validStableID(command.AuthorID) || !validSourceRef(command.Source) {
		return empty, ErrInvalidResolution
	}
	targets, err := normalizeResolutionTokens(command.Tokens)
	if err != nil {
		return empty, err
	}
	if len(targets) == 0 {
		if len(command.ConfirmedTeamSnapshots) != 0 {
			return empty, ErrTeamConfirmationMismatch
		}
		return empty, nil
	}

	authorizationRows, err := r.repository.ListCandidates(ctx, CandidateQuery{
		AuthorID: command.AuthorID, Source: command.Source, AuthorizationOnly: true,
	})
	if err != nil {
		return empty, err
	}
	if len(authorizationRows) != 0 {
		return empty, ErrInvalidAccessData
	}

	directIDs, teamIDs := targetIDs(targets)
	directRows := []MemberAccess{}
	if len(directIDs) > 0 {
		directRows, err = r.repository.LoadDirectAccess(ctx, command.Source, directIDs)
		if err != nil {
			return empty, err
		}
	}
	teamRows := []TeamAccess{}
	if len(teamIDs) > 0 {
		teamRows, err = r.repository.LoadTeamAccess(ctx, command.Source, teamIDs)
		if err != nil {
			return empty, err
		}
	}

	directByID, err := indexDirectAccess(command.Source, directIDs, directRows)
	if err != nil {
		return empty, err
	}
	teamByID, err := indexTeamAccess(command.Source, teamIDs, teamRows)
	if err != nil {
		return empty, err
	}

	eligible := make(map[string]*recipientAccumulator)
	excluded := make(map[string]*excludedAccumulator)
	knownAccess := make(map[string]MemberAccess)
	usedConfirmations := make(map[string]struct{}, len(teamIDs))

	for _, target := range targets {
		switch target.targetType {
		case TargetStaff:
			member, exists := directByID[target.targetID]
			if !exists {
				return empty, scope.ErrNotFound
			}
			if err := rememberAccess(knownAccess, member); err != nil {
				return empty, err
			}
			if reason := exclusionFor(member, command.AuthorID); reason != "" {
				return empty, ErrDirectTargetIneligible
			}
			entry := eligibleEntry(eligible, member.StaffID)
			entry.direct = true
			addStrings(entry.tokenIDs, target.tokenIDs)

		case TargetTeam:
			team, exists := teamByID[target.targetID]
			if !exists {
				return empty, scope.ErrNotFound
			}
			teamEligible := make([]string, 0, len(team.Members))
			teamExcluded := make([]memberExclusion, 0, len(team.Members))
			for _, member := range team.Members {
				if err := rememberAccess(knownAccess, member); err != nil {
					return empty, err
				}
				reason := exclusionFor(member, command.AuthorID)
				if reason == "" {
					teamEligible = append(teamEligible, member.StaffID)
					continue
				}
				teamExcluded = append(teamExcluded, memberExclusion{member: member, reason: reason})
			}
			sort.Strings(teamEligible)
			if len(teamEligible) == 0 {
				return empty, ErrTeamHasNoEligibleRecipients
			}
			confirmation, provided := command.ConfirmedTeamSnapshots[team.TeamID]
			if len(teamExcluded) > 0 && !provided {
				return empty, ErrTeamConfirmationRequired
			}
			if provided {
				usedConfirmations[team.TeamID] = struct{}{}
				if confirmation.TeamVersion != team.Version ||
					!reflect.DeepEqual(confirmation.EligibleMemberIDs, teamEligible) {
					return empty, ErrTeamConfirmationMismatch
				}
			}
			for _, staffID := range teamEligible {
				entry := eligibleEntry(eligible, staffID)
				entry.teamIDs[team.TeamID] = struct{}{}
				addStrings(entry.tokenIDs, target.tokenIDs)
			}
			for _, denied := range teamExcluded {
				entry := excludedEntry(excluded, denied.member.StaffID, denied.reason)
				entry.teamIDs[team.TeamID] = struct{}{}
				addStrings(entry.tokenIDs, target.tokenIDs)
			}
		}
	}

	if len(usedConfirmations) != len(command.ConfirmedTeamSnapshots) {
		return empty, ErrTeamConfirmationMismatch
	}
	return buildResolution(eligible, excluded), nil
}

type resolutionTarget struct {
	targetType TargetType
	targetID   string
	tokenIDs   []string
}

func normalizeResolutionTokens(tokens []Token) ([]resolutionTarget, error) {
	if len(tokens) > maximumTokensPerSource {
		return nil, ErrInvalidResolution
	}
	type tokenIdentity struct {
		targetType TargetType
		targetID   string
	}
	seenTokens := make(map[string]tokenIdentity, len(tokens))
	targetTokens := make(map[string]map[string]struct{}, len(tokens))
	targetValues := make(map[string]tokenIdentity, len(tokens))
	for _, token := range tokens {
		if !validStableID(token.ID) || !validStableID(token.TargetID) ||
			(token.TargetType != TargetStaff && token.TargetType != TargetTeam) {
			return nil, ErrInvalidResolution
		}
		identity := tokenIdentity{targetType: token.TargetType, targetID: token.TargetID}
		if previous, exists := seenTokens[token.ID]; exists {
			if previous != identity {
				return nil, ErrInvalidResolution
			}
			continue
		}
		seenTokens[token.ID] = identity
		key := string(token.TargetType) + "\x00" + token.TargetID
		if targetTokens[key] == nil {
			targetTokens[key] = make(map[string]struct{})
			targetValues[key] = identity
		}
		targetTokens[key][token.ID] = struct{}{}
	}
	keys := make([]string, 0, len(targetTokens))
	for key := range targetTokens {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]resolutionTarget, 0, len(keys))
	for _, key := range keys {
		identity := targetValues[key]
		result = append(result, resolutionTarget{
			targetType: identity.targetType, targetID: identity.targetID,
			tokenIDs: sortedSet(targetTokens[key]),
		})
	}
	return result, nil
}

func targetIDs(targets []resolutionTarget) ([]string, []string) {
	direct := make([]string, 0, len(targets))
	teams := make([]string, 0, len(targets))
	for _, target := range targets {
		if target.targetType == TargetStaff {
			direct = append(direct, target.targetID)
		} else {
			teams = append(teams, target.targetID)
		}
	}
	sort.Strings(direct)
	sort.Strings(teams)
	return direct, teams
}

func indexDirectAccess(source SourceRef, requested []string, rows []MemberAccess) (map[string]MemberAccess, error) {
	allowed := stringSet(requested)
	result := make(map[string]MemberAccess, len(rows))
	for _, row := range rows {
		if !validStableID(row.StaffID) || !validStableID(row.MSPID) {
			return nil, ErrInvalidAccessData
		}
		if row.MSPID != source.MSPID {
			return nil, scope.ErrNotFound
		}
		if _, exists := allowed[row.StaffID]; !exists {
			return nil, ErrInvalidAccessData
		}
		if previous, exists := result[row.StaffID]; exists && previous != row {
			return nil, ErrInvalidAccessData
		}
		result[row.StaffID] = row
	}
	return result, nil
}

func indexTeamAccess(source SourceRef, requested []string, rows []TeamAccess) (map[string]TeamAccess, error) {
	allowed := stringSet(requested)
	result := make(map[string]TeamAccess, len(rows))
	for _, row := range rows {
		if !validStableID(row.TeamID) || !validStableID(row.MSPID) || row.Version < 1 ||
			len(row.Members) > maximumTeamMembersPerSnapshot {
			return nil, ErrInvalidAccessData
		}
		if row.MSPID != source.MSPID {
			return nil, scope.ErrNotFound
		}
		if _, exists := allowed[row.TeamID]; !exists {
			return nil, ErrInvalidAccessData
		}
		members := make([]MemberAccess, 0, len(row.Members))
		seen := make(map[string]MemberAccess, len(row.Members))
		for _, member := range row.Members {
			if !validStableID(member.StaffID) || !validStableID(member.MSPID) {
				return nil, ErrInvalidAccessData
			}
			if member.MSPID != source.MSPID {
				return nil, scope.ErrNotFound
			}
			if previous, exists := seen[member.StaffID]; exists {
				if previous != member {
					return nil, ErrInvalidAccessData
				}
				continue
			}
			seen[member.StaffID] = member
			members = append(members, member)
		}
		sort.Slice(members, func(left, right int) bool { return members[left].StaffID < members[right].StaffID })
		row.Members = members
		if previous, exists := result[row.TeamID]; exists && !reflect.DeepEqual(previous, row) {
			return nil, ErrInvalidAccessData
		}
		result[row.TeamID] = row
	}
	return result, nil
}

func exclusionFor(member MemberAccess, authorID string) ExclusionReason {
	switch {
	case member.StaffID == authorID:
		return ExcludedAuthor
	case !member.Active:
		return ExcludedInactive
	case !member.Internal:
		return ExcludedNotInternal
	case !member.HasMentionRead:
		return ExcludedMentionRead
	case !member.CanRead:
		return ExcludedSourceRead
	default:
		return ""
	}
}

func rememberAccess(known map[string]MemberAccess, member MemberAccess) error {
	if previous, exists := known[member.StaffID]; exists && previous != member {
		return ErrInvalidAccessData
	}
	known[member.StaffID] = member
	return nil
}

type recipientAccumulator struct {
	direct   bool
	teamIDs  map[string]struct{}
	tokenIDs map[string]struct{}
}

type excludedAccumulator struct {
	staffID  string
	reason   ExclusionReason
	teamIDs  map[string]struct{}
	tokenIDs map[string]struct{}
}

type memberExclusion struct {
	member MemberAccess
	reason ExclusionReason
}

func eligibleEntry(entries map[string]*recipientAccumulator, staffID string) *recipientAccumulator {
	if entries[staffID] == nil {
		entries[staffID] = &recipientAccumulator{teamIDs: make(map[string]struct{}), tokenIDs: make(map[string]struct{})}
	}
	return entries[staffID]
}

func excludedEntry(entries map[string]*excludedAccumulator, staffID string, reason ExclusionReason) *excludedAccumulator {
	key := staffID + "\x00" + string(reason)
	if entries[key] == nil {
		entries[key] = &excludedAccumulator{
			staffID: staffID, reason: reason, teamIDs: make(map[string]struct{}), tokenIDs: make(map[string]struct{}),
		}
	}
	return entries[key]
}

func buildResolution(eligible map[string]*recipientAccumulator, excluded map[string]*excludedAccumulator) Resolution {
	result := Resolution{
		Eligible: make([]RecipientResolution, 0, len(eligible)),
		Excluded: make([]ExcludedResolution, 0, len(excluded)),
	}
	for staffID, entry := range eligible {
		path := ResolutionTeam
		if entry.direct && len(entry.teamIDs) == 0 {
			path = ResolutionDirect
		} else if entry.direct {
			path = ResolutionBoth
		}
		result.Eligible = append(result.Eligible, RecipientResolution{
			StaffID: staffID, Path: path, TeamIDs: sortedSet(entry.teamIDs), TokenIDs: sortedSet(entry.tokenIDs),
		})
	}
	for _, entry := range excluded {
		result.Excluded = append(result.Excluded, ExcludedResolution{
			StaffID: entry.staffID, ReasonCode: entry.reason,
			TeamIDs: sortedSet(entry.teamIDs), TokenIDs: sortedSet(entry.tokenIDs),
		})
	}
	sort.Slice(result.Eligible, func(left, right int) bool {
		return result.Eligible[left].StaffID < result.Eligible[right].StaffID
	})
	sort.Slice(result.Excluded, func(left, right int) bool {
		if result.Excluded[left].StaffID != result.Excluded[right].StaffID {
			return result.Excluded[left].StaffID < result.Excluded[right].StaffID
		}
		return result.Excluded[left].ReasonCode < result.Excluded[right].ReasonCode
	})
	return result
}

func addStrings(destination map[string]struct{}, values []string) {
	for _, value := range values {
		destination[value] = struct{}{}
	}
}

func sortedSet(values map[string]struct{}) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func stringSet(values []string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		result[value] = struct{}{}
	}
	return result
}
