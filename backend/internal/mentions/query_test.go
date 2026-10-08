package mentions

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/observability"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

func TestMentionWidgetBindsCursorToRecipientAndStateAndSanitizesPreview(t *testing.T) {
	now := time.Date(2026, time.August, 8, 12, 0, 0, 0, time.UTC)
	repository := &queryRepositoryStub{snapshot: WidgetSnapshot{
		Counts: StateCounts{Unread: 2, Read: 1},
		Rows: []WidgetRecord{{Item: WidgetItem{
			ID: "item-1", ParentType: ParentWorkRecord, ParentID: "work-1",
			LatestOccurrenceID: "occurrence-1", State: Unread,
			LastMentionedAt: now, Version: 3,
		}, SourceAvailable: true, Body: "<b>VPN</b> is down\n\tfor the executive", TokenStart: 3, TokenEnd: 6}, {Item: WidgetItem{
			ID: "item-2", ParentType: ParentWorkRecord, ParentID: "work-2",
			LatestOccurrenceID: "occurrence-2", State: Unread,
			LastMentionedAt: now.Add(-time.Minute), Version: 1,
		}}},
	}}
	service := NewQueryService(repository, []byte("a cursor signing key with enough entropy"), func() time.Time { return now })
	principal := queryPrincipal("staff-1")
	page, err := service.ListWidget(context.Background(), WidgetQuery{Principal: principal, State: Unread, Limit: 1})
	if err != nil || len(page.Items) != 1 || page.Counts.Unread != 2 {
		t.Fatalf("ListWidget() page=%+v err=%v", page, err)
	}
	if page.Items[0].Preview != "VPN is down for the executive" || utf8.RuneCountInString(page.Items[0].Preview) > 240 {
		t.Fatalf("unsafe preview %q", page.Items[0].Preview)
	}
	if page.NextCursor == "" {
		t.Fatal("missing signed cursor")
	}
	_, err = service.ListWidget(context.Background(), WidgetQuery{Principal: queryPrincipal("staff-2"), State: Unread, Cursor: page.NextCursor, Limit: 1})
	if !errors.Is(err, ErrInvalidWidgetQuery) {
		t.Fatalf("cross-recipient cursor error=%v", err)
	}
	_, err = service.ListWidget(context.Background(), WidgetQuery{Principal: principal, State: Archived, Cursor: page.NextCursor, Limit: 1})
	if !errors.Is(err, ErrInvalidWidgetQuery) {
		t.Fatalf("cross-state cursor error=%v", err)
	}
	tampered := page.NextCursor[:len(page.NextCursor)-1] + "A"
	if _, err = service.ListWidget(context.Background(), WidgetQuery{Principal: principal, State: Unread, Cursor: tampered, Limit: 1}); !errors.Is(err, ErrInvalidWidgetQuery) {
		t.Fatalf("tampered cursor error=%v", err)
	}
}

