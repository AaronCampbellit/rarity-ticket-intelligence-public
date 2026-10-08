package mentions

import (
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
)

func TestOccurrenceRestoresArchivedItemToUnread(t *testing.T) {
	readAt := fixedNow().Add(-2 * time.Hour)
	archivedAt := fixedNow().Add(-time.Hour)
	suppressedAt := fixedNow().Add(-30 * time.Minute)
	item := Item{
		State: Archived, Version: 4, ReadAt: &readAt, ArchivedAt: &archivedAt,
		SuppressedAt: &suppressedAt, SuppressionReason: "access_revoked",
	}
	if err := item.ApplyOccurrence("occurrence-2", fixedNow()); err != nil {
		t.Fatalf("ApplyOccurrence() error = %v", err)
	}
	if item.State != Unread || item.Version != 5 || item.ReadAt != nil || item.ArchivedAt != nil ||
		item.SuppressedAt != nil || item.SuppressionReason != "" ||
		item.LatestOccurrenceID != "occurrence-2" || !item.LastMentionedAt.Equal(fixedNow()) {
		t.Fatalf("item=%+v", item)
	}
}

func TestOccurrenceRejectsInvalidInputWithoutMutation(t *testing.T) {
	old := fixedNow().Add(-time.Hour)
	tests := []struct {
		name         string
		item         Item
		occurrenceID string
		at           time.Time
	}{
		{name: "empty occurrence ID", item: Item{State: Archived, Version: 4, ArchivedAt: &old}, at: fixedNow()},
		{name: "malformed occurrence ID", item: Item{State: Archived, Version: 4, ArchivedAt: &old}, occurrenceID: "bad/id", at: fixedNow()},
		{name: "overlong occurrence ID", item: Item{State: Archived, Version: 4, ArchivedAt: &old}, occurrenceID: strings.Repeat("a", 129), at: fixedNow()},
		{name: "zero timestamp", item: Item{State: Archived, Version: 4, ArchivedAt: &old}, occurrenceID: "occurrence-2"},
		{name: "zero version", item: Item{State: Archived, ArchivedAt: &old}, occurrenceID: "occurrence-2", at: fixedNow()},
		{name: "negative version", item: Item{State: Archived, Version: -1, ArchivedAt: &old}, occurrenceID: "occurrence-2", at: fixedNow()},
		{name: "version overflow", item: Item{State: Archived, Version: math.MaxInt64, ArchivedAt: &old}, occurrenceID: "occurrence-2", at: fixedNow()},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			before := test.item
			if err := test.item.ApplyOccurrence(test.occurrenceID, test.at); !errors.Is(err, ErrInvalidItemState) {
				t.Fatalf("ApplyOccurrence() error = %v, want ErrInvalidItemState", err)
			}
			if test.item != before {
				t.Fatalf("invalid occurrence mutated item: before=%+v after=%+v", before, test.item)
			}
		})
	}
}

func TestOccurrenceAndStateMutationsAreNilSafe(t *testing.T) {
	var item *Item
	if err := item.ApplyOccurrence("occurrence-2", fixedNow()); !errors.Is(err, ErrInvalidItemState) {
		t.Fatalf("nil ApplyOccurrence() error = %v, want ErrInvalidItemState", err)
	}
	if err := item.ApplyState(Read, 1, "tech-1", fixedNow()); !errors.Is(err, ErrInvalidItemState) {
		t.Fatalf("nil ApplyState() error = %v, want ErrInvalidItemState", err)
	}
}

func TestItemStateTransitionsSetOnlyCompatibleTimestamp(t *testing.T) {
	tests := []struct {
		name         string
		next         ItemState
		wantRead     bool
		wantArchived bool
	}{
		{name: "unread", next: Unread},
		{name: "read", next: Read, wantRead: true},
		{name: "archived", next: Archived, wantArchived: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			old := fixedNow().Add(-time.Hour)
			item := Item{State: Read, Version: 7, ReadAt: &old, ArchivedAt: &old}
			if err := item.ApplyState(test.next, 7, "tech-1", fixedNow()); err != nil {
				t.Fatalf("ApplyState() error = %v", err)
			}
			if item.State != test.next || item.Version != 8 ||
				(item.ReadAt != nil) != test.wantRead || (item.ArchivedAt != nil) != test.wantArchived ||
				!item.UpdatedAt.Equal(fixedNow()) || item.UpdatedBy != "tech-1" {
				t.Fatalf("item=%+v", item)
			}
			if item.ReadAt != nil && !item.ReadAt.Equal(fixedNow()) {
				t.Fatalf("read_at=%v", item.ReadAt)
			}
			if item.ArchivedAt != nil && !item.ArchivedAt.Equal(fixedNow()) {
				t.Fatalf("archived_at=%v", item.ArchivedAt)
			}
		})
	}
}

func TestItemStateRejectsStaleVersionWithoutMutation(t *testing.T) {
	item := Item{State: Unread, Version: 3}
	before := item
	if err := item.ApplyState(Read, 2, "tech-1", fixedNow()); !errors.Is(err, object.ErrVersionConflict) {
		t.Fatalf("ApplyState() error = %v, want ErrVersionConflict", err)
	}
	if item != before {
		t.Fatalf("stale transition mutated item: before=%+v after=%+v", before, item)
	}
}

func TestItemStateRejectsInvalidInputWithoutMutation(t *testing.T) {
	tests := []struct {
		name    string
		item    Item
		next    ItemState
		actorID string
		at      time.Time
	}{
		{name: "invalid next state", item: Item{State: Unread, Version: 1}, next: "deleted", actorID: "tech-1", at: fixedNow()},
		{name: "missing actor", item: Item{State: Unread, Version: 1}, next: Read, at: fixedNow()},
		{name: "zero timestamp", item: Item{State: Unread, Version: 1}, next: Read, actorID: "tech-1"},
		{name: "invalid current version", item: Item{State: Unread}, next: Read, actorID: "tech-1", at: fixedNow()},
		{name: "negative current version", item: Item{State: Unread, Version: -1}, next: Read, actorID: "tech-1", at: fixedNow()},
		{name: "version overflow", item: Item{State: Unread, Version: math.MaxInt64}, next: Read, actorID: "tech-1", at: fixedNow()},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			before := test.item
			if err := test.item.ApplyState(test.next, test.item.Version+1, test.actorID, test.at); !errors.Is(err, ErrInvalidItemState) {
				t.Fatalf("ApplyState() error = %v, want ErrInvalidItemState", err)
			}
			if test.item != before {
				t.Fatalf("invalid transition mutated item: before=%+v after=%+v", before, test.item)
			}
		})
	}
}

func fixedNow() time.Time {
	return time.Date(2026, time.August, 8, 12, 34, 56, 0, time.FixedZone("test", -4*60*60))
}
