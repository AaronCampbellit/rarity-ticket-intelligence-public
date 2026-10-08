package mentions

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"html"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/observability"
)

var (
	ErrInvalidWidgetQuery = errors.New("invalid mention widget query")
	ErrInvalidStateChange = errors.New("invalid mention state change")
	ErrInvalidDeepLink    = errors.New("invalid mention deep link")
)

type WidgetQuery struct {
	Principal authorization.Principal
	State     ItemState
	Cursor    string
	Limit     int
}

type StateCounts struct {
	Unread   int `json:"unread"`
	Read     int `json:"read"`
	Archived int `json:"archived"`
}

type WidgetItem struct {
	ID                 string     `json:"id"`
	ParentType         ParentType `json:"parent_type"`
	ParentID           string     `json:"parent_id"`
	ParentDisplayID    string     `json:"parent_display_id"`
	ParentSubject      string     `json:"parent_subject"`
	LatestOccurrenceID string     `json:"latest_occurrence_id"`
	AuthorLabel        string     `json:"author_label"`
	Origin             string     `json:"origin"`
	Preview            string     `json:"preview,omitempty"`
	State              ItemState  `json:"state"`
	LastMentionedAt    time.Time  `json:"last_mentioned_at"`
	Version            int64      `json:"version"`
}

type WidgetPage struct {
	Counts     StateCounts  `json:"counts"`
	Items      []WidgetItem `json:"items"`
	NextCursor string       `json:"next_cursor,omitempty"`
}

// WidgetRecord is an ephemeral repository result. Body is never returned or
// persisted by QueryService; it exists only long enough to build a safe preview.
type WidgetRecord struct {
	Item            WidgetItem
	SourceAvailable bool
	Body            string
	TokenStart      int
	TokenEnd        int
}
type WidgetSnapshot struct {
	Counts StateCounts
	Rows   []WidgetRecord
}
type WidgetStoreQuery struct {
	MSPID, RecipientID string
	State              ItemState
	BeforeAt           time.Time
	BeforeID           string
	Limit              int
}

type StateChange struct {
	Principal       authorization.Principal
	ItemID          string
	State           ItemState
	ExpectedVersion int64
}
type StateStoreChange struct {
	MSPID, RecipientID, ItemID string
	State                      ItemState
	ExpectedVersion            int64
	ChangedAt                  time.Time
}

type DeepLinkQuery struct {
	Principal            authorization.Principal
	ItemID, OccurrenceID string
	ExpectedVersion      int64
}
type DeepLinkStoreQuery struct {
	MSPID, RecipientID, ItemID, OccurrenceID string
	ExpectedVersion                          int64
	ResolvedAt                               time.Time
}
type DeepLink struct {
	Href            string `json:"href"`
	ClientID        string `json:"client_id"`
	ParentType      string `json:"parent_type"`
	ParentID        string `json:"parent_id"`
	SourceID        string `json:"source_id,omitempty"`
	TokenID         string `json:"token_id,omitempty"`
	SourceAvailable bool   `json:"source_available"`
	ItemVersion     int64  `json:"item_version"`
}

type PreviewStoreQuery struct {
	MSPID, RecipientID, ItemID string
}

type PreviewRecord struct {
	SourceAvailable bool
	ParentType      ParentType
	MSPID           string
	ClientID        string
	ParentID        string
	Body            string
	TokenStart      int
	TokenEnd        int
}

type QueryRepository interface {
	AccessRepository
	ListWidget(context.Context, WidgetStoreQuery) (WidgetSnapshot, error)
	ChangeState(context.Context, StateStoreChange) (Item, error)
	ResolveDeepLink(context.Context, DeepLinkStoreQuery) (DeepLink, error)
	LoadPreview(context.Context, PreviewStoreQuery) (PreviewRecord, error)
}

type QueryService struct {
	repository QueryRepository
	cursorKey  []byte
	now        func() time.Time
	telemetry  *observability.MentionTelemetry
}

func (s *QueryService) WithTelemetry(telemetry *observability.MentionTelemetry) *QueryService {
	if s != nil {
		s.telemetry = telemetry
	}
	return s
}

func NewQueryService(repository QueryRepository, cursorKey []byte, now func() time.Time) *QueryService {
	return &QueryService{repository: repository, cursorKey: append([]byte(nil), cursorKey...), now: now}
}

