package adapters

import (
	"context"
	"crypto/sha256"
	"fmt"
	"sort"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/calendar"
)

type TagResolver interface {
	ResolveCalendarTags(context.Context, calendar.SourceRef) ([]string, error)
}

// ProjectionRevisionResolver returns one monotonic revision that covers every
// authoritative input read by an adapter, including independently versioned
// classification and SLA records.
type ProjectionRevisionResolver interface {
	ResolveCalendarProjectionRevision(context.Context, calendar.SourceRef, int64) (int64, error)
}

func resolveProjectionRevision(ctx context.Context, source any, ref calendar.SourceRef, base int64) (int64, error) {
	if resolver, ok := source.(ProjectionRevisionResolver); ok {
		return resolver.ResolveCalendarProjectionRevision(ctx, ref, base)
	}
	return base, nil
}

func resolveTags(ctx context.Context, source any, ref calendar.SourceRef) ([]string, error) {
	if resolver, ok := source.(TagResolver); ok {
		return resolver.ResolveCalendarTags(ctx, ref)
	}
	return nil, nil
}

func projectionID(source calendar.SourceRef, role, sourceRole string) string {
	digest := sha256.Sum256([]byte(source.MSPID + "\x00" + source.Type + "\x00" + source.ID + "\x00" + role + "\x00" + sourceRole))
	digest[6] = (digest[6] & 0x0f) | 0x50
	digest[8] = (digest[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", digest[0:4], digest[4:6], digest[6:8], digest[8:10], digest[10:16])
}

func allDayProjection(source calendar.SourceRef, revision int64, title, role string, date time.Time) calendar.Projection {
	date = dateOnly(date)
	return calendar.Projection{ID: projectionID(source, role, ""), EventRole: role, Source: source, SourceRevision: revision, Title: title, AllDay: true, StartsOn: &date, SchedulingMode: calendar.Informational, TerminalState: calendar.Active}
}
func timedProjection(source calendar.SourceRef, revision int64, title, role string, start, end *time.Time, timezone string, mode calendar.SchedulingMode, recurrence *calendar.RecurrenceRule) calendar.Projection {
	return calendar.Projection{ID: projectionID(source, role, ""), EventRole: role, Source: source, SourceRevision: revision, Title: title, StartsAt: start, EndsAt: end, Timezone: timezone, SchedulingMode: mode, Recurrence: recurrence, TerminalState: calendar.Active}
}
func dateOnly(value time.Time) time.Time {
	y, m, d := value.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}
func sortProjections(values []calendar.Projection) {
	sort.Slice(values, func(i, j int) bool {
		if values[i].EventRole == values[j].EventRole {
			return values[i].SourceRoleKey < values[j].SourceRoleKey
		}
		return values[i].EventRole < values[j].EventRole
	})
}
func terminalState(status string) calendar.TerminalState {
	switch status {
	case "completed", "resolved", "closed", "renewed", "expired":
		return calendar.Completed
	case "cancelled", "rejected":
		return calendar.Cancelled
	default:
		return calendar.Active
	}
}
