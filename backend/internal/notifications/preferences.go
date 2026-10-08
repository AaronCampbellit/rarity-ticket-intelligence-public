package notifications

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
)

const MentionOccurred = "mention.occurred"

var (
	ErrInvalidPreference = errors.New("invalid notification preference")
	clockValue           = regexp.MustCompile(`^(?:[01][0-9]|2[0-3]):[0-5][0-9]$`)
)

type RecipientPreference struct {
	TechnicianID   string `json:"technician_id"`
	EventType      string `json:"event_type"`
	EmailEnabled   bool   `json:"email_enabled"`
	TeamsEnabled   bool   `json:"teams_enabled"`
	TimeZone       string `json:"time_zone"`
	QuietStart     string `json:"quiet_start,omitempty"`
	QuietEnd       string `json:"quiet_end,omitempty"`
	Version        int64  `json:"version"`
	EmailAvailable bool   `json:"email_available"`
	TeamsAvailable bool   `json:"teams_available"`
}

type ChannelAvailability struct {
	Email bool
	Teams bool
}

type PreferenceRepository interface {
	GetMentionPreference(context.Context, string, string) (RecipientPreference, error)
	MentionChannelAvailability(context.Context, string) (ChannelAvailability, error)
	SaveMentionPreference(context.Context, string, RecipientPreference, int64) (RecipientPreference, error)
}

type PreferenceService struct{ repository PreferenceRepository }

func NewPreferenceService(repository PreferenceRepository) *PreferenceService {
	return &PreferenceService{repository: repository}
}

func DefaultMentionPreference(technicianID string) RecipientPreference {
	return RecipientPreference{TechnicianID: technicianID, EventType: MentionOccurred, EmailEnabled: true, TeamsEnabled: true, TimeZone: "UTC"}
}

func (s *PreferenceService) Get(ctx context.Context, principal authorization.Principal) (RecipientPreference, error) {
	if s == nil || s.repository == nil || strings.TrimSpace(principal.ID) == "" || strings.TrimSpace(principal.Scope.MSPID) == "" {
		return RecipientPreference{}, ErrInvalidPreference
	}
	preference, err := s.repository.GetMentionPreference(ctx, principal.Scope.MSPID, principal.ID)
	if err != nil {
		return RecipientPreference{}, err
	}
	availability, err := s.repository.MentionChannelAvailability(ctx, principal.Scope.MSPID)
	if err != nil {
		return RecipientPreference{}, err
	}
	preference.EmailAvailable, preference.TeamsAvailable = availability.Email, availability.Teams
	return preference, nil
}

type UpdatePreferenceCommand struct {
	Principal       authorization.Principal
	EmailEnabled    bool
	TeamsEnabled    bool
	TimeZone        string
	QuietStart      string
	QuietEnd        string
	ExpectedVersion int64
}

func (s *PreferenceService) Update(ctx context.Context, command UpdatePreferenceCommand) (RecipientPreference, error) {
	if s == nil || s.repository == nil || strings.TrimSpace(command.Principal.ID) == "" || strings.TrimSpace(command.Principal.Scope.MSPID) == "" || command.ExpectedVersion < 0 {
		return RecipientPreference{}, ErrInvalidPreference
	}
	zone, start, end := strings.TrimSpace(command.TimeZone), strings.TrimSpace(command.QuietStart), strings.TrimSpace(command.QuietEnd)
	if zone == "" {
		return RecipientPreference{}, ErrInvalidPreference
	}
	if _, err := time.LoadLocation(zone); err != nil || (start == "") != (end == "") || (start != "" && (!clockValue.MatchString(start) || !clockValue.MatchString(end))) {
		return RecipientPreference{}, ErrInvalidPreference
	}
	availability, err := s.repository.MentionChannelAvailability(ctx, command.Principal.Scope.MSPID)
	if err != nil {
		return RecipientPreference{}, err
	}
	if (command.EmailEnabled && !availability.Email) || (command.TeamsEnabled && !availability.Teams) {
		return RecipientPreference{}, ErrInvalidPreference
	}
	preference := RecipientPreference{
		TechnicianID: command.Principal.ID, EventType: MentionOccurred,
		EmailEnabled: command.EmailEnabled, TeamsEnabled: command.TeamsEnabled,
		TimeZone: zone, QuietStart: start, QuietEnd: end,
		Version:        command.ExpectedVersion + 1,
		EmailAvailable: availability.Email, TeamsAvailable: availability.Teams,
	}
	return s.repository.SaveMentionPreference(ctx, command.Principal.Scope.MSPID, preference, command.ExpectedVersion)
}

func InRecipientQuietHours(at time.Time, preference RecipientPreference) bool {
	if preference.QuietStart == "" || preference.QuietEnd == "" {
		return false
	}
	location, err := time.LoadLocation(preference.TimeZone)
	if err != nil {
		return false
	}
	start, startErr := time.Parse("15:04", preference.QuietStart)
	end, endErr := time.Parse("15:04", preference.QuietEnd)
	if startErr != nil || endErr != nil {
		return false
	}
	local := at.In(location)
	minute := local.Hour()*60 + local.Minute()
	startMinute, endMinute := start.Hour()*60+start.Minute(), end.Hour()*60+end.Minute()
	if startMinute == endMinute {
		return true
	}
	if startMinute < endMinute {
		return minute >= startMinute && minute < endMinute
	}
	return minute >= startMinute || minute < endMinute
}