func (s *QueryService) ListCandidates(ctx context.Context, query CandidateQuery) ([]Candidate, error) {
	if s == nil || s.repository == nil {
		return nil, ErrInvalidCandidateQuery
	}
	return NewCandidateService(s.repository).List(ctx, query)
}

func (s *QueryService) ListWidget(ctx context.Context, query WidgetQuery) (WidgetPage, error) {
	empty := WidgetPage{Items: []WidgetItem{}}
	if !s.valid() || !validQueryPrincipal(query.Principal) || !validQueryItemState(query.State) || query.Limit < 1 || query.Limit > 50 {
		return empty, ErrInvalidWidgetQuery
	}
	store := WidgetStoreQuery{MSPID: query.Principal.Scope.MSPID, RecipientID: query.Principal.ID, State: query.State, Limit: query.Limit + 1}
	if query.Cursor != "" {
		cursor, err := s.decodeCursor(query.Cursor)
		if err != nil || cursor.MSPID != store.MSPID || cursor.RecipientID != store.RecipientID || cursor.State != store.State || !validStableID(cursor.ID) || cursor.At.IsZero() {
			return empty, ErrInvalidWidgetQuery
		}
		store.BeforeAt, store.BeforeID = cursor.At, cursor.ID
	}
	snapshot, err := s.repository.ListWidget(ctx, store)
	if err != nil {
		return empty, err
	}
	if snapshot.Counts.Unread < 0 || snapshot.Counts.Read < 0 || snapshot.Counts.Archived < 0 {
		return empty, ErrInvalidWidgetQuery
	}
	page := WidgetPage{Counts: snapshot.Counts, Items: make([]WidgetItem, 0, min(len(snapshot.Rows), query.Limit))}
	for index, row := range snapshot.Rows {
		if index == query.Limit {
			break
		}
		item := row.Item
		if !validWidgetItem(item, query.State) {
			return empty, ErrInvalidWidgetQuery
		}
		if row.SourceAvailable {
			item.Preview = sanitizedPreview(row.Body, row.TokenStart, row.TokenEnd)
		} else {
			item.Preview = ""
		}
		page.Items = append(page.Items, item)
	}
	if len(snapshot.Rows) > query.Limit && len(page.Items) != 0 {
		last := page.Items[len(page.Items)-1]
		page.NextCursor, err = s.encodeCursor(widgetCursor{MSPID: store.MSPID, RecipientID: store.RecipientID, State: store.State, At: last.LastMentionedAt, ID: last.ID})
		if err != nil {
			return empty, ErrInvalidWidgetQuery
		}
	}
	return page, nil
}

func (s *QueryService) ChangeState(ctx context.Context, command StateChange) (Item, error) {
	if !s.valid() || !validQueryPrincipal(command.Principal) || !validStableID(command.ItemID) || !validQueryItemState(command.State) || command.ExpectedVersion < 1 {
		return Item{}, ErrInvalidStateChange
	}
	item, err := s.repository.ChangeState(ctx, StateStoreChange{MSPID: command.Principal.Scope.MSPID, RecipientID: command.Principal.ID, ItemID: command.ItemID, State: command.State, ExpectedVersion: command.ExpectedVersion, ChangedAt: s.now().UTC()})
	if err == nil {
		s.telemetry.Count(observability.MentionMetric{Name: "item_state_transition", ParentType: string(item.ParentType), State: string(item.State), Outcome: string(item.State), MSPID: item.MSPID, ClientID: item.ClientID, ObjectID: item.ParentID})
	}
	return item, err
}

func (s *QueryService) ResolveDeepLink(ctx context.Context, query DeepLinkQuery) (DeepLink, error) {
	if !s.valid() || !validQueryPrincipal(query.Principal) || !validStableID(query.ItemID) || !validStableID(query.OccurrenceID) || query.ExpectedVersion < 1 {
		return DeepLink{}, ErrInvalidDeepLink
	}
	link, err := s.repository.ResolveDeepLink(ctx, DeepLinkStoreQuery{MSPID: query.Principal.Scope.MSPID, RecipientID: query.Principal.ID, ItemID: query.ItemID, OccurrenceID: query.OccurrenceID, ExpectedVersion: query.ExpectedVersion, ResolvedAt: s.now().UTC()})
	if err == nil {
		outcome := "source_unavailable"
		if link.SourceAvailable {
			outcome = "exact_source"
		}
		s.telemetry.Count(observability.MentionMetric{Name: "deep_link", ParentType: link.ParentType, Outcome: outcome, MSPID: query.Principal.Scope.MSPID, ClientID: link.ClientID, ObjectID: link.ParentID, OccurrenceID: query.OccurrenceID})
	}
	return link, err
}

