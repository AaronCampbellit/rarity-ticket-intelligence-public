package mentions

import (
	"math"
	"strings"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
)

func (item *Item) ApplyState(next ItemState, expectedVersion int64, actorID string, at time.Time) error {
	if item == nil || !validItemState(next) || item.Version < 1 || item.Version == math.MaxInt64 ||
		strings.TrimSpace(actorID) == "" || at.IsZero() {
		return ErrInvalidItemState
	}
	if err := object.RequireVersion(item.Version, expectedVersion); err != nil {
		return err
	}

	changedAt := at.UTC()
	item.State = next
	item.ReadAt = nil
	item.ArchivedAt = nil
	switch next {
	case Read:
		item.ReadAt = timePointer(changedAt)
	case Archived:
		item.ArchivedAt = timePointer(changedAt)
	}
	item.Version++
	item.UpdatedAt = changedAt
	item.UpdatedBy = strings.TrimSpace(actorID)
	return nil
}

func (item *Item) ApplyOccurrence(occurrenceID string, at time.Time) error {
	if item == nil || !validStableID(occurrenceID) || at.IsZero() ||
		item.Version < 1 || item.Version == math.MaxInt64 {
		return ErrInvalidItemState
	}
	mentionedAt := at.UTC()
	item.LatestOccurrenceID = occurrenceID
	item.State = Unread
	item.ReadAt = nil
	item.ArchivedAt = nil
	item.LastMentionedAt = mentionedAt
	item.SuppressedAt = nil
	item.SuppressionReason = ""
	item.Version++
	item.UpdatedAt = mentionedAt
	return nil
}

func validItemState(state ItemState) bool {
	return state == Unread || state == Read || state == Archived
}

func timePointer(value time.Time) *time.Time {
	return &value
}
