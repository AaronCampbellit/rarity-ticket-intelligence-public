package calendar

import "time"

// TypedInterval preserves date-only versus timed source semantics. An empty
// interval is valid when the owning source has another authoritative date.
type TypedInterval struct {
	AllDay           bool
	StartsOn, EndsOn *time.Time
	StartsAt, EndsAt *time.Time
	Timezone         string
}

func (v TypedInterval) Empty() bool {
	return v.StartsOn == nil && v.EndsOn == nil && v.StartsAt == nil && v.EndsAt == nil && v.Timezone == ""
}
func (v TypedInterval) Validate(requireEnd bool) error {
	if v.Empty() {
		return nil
	}
	if v.AllDay {
		if v.StartsOn == nil || v.StartsAt != nil || v.EndsAt != nil || v.Timezone != "" || !isDate(*v.StartsOn) || v.EndsOn != nil && (!isDate(*v.EndsOn) || v.EndsOn.Before(*v.StartsOn)) {
			return ErrInvalidProjection
		}
		return nil
	}
	if v.StartsAt == nil || v.StartsOn != nil || v.EndsOn != nil || !validTimezone(v.Timezone) || requireEnd && v.EndsAt == nil || v.EndsAt != nil && !v.EndsAt.After(*v.StartsAt) {
		return ErrInvalidProjection
	}
	return nil
}
func NormalizeDatePointer(v *time.Time) *time.Time {
	if v == nil {
		return nil
	}
	y, m, d := v.Date()
	n := time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	return &n
}