func TestMentionQueriesEmitStatePreviewAndDeepLinkOutcomeCounters(t *testing.T) {
	telemetry := observability.NewMentionTelemetry(nil)
	repository := &queryRepositoryStub{
		changed:  Item{ID: "item", RecipientID: "staff", ParentType: ParentProject, ParentID: "project", State: Read, Version: 2},
		link:     DeepLink{Href: "#/project", ParentType: string(ParentProject), ParentID: "project", SourceAvailable: false, ItemVersion: 3},
		snapshot: WidgetSnapshot{Rows: []WidgetRecord{{Item: WidgetItem{ID: "item", ParentType: ParentProject}, SourceAvailable: false}}},
	}
	service := NewQueryService(repository, []byte("a cursor signing key with enough entropy"), time.Now).WithTelemetry(telemetry)
	principal := queryPrincipal("staff")
	if _, err := service.ChangeState(context.Background(), StateChange{Principal: principal, ItemID: "item", State: Read, ExpectedVersion: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.LoadPreview(context.Background(), principal, "item"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ResolveDeepLink(context.Background(), DeepLinkQuery{Principal: principal, ItemID: "item", OccurrenceID: "occurrence", ExpectedVersion: 2}); err != nil {
		t.Fatal(err)
	}
	if telemetry.Value("item_state_transition", "project", "read") != 1 ||
		telemetry.Value("preview", "project", "unavailable") != 1 ||
		telemetry.Value("deep_link", "project", "source_unavailable") != 1 {
		t.Fatalf("missing query telemetry")
	}
}

func TestMentionWidgetValidatesBoundsAndSuppressesUnavailablePreview(t *testing.T) {
	repository := &queryRepositoryStub{snapshot: WidgetSnapshot{Rows: []WidgetRecord{{
		Item:            WidgetItem{ID: "item", ParentType: ParentProject, ParentID: "project", LatestOccurrenceID: "occurrence", State: Read, LastMentionedAt: time.Now(), Version: 1},
		SourceAvailable: false, Body: "must not escape",
	}}}}
	service := NewQueryService(repository, []byte("a cursor signing key with enough entropy"), time.Now)
	for _, limit := range []int{0, 51} {
		if _, err := service.ListWidget(context.Background(), WidgetQuery{Principal: queryPrincipal("staff"), State: Read, Limit: limit}); !errors.Is(err, ErrInvalidWidgetQuery) {
			t.Fatalf("limit %d error=%v", limit, err)
		}
	}
	page, err := service.ListWidget(context.Background(), WidgetQuery{Principal: queryPrincipal("staff"), State: Read, Limit: 50})
	if err != nil || page.Items[0].Preview != "" {
		t.Fatalf("unavailable source leaked: page=%+v err=%v", page, err)
	}
}

func TestMentionStateAndDeepLinkAreRecipientOwnedAndVersioned(t *testing.T) {
	repository := &queryRepositoryStub{
		changed: Item{ID: "item", RecipientID: "staff", State: Archived, Version: 4},
		link:    DeepLink{Href: "/tickets/work?mention=occurrence", ParentType: string(ParentWorkRecord), ParentID: "work", SourceID: "source", SourceAvailable: true, ItemVersion: 5},
	}
	service := NewQueryService(repository, []byte("a cursor signing key with enough entropy"), time.Now)
	principal := queryPrincipal("staff")
	item, err := service.ChangeState(context.Background(), StateChange{Principal: principal, ItemID: "item", State: Archived, ExpectedVersion: 3})
	if err != nil || item.Version != 4 || repository.state.RecipientID != "staff" {
		t.Fatalf("ChangeState() item=%+v command=%+v err=%v", item, repository.state, err)
	}
	link, err := service.ResolveDeepLink(context.Background(), DeepLinkQuery{Principal: principal, ItemID: "item", OccurrenceID: "occurrence", ExpectedVersion: 4})
	if err != nil || !link.SourceAvailable || repository.deep.RecipientID != "staff" {
		t.Fatalf("ResolveDeepLink() link=%+v query=%+v err=%v", link, repository.deep, err)
	}
	if _, err := service.ChangeState(context.Background(), StateChange{Principal: principal, ItemID: "item", State: Unread, ExpectedVersion: 0}); !errors.Is(err, ErrInvalidStateChange) {
		t.Fatalf("invalid version error=%v", err)
	}
	repository.err = object.ErrVersionConflict
	if _, err := service.ResolveDeepLink(context.Background(), DeepLinkQuery{Principal: principal, ItemID: "item", OccurrenceID: "occurrence", ExpectedVersion: 4}); !errors.Is(err, object.ErrVersionConflict) {
		t.Fatalf("version conflict hidden: %v", err)
	}
}

func TestMentionCandidatesUseSamePermissionRepository(t *testing.T) {
	repository := &queryRepositoryStub{candidates: []Candidate{{TargetType: TargetStaff, ID: "staff-2", Label: "Taylor", Version: 1, MSPID: "msp", Active: true, Internal: true, HasMentionRead: true, CanRead: true}}}
	service := NewQueryService(repository, []byte("a cursor signing key with enough entropy"), time.Now)
	rows, err := service.ListCandidates(context.Background(), CandidateQuery{AuthorID: "staff-1", Source: SourceRef{MSPID: "msp", ClientID: "client", ParentType: ParentWorkRecord, ParentID: "work", SourceKind: SourceComment}})
	if err != nil || len(rows) != 1 || rows[0].ID != "staff-2" {
		t.Fatalf("ListCandidates()=%+v err=%v", rows, err)
	}
}

func queryPrincipal(id string) authorization.Principal {
	return authorization.Principal{ID: id, Scope: scope.Principal{MSPID: "msp"}}
}

type queryRepositoryStub struct {
	snapshot   WidgetSnapshot
	changed    Item
	link       DeepLink
	candidates []Candidate
	state      StateStoreChange
	deep       DeepLinkStoreQuery
	err        error
}

func (r *queryRepositoryStub) ListWidget(context.Context, WidgetStoreQuery) (WidgetSnapshot, error) {
	return r.snapshot, r.err
}
func (r *queryRepositoryStub) ChangeState(_ context.Context, command StateStoreChange) (Item, error) {
	r.state = command
	return r.changed, r.err
}
func (r *queryRepositoryStub) ResolveDeepLink(_ context.Context, query DeepLinkStoreQuery) (DeepLink, error) {
	r.deep = query
	return r.link, r.err
}
func (r *queryRepositoryStub) LoadPreview(context.Context, PreviewStoreQuery) (PreviewRecord, error) {
	if len(r.snapshot.Rows) == 0 {
		return PreviewRecord{}, r.err
	}
	row := r.snapshot.Rows[0]
	return PreviewRecord{SourceAvailable: row.SourceAvailable, ParentType: row.Item.ParentType, Body: row.Body, TokenStart: row.TokenStart, TokenEnd: row.TokenEnd}, r.err
}
func (r *queryRepositoryStub) ListCandidates(context.Context, CandidateQuery) ([]Candidate, error) {
	return r.candidates, r.err
}
func (r *queryRepositoryStub) LoadDirectAccess(context.Context, SourceRef, []string) ([]MemberAccess, error) {
	return nil, r.err
}
func (r *queryRepositoryStub) LoadTeamAccess(context.Context, SourceRef, []string) ([]TeamAccess, error) {
	return nil, r.err
}

func TestSanitizedMentionPreviewUsesUnicodeLimit(t *testing.T) {
	value := sanitizedPreview(strings.Repeat("界", 300), 0, 1)
	if utf8.RuneCountInString(value) != 240 {
		t.Fatalf("runes=%d", utf8.RuneCountInString(value))
	}
}

func TestSanitizedMentionPreviewCentersPersistedUTF16SpanNotFirstLabel(t *testing.T) {
	prefix := "@Alex wrong context " + strings.Repeat("前", 260) + " 🚀 "
	body := prefix + "@Alex ACTUAL CONTEXT " + strings.Repeat("後", 260)
	start := len(utf16.Encode([]rune(prefix)))
	end := start + len(utf16.Encode([]rune("@Alex")))
	value := sanitizedPreview(body, start, end)
	if !strings.Contains(value, "ACTUAL CONTEXT") || strings.Contains(value, "wrong context") || utf8.RuneCountInString(value) > 240 {
		t.Fatalf("preview centered on wrong repeated label: %q", value)
	}
}

func TestLoadPreviewSuppressesRemovedOrRedactedSource(t *testing.T) {
	repository := &queryRepositoryStub{snapshot: WidgetSnapshot{Rows: []WidgetRecord{{
		Item: WidgetItem{ID: "item"}, SourceAvailable: false, Body: "redacted secret",
	}}}}
	service := NewQueryService(repository, []byte("a cursor signing key with enough entropy"), time.Now)
	preview, err := service.LoadPreview(context.Background(), queryPrincipal("staff"), "item")
	if err != nil || preview != "" {
		t.Fatalf("unavailable preview=%q err=%v", preview, err)
	}
}
