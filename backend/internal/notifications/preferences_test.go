package notifications

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

type preferenceRepository struct {
	current      RecipientPreference
	availability ChannelAvailability
	saved        RecipientPreference
	expected     int64
}

func (r *preferenceRepository) GetMentionPreference(context.Context, string, string) (RecipientPreference, error) {
	return r.current, nil
}
func (r *preferenceRepository) MentionChannelAvailability(context.Context, string) (ChannelAvailability, error) {
	return r.availability, nil
}
func (r *preferenceRepository) SaveMentionPreference(_ context.Context, _ string, preference RecipientPreference, expected int64) (RecipientPreference, error) {
	r.saved, r.expected = preference, expected
	return preference, nil
}

func TestPreferenceServiceValidatesIANAQuietHoursAndOrganizationAvailability(t *testing.T) {
	repository := &preferenceRepository{current: DefaultMentionPreference("tech"), availability: ChannelAvailability{Email: true}}
	service := NewPreferenceService(repository)
	principal := authorization.Principal{ID: "tech", Scope: scope.Principal{MSPID: "msp"}}
	_, err := service.Update(context.Background(), UpdatePreferenceCommand{Principal: principal, EmailEnabled: true, TeamsEnabled: true, TimeZone: "America/New_York", QuietStart: "22:00", QuietEnd: "06:00", ExpectedVersion: 0})
	if !errors.Is(err, ErrInvalidPreference) {
		t.Fatalf("unavailable Teams enablement error=%v", err)
	}
	_, err = service.Update(context.Background(), UpdatePreferenceCommand{Principal: principal, EmailEnabled: true, TimeZone: "not/a-zone", ExpectedVersion: 0})
	if !errors.Is(err, ErrInvalidPreference) {
		t.Fatalf("invalid zone error=%v", err)
	}
	_, err = service.Update(context.Background(), UpdatePreferenceCommand{Principal: principal, EmailEnabled: true, TimeZone: "", ExpectedVersion: 0})
	if !errors.Is(err, ErrInvalidPreference) {
		t.Fatalf("missing zone error=%v", err)
	}
	updated, err := service.Update(context.Background(), UpdatePreferenceCommand{Principal: principal, EmailEnabled: true, TeamsEnabled: false, TimeZone: "America/New_York", QuietStart: "22:00", QuietEnd: "06:00", ExpectedVersion: 0})
	if err != nil || updated.TechnicianID != "tech" || repository.saved.EventType != MentionOccurred {
		t.Fatalf("updated=%+v saved=%+v err=%v", updated, repository.saved, err)
	}
}

func TestInRecipientQuietHoursHandlesOvernightAndDST(t *testing.T) {
	preference := RecipientPreference{TimeZone: "America/New_York", QuietStart: "22:00", QuietEnd: "06:00"}
	for _, instant := range []time.Time{
		time.Date(2026, 3, 8, 6, 30, 0, 0, time.UTC),  // 01:30 before spring-forward.
		time.Date(2026, 11, 1, 6, 30, 0, 0, time.UTC), // 01:30 after fall-back.
	} {
		if !InRecipientQuietHours(instant, preference) {
			t.Fatalf("instant %s should be quiet", instant)
		}
	}
	if InRecipientQuietHours(time.Date(2026, 8, 6, 17, 0, 0, 0, time.UTC), preference) {
		t.Fatal("13:00 local should not be quiet")
	}
}