func (s *QueryService) LoadPreview(ctx context.Context, principal authorization.Principal, itemID string) (string, error) {
	if !s.valid() || !validQueryPrincipal(principal) || !validStableID(itemID) {
		return "", ErrInvalidWidgetQuery
	}
	record, err := s.repository.LoadPreview(ctx, PreviewStoreQuery{MSPID: principal.Scope.MSPID, RecipientID: principal.ID, ItemID: itemID})
	if err != nil {
		return "", err
	}
	if !record.SourceAvailable {
		s.telemetry.Count(observability.MentionMetric{Name: "preview", ParentType: string(record.ParentType), Outcome: "unavailable", MSPID: record.MSPID, ClientID: record.ClientID, ObjectID: record.ParentID})
		return "", nil
	}
	s.telemetry.Count(observability.MentionMetric{Name: "preview", ParentType: string(record.ParentType), Outcome: "available", MSPID: record.MSPID, ClientID: record.ClientID, ObjectID: record.ParentID})
	return sanitizedPreview(record.Body, record.TokenStart, record.TokenEnd), nil
}

func (s *QueryService) valid() bool {
	return s != nil && s.repository != nil && len(s.cursorKey) >= 32 && s.now != nil
}
func validQueryPrincipal(principal authorization.Principal) bool {
	return validStableID(principal.ID) && validStableID(principal.Scope.MSPID)
}
func validQueryItemState(state ItemState) bool {
	return state == Unread || state == Read || state == Archived
}
func validWidgetItem(item WidgetItem, state ItemState) bool {
	return validStableID(item.ID) && validParentType(item.ParentType) && validStableID(item.ParentID) && validStableID(item.LatestOccurrenceID) && item.State == state && !item.LastMentionedAt.IsZero() && item.Version > 0
}

type widgetCursor struct {
	MSPID       string    `json:"m"`
	RecipientID string    `json:"r"`
	State       ItemState `json:"s"`
	At          time.Time `json:"a"`
	ID          string    `json:"i"`
}

func (s *QueryService) encodeCursor(cursor widgetCursor) (string, error) {
	payload, err := json.Marshal(cursor)
	if err != nil {
		return "", err
	}
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, s.cursorKey)
	_, _ = mac.Write([]byte(encoded))
	return encoded + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}
func (s *QueryService) decodeCursor(value string) (widgetCursor, error) {
	var cursor widgetCursor
	if len(value) > 1024 {
		return cursor, ErrInvalidWidgetQuery
	}
	parts := strings.Split(value, ".")
	if len(parts) != 2 {
		return cursor, ErrInvalidWidgetQuery
	}
	provided, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return cursor, err
	}
	mac := hmac.New(sha256.New, s.cursorKey)
	_, _ = mac.Write([]byte(parts[0]))
	if !hmac.Equal(provided, mac.Sum(nil)) {
		return cursor, ErrInvalidWidgetQuery
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return cursor, err
	}
	err = json.Unmarshal(payload, &cursor)
	return cursor, err
}

func sanitizeMentionPreviewText(body string) string {
	var builder strings.Builder
	inTag := false
	for _, char := range html.UnescapeString(body) {
		switch char {
		case '<':
			inTag = true
		case '>':
			if inTag {
				inTag = false
				builder.WriteByte(' ')
			}
		default:
			if !inTag {
				builder.WriteRune(char)
			}
		}
	}
	return strings.Join(strings.Fields(builder.String()), " ")
}

func sanitizedPreview(body string, tokenStart, tokenEnd int) string {
	clean := sanitizeMentionPreviewText(body)
	if utf8.RuneCountInString(clean) <= 240 {
		return clean
	}
	runes := []rune(clean)
	center := 0
	boundaries, _ := utf16Boundaries(body)
	startByte, startOK := boundaries[tokenStart]
	endByte, endOK := boundaries[tokenEnd]
	if startOK && endOK && tokenEnd > tokenStart {
		prefix := sanitizeMentionPreviewText(body[:startByte])
		token := sanitizeMentionPreviewText(body[startByte:endByte])
		center = utf8.RuneCountInString(prefix) + utf8.RuneCountInString(token)/2
	}
	start := center - 120
	if start < 0 {
		start = 0
	}
	if start+240 > len(runes) {
		start = len(runes) - 240
	}
	return strings.TrimSpace(string(runes[start : start+240]))
}
