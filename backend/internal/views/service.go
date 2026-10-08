// Package views owns permission-safe saved searches and dashboards.
package views

import (
	"context"
	"errors"
	"strings"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

var ErrInvalid = errors.New("invalid saved view")

type Kind string

const (
	SavedSearch      Kind = "saved_search"
	Dashboard        Kind = "dashboard"
	KindCalendarLens Kind = "calendar_lens"
)

type AudienceType string

const (
	Private    AudienceType = "private"
	Team       AudienceType = "team"
	Department AudienceType = "department"
	Queue      AudienceType = "queue"
	MSP        AudienceType = "msp"
)

type Audience struct {
	Type AudienceType `json:"type"`
	ID   string       `json:"id,omitempty"`
}

type View struct {
	ID       string         `json:"id"`
	MSPID    string         `json:"msp_id"`
	OwnerID  string         `json:"owner_id"`
	Kind     Kind           `json:"kind"`
	Name     string         `json:"name"`
	Query    map[string]any `json:"query"`
	Audience Audience       `json:"audience"`
	Version  int64          `json:"version"`
}

type SaveCommand struct {
	Principal authorization.Principal
	OwnerID   string
	Kind      Kind
	Name      string
	Query     map[string]any
	Audience  Audience
}

type ResolveCommand struct {
	Principal authorization.Principal
	Target    scope.Target
	ViewID    string
}

type ListCommand struct {
	Principal authorization.Principal
	Target    scope.Target
	Kind      Kind
}

type Resolved struct {
	ViewID string         `json:"view_id"`
	Kind   Kind           `json:"kind"`
	Query  map[string]any `json:"query"`
}

type Repository interface {
	Save(context.Context, View) error
	Find(context.Context, string, string) (View, error)
	List(context.Context, string, Kind) ([]View, error)
}

type authorizedClientRepository interface {
	AuthorizedViewClientIDs(context.Context, authorization.Principal) ([]string, error)
}

func (s *Service) List(
	ctx context.Context,
	command ListCommand,
) ([]View, error) {
	if command.Kind != SavedSearch && command.Kind != Dashboard && command.Kind != KindCalendarLens {
		return nil, ErrInvalid
	}
	target := command.Target
	if target.MSPID == "" {
		target = scope.Target{
			MSPID:    command.Principal.Scope.MSPID,
			ClientID: command.Principal.Scope.ClientID,
		}
	}
	if target.ClientID == "" && command.Kind != KindCalendarLens {
		return nil, ErrInvalid
	}
	capability := "search.read"
	if command.Kind == Dashboard {
		capability = "dashboard.read"
	} else if command.Kind == KindCalendarLens {
		capability = "calendar.read"
		command.Principal.Scope.ClientID = ""
		target.ClientID = ""
	}
	if err := authorization.Authorize(command.Principal, capability, target); err != nil {
		return nil, err
	}
	found, err := s.repository.List(ctx, target.MSPID, command.Kind)
	if err != nil {
		return nil, err
	}
	visible := make([]View, 0, len(found))
	for _, view := range found {
		if canReadAudience(command.Principal, view) {
			view.Query = cloneQuery(view.Query)
			if command.Kind == KindCalendarLens {
				view.Query, err = s.resolveCalendarLensQuery(ctx, command.Principal, view.Query)
				if err != nil {
					return nil, err
				}
			} else {
				view.Query["msp_id"] = target.MSPID
				view.Query["client_id"] = target.ClientID
			}
			visible = append(visible, view)
		}
	}
	return visible, nil
}

type Service struct {
	repository Repository
	newID      func() string
}

func NewService(repository Repository, newID func() string) *Service {
	return &Service{repository: repository, newID: newID}
}

func (s *Service) Save(ctx context.Context, command SaveCommand) (View, error) {
	if command.Principal.Scope.MSPID == "" ||
		command.OwnerID == "" ||
		(command.Kind != SavedSearch && command.Kind != Dashboard && command.Kind != KindCalendarLens) ||
		strings.TrimSpace(command.Name) == "" ||
		len(command.Query) == 0 ||
		!validAudience(command.Audience) {
		return View{}, ErrInvalid
	}
	target := scope.Target{
		MSPID: command.Principal.Scope.MSPID, ClientID: command.Principal.Scope.ClientID,
	}
	if err := authorization.Authorize(command.Principal, "view.save", target); err != nil {
		return View{}, err
	}
	if command.Audience.Type != Private {
		if err := authorization.Authorize(command.Principal, "view.share", target); err != nil {
			return View{}, err
		}
	}
	view := View{
		ID: s.newID(), MSPID: target.MSPID, OwnerID: command.OwnerID,
		Kind: command.Kind, Name: strings.TrimSpace(command.Name),
		Query: cloneQuery(command.Query), Audience: command.Audience, Version: 1,
	}
	delete(view.Query, "client_id")
	delete(view.Query, "msp_id")
	if view.Kind == KindCalendarLens {
		if !validCalendarLensQuery(view.Query) {
			return View{}, ErrInvalid
		}
	}
	if err := s.repository.Save(ctx, view); err != nil {
		return View{}, err
	}
	return view, nil
}

func (s *Service) Resolve(ctx context.Context, command ResolveCommand) (Resolved, error) {
	if command.ViewID == "" {
		return Resolved{}, ErrInvalid
	}
	view, err := s.repository.Find(ctx, command.Principal.Scope.MSPID, command.ViewID)
	if err != nil {
		return Resolved{}, err
	}
	if view.MSPID != command.Principal.Scope.MSPID || !canReadAudience(command.Principal, view) {
		return Resolved{}, scope.ErrNotFound
	}
	target := command.Target
	if target.MSPID == "" {
		target = scope.Target{
			MSPID: command.Principal.Scope.MSPID, ClientID: command.Principal.Scope.ClientID,
		}
	}
	if target.ClientID == "" && view.Kind != KindCalendarLens {
		return Resolved{}, ErrInvalid
	}
	capability := "search.read"
	if view.Kind == Dashboard {
		capability = "dashboard.read"
	} else if view.Kind == KindCalendarLens {
		capability = "calendar.read"
		command.Principal.Scope.ClientID = ""
		target.ClientID = ""
	}
	if err := authorization.Authorize(command.Principal, capability, target); err != nil {
		return Resolved{}, err
	}
	query := cloneQuery(view.Query)
	if view.Kind == KindCalendarLens {
		query, err = s.resolveCalendarLensQuery(ctx, command.Principal, query)
		if err != nil {
			return Resolved{}, err
		}
	} else {
		query["msp_id"] = target.MSPID
		query["client_id"] = target.ClientID
	}
	return Resolved{ViewID: view.ID, Kind: view.Kind, Query: query}, nil
}

var calendarLensKeys = map[string]string{
	"technician_ids": "strings", "owner_ids": "strings", "team_ids": "strings",
	"client_ids": "strings", "technology_ids": "strings", "project_ids": "strings",
	"phase_ids": "strings", "sla_ids": "strings", "ticket_types": "strings",
	"tag_ids": "strings", "priorities": "strings", "event_roles": "strings",
	"health_states": "strings", "conflicts_only": "bool", "lens": "string",
	"viewer_timezone": "string", "source_types": "strings", "scheduling_modes": "strings",
	"terminal_states": "strings",
}

func validCalendarLensQuery(query map[string]any) bool {
	if len(query) == 0 {
		return false
	}
	lens, ok := query["lens"].(string)
	if !ok || !canonicalCalendarLenses[strings.TrimSpace(lens)] {
		return false
	}
	for key, value := range query {
		kind, ok := calendarLensKeys[key]
		if !ok {
			return false
		}
		switch kind {
		case "bool":
			if _, ok = value.(bool); !ok {
				return false
			}
		case "string":
			if v, ok := value.(string); !ok || strings.TrimSpace(v) == "" {
				return false
			}
		case "strings":
			if _, ok := stringSlice(value); !ok {
				return false
			}
		}
	}
	return true
}

var canonicalCalendarLenses = map[string]bool{
	"day": true, "week": true, "month": true,
	"timeline": true, "capacity": true, "agenda": true,
}

func stringSlice(value any) ([]string, bool) {
	switch values := value.(type) {
	case []string:
		result := make([]string, 0, len(values))
		for _, v := range values {
			v = strings.TrimSpace(v)
			if v == "" {
				return nil, false
			}
			result = append(result, v)
		}
		return result, true
	case []any:
		result := make([]string, 0, len(values))
		for _, raw := range values {
			v, ok := raw.(string)
			v = strings.TrimSpace(v)
			if !ok || v == "" {
				return nil, false
			}
			result = append(result, v)
		}
		return result, true
	default:
		return nil, false
	}
}

func (s *Service) resolveCalendarLensQuery(ctx context.Context, principal authorization.Principal, query map[string]any) (map[string]any, error) {
	if !validCalendarLensQuery(query) {
		return nil, ErrInvalid
	}
	resolved := cloneQuery(query)
	requested, exists := resolved["client_ids"]
	if !exists {
		return resolved, nil
	}
	values, _ := stringSlice(requested)
	allowed := map[string]bool{}
	if repository, ok := s.repository.(authorizedClientRepository); ok {
		ids, err := repository.AuthorizedViewClientIDs(ctx, principal)
		if err != nil {
			return nil, err
		}
		for _, id := range ids {
			allowed[id] = true
		}
	} else {
		for dataScope := range principal.DataScopes {
			if strings.HasPrefix(dataScope, "client:") {
				allowed[strings.TrimPrefix(dataScope, "client:")] = true
			}
		}
	}
	filtered := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, id := range values {
		if allowed[id] && !seen[id] {
			seen[id] = true
			filtered = append(filtered, id)
		}
	}
	resolved["client_ids"] = filtered
	return resolved, nil
}

func validAudience(audience Audience) bool {
	switch audience.Type {
	case Private, MSP:
		return audience.ID == ""
	case Team, Department, Queue:
		return audience.ID != ""
	default:
		return false
	}
}

func canReadAudience(principal authorization.Principal, view View) bool {
	switch view.Audience.Type {
	case Private:
		return principal.ID != "" && principal.ID == view.OwnerID
	case MSP:
		return true
	case Team, Department, Queue:
		return principal.Capabilities.Has(
			"audience." + string(view.Audience.Type) + ":" + view.Audience.ID,
		)
	default:
		return false
	}
}

func cloneQuery(query map[string]any) map[string]any {
	cloned := make(map[string]any, len(query))
	for key, value := range query {
		cloned[key] = value
	}
	return cloned
}
