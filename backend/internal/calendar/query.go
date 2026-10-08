package calendar

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

var (
	ErrInvalidCalendarQuery = errors.New("invalid calendar query")
	ErrWindowTooLarge       = errors.New("window_too_large")
	ErrResultTooLarge       = errors.New("result_too_large")
	ErrInvalidAgendaCursor  = errors.New("invalid calendar agenda cursor")
)

const (
	maximumCalendarQueryWindow = 366 * 24 * time.Hour
	maximumCalendarResults     = 10_000
	maximumCalendarPage        = 1_000
	defaultCalendarPage        = 250
)

type QueryProjection struct {
	Projection  Projection
	Exceptions  []RecurrenceException
	Health      HealthResult
	HasConflict bool
}

type CalendarQueryRepository interface {
	AuthorizedCalendarClientIDs(context.Context, authorization.Principal) ([]string, error)
	ListCalendarProjections(context.Context, authorization.Principal, []string, QueryWindow, Filter, QueryVisibility, int) ([]QueryProjection, error)
}

type QueryVisibility string

const (
	QueryVisibilityFull QueryVisibility = "full"
	QueryVisibilityBusy QueryVisibility = "busy"
)

type CalendarQueryAvailabilityRepository interface {
	LoadCalendarQueryAvailability(context.Context, string, []string, QueryWindow) (map[string]ResolvedAvailability, error)
}

type CalendarRecurrenceExpander interface {
	ExpandCalendarOccurrences(context.Context, Projection, QueryWindow, []RecurrenceException) ([]Occurrence, error)
}

type EventView struct {
	ProjectionID    string            `json:"projection_id,omitempty"`
	OccurrenceKey   string            `json:"occurrence_key,omitempty"`
	SourceRevision  int64             `json:"source_revision,omitempty"`
	HealthReasons   []HealthReason    `json:"health_reasons,omitempty"`
	HasConflict     bool              `json:"has_conflict,omitempty"`
	Recurrence      *RecurrenceRule   `json:"recurrence,omitempty"`
	PlannedMinutes  int64             `json:"planned_minutes,omitempty"`
	CapacityBearing bool              `json:"capacity_bearing,omitempty"`
	ID              string            `json:"id"`
	OccurrenceID    string            `json:"occurrence_id"`
	EventRole       string            `json:"event_role,omitempty"`
	Title           string            `json:"title"`
	Privacy         PrivacyMode       `json:"privacy"`
	AllDay          bool              `json:"all_day"`
	StartsOn        *time.Time        `json:"starts_on,omitempty"`
	EndsOn          *time.Time        `json:"ends_on,omitempty"`
	StartsAt        *time.Time        `json:"starts_at,omitempty"`
	EndsAt          *time.Time        `json:"ends_at,omitempty"`
	Timezone        string            `json:"timezone,omitempty"`
	OwnerID         string            `json:"owner_id,omitempty"`
	AssigneeID      string            `json:"assignee_id,omitempty"`
	Source          SourceRef         `json:"source,omitempty"`
	Tags            []string          `json:"tags,omitempty"`
	TechnologyIDs   []string          `json:"technology_ids,omitempty"`
	ProjectIDs      []string          `json:"project_ids,omitempty"`
	TeamIDs         []string          `json:"team_ids,omitempty"`
	Priority        string            `json:"priority,omitempty"`
	Health          HealthState       `json:"health,omitempty"`
	SchedulingMode  SchedulingMode    `json:"scheduling_mode,omitempty"`
	Capabilities    EventCapabilities `json:"capabilities"`
}

type QueryRequest struct {
	Principal authorization.Principal
	Window    QueryWindow
	Filter    Filter
	Cursor    string
	Limit     int
}
type QueryPage struct {
	Events     []EventView `json:"events"`
	NextCursor string      `json:"next_cursor,omitempty"`
}
type FilterOptionCounts struct {
	Owners       map[string]int `json:"owners"`
	Phases       map[string]int `json:"phases"`
	SLAs         map[string]int `json:"slas"`
	TicketTypes  map[string]int `json:"ticket_types"`
	Technicians  map[string]int `json:"technicians"`
	Clients      map[string]int `json:"clients"`
	Tags         map[string]int `json:"tags"`
	Technologies map[string]int `json:"technologies"`
	Projects     map[string]int `json:"projects"`
	Teams        map[string]int `json:"teams"`
	Priorities   map[string]int `json:"priorities"`
	EventRoles   map[string]int `json:"event_roles"`
}

type QueryService struct {
	repository CalendarQueryRepository
	authorizer SourceVisibilityAuthorizer
	recurrence CalendarRecurrenceExpander
}

func NewQueryService(repository CalendarQueryRepository, authorizer SourceVisibilityAuthorizer, recurrence CalendarRecurrenceExpander) *QueryService {
	return &QueryService{repository: repository, authorizer: authorizer, recurrence: recurrence}
}

type queryItem struct {
	view       EventView
	projection Projection
	occurrence Occurrence
	sortAt     time.Time
}
type agendaCursor struct {
	At time.Time `json:"at"`
	ID string    `json:"id"`
}

func (s *QueryService) List(ctx context.Context, request QueryRequest) (QueryPage, error) {
	items, cursor, limit, err := s.listItems(ctx, request)
	if err != nil {
		return QueryPage{}, err
	}
	page := QueryPage{Events: []EventView{}}
	for _, item := range items {
		if afterAgenda(item, cursor) {
			page.Events = append(page.Events, item.view)
			if len(page.Events) == limit {
				break
			}
		}
	}
	remaining := 0
	for _, item := range items {
		if afterAgenda(item, cursor) {
			remaining++
		}
	}
	if remaining > limit {
		last := page.Events[len(page.Events)-1]
		for _, item := range items {
			if item.view.OccurrenceID == last.OccurrenceID {
				page.NextCursor = encodeAgendaCursor(agendaCursor{At: item.sortAt, ID: item.view.OccurrenceID})
				break
			}
		}
	}
	return page, nil
}

func (s *QueryService) listItems(ctx context.Context, request QueryRequest) ([]queryItem, agendaCursor, int, error) {
	if s == nil || s.repository == nil || s.authorizer == nil || strings.TrimSpace(request.Principal.Scope.MSPID) == "" || request.Window.Start.IsZero() || !request.Window.End.After(request.Window.Start) {
		return nil, agendaCursor{}, 0, ErrInvalidCalendarQuery
	}
	principal := request.Principal
	principal.Scope.ClientID = ""
	if err := authorization.Authorize(principal, "calendar.read", scope.Target{MSPID: principal.Scope.MSPID}); err != nil {
		return nil, agendaCursor{}, 0, err
	}
	if request.Window.End.Sub(request.Window.Start) > maximumCalendarQueryWindow {
		return nil, agendaCursor{}, 0, ErrWindowTooLarge
	}
	limit := request.Limit
	if limit == 0 {
		limit = defaultCalendarPage
	}
	if limit < 1 || limit > maximumCalendarPage {
		return nil, agendaCursor{}, 0, ErrResultTooLarge
	}
	cursor, err := decodeAgendaCursor(request.Cursor)
	if err != nil {
		return nil, agendaCursor{}, 0, err
	}
	if request.Filter.ClientIDs != nil && len(request.Filter.ClientIDs) == 0 {
		return []queryItem{}, cursor, limit, nil
	}
	authorized, err := s.repository.AuthorizedCalendarClientIDs(ctx, principal)
	if err != nil {
		return nil, agendaCursor{}, 0, err
	}
	clients := intersectClientIDs(authorized, nil)
	// Full and Busy rows are disjoint, repository-authorized streams. This keeps
	// inaccessible rows out of both filtered limits and result-volume signals.
	repositoryFilter := request.Filter
	repositoryFilter.Principal = authorization.Principal{}
	repositoryFilter.Target = scope.Target{}
	rows, err := s.repository.ListCalendarProjections(ctx, principal, clients, request.Window, repositoryFilter, QueryVisibilityFull, maximumCalendarResults+1)
	if err != nil {
		return nil, agendaCursor{}, 0, err
	}
	if len(rows) > maximumCalendarResults {
		return nil, agendaCursor{}, 0, ErrResultTooLarge
	}
	type candidate struct {
		row      QueryProjection
		busyOnly bool
	}
	candidates := make([]candidate, 0, len(rows))
	seen := make(map[string]bool, len(rows))
	for _, row := range rows {
		candidates = append(candidates, candidate{row: row})
		seen[row.Projection.ID] = true
	}
	fallback, fallbackErr := s.repository.ListCalendarProjections(ctx, principal, clients, request.Window, Filter{TechnicianIDs: request.Filter.TechnicianIDs}, QueryVisibilityBusy, maximumCalendarResults+1)
	if fallbackErr != nil {
		return nil, agendaCursor{}, 0, fallbackErr
	}
	if len(fallback) > maximumCalendarResults {
		return nil, agendaCursor{}, 0, ErrResultTooLarge
	}
	for _, row := range fallback {
		if !seen[row.Projection.ID] {
			candidates = append(candidates, candidate{row: row, busyOnly: true})
		}
	}
	items := make([]queryItem, 0, len(rows))
	for _, candidate := range candidates {
		row := candidate.row
		occurrences, expandErr := s.expand(ctx, row, request.Window)
		if errors.Is(expandErr, ErrExpansionLimit) {
			return nil, agendaCursor{}, 0, ErrResultTooLarge
		}
		if expandErr != nil {
			return nil, agendaCursor{}, 0, expandErr
		}
		for _, occurrence := range occurrences {
			item, ok, viewErr := s.eventItem(ctx, principal, row, occurrence)
			if viewErr != nil {
				return nil, agendaCursor{}, 0, viewErr
			}
			if !ok {
				continue
			}
			if candidate.busyOnly && item.view.Privacy != PrivacyBusy {
				continue
			}
			if item.view.Privacy == PrivacyFull {
				if !matchesCalendarFilter(row, request.Filter) {
					continue
				}
			} else if !matchesBusyFilter(item.view, request.Filter) {
				continue
			}
			items = append(items, item)
			if len(items) > maximumCalendarResults {
				return nil, agendaCursor{}, 0, ErrResultTooLarge
			}
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].sortAt.Equal(items[j].sortAt) {
			return items[i].view.OccurrenceID < items[j].view.OccurrenceID
		}
		return items[i].sortAt.Before(items[j].sortAt)
	})
	return items, cursor, limit, nil
}

func (s *QueryService) expand(ctx context.Context, row QueryProjection, window QueryWindow) ([]Occurrence, error) {
	if s.recurrence != nil {
		return s.recurrence.ExpandCalendarOccurrences(ctx, row.Projection, window, row.Exceptions)
	}
	return expandOccurrences(row.Projection, window, row.Exceptions)
}

func (s *QueryService) eventItem(ctx context.Context, principal authorization.Principal, row QueryProjection, o Occurrence) (queryItem, bool, error) {
	read, err := s.authorizer.CanReadCalendarSource(ctx, principal, row.Projection.Source)
	if err != nil {
		return queryItem{}, false, err
	}
	schedule := false
	if row.Projection.AssigneeID != "" {
		schedule, err = s.authorizer.CanScheduleCalendarTechnician(ctx, principal, row.Projection.AssigneeID)
		if err != nil {
			return queryItem{}, false, err
		}
	}
	if !read && !schedule {
		return queryItem{}, false, nil
	}
	view := EventView{ProjectionID: row.Projection.ID, OccurrenceKey: o.OriginalLocalKey, SourceRevision: row.Projection.SourceRevision, HealthReasons: row.Health.Reasons, HasConflict: row.HasConflict, Recurrence: row.Projection.Recurrence, PlannedMinutes: row.Projection.PlannedMinutes, CapacityBearing: row.Projection.CapacityBearing, ID: stableEventID(row.Projection.Source, row.Projection.EventRole), OccurrenceID: stableCalendarID(row.Projection.Source.MSPID, row.Projection.Source.Type, row.Projection.Source.ID, row.Projection.EventRole, o.OriginalLocalKey), Title: row.Projection.Title, Privacy: PrivacyFull, AllDay: o.AllDay, Timezone: o.Timezone, OwnerID: row.Projection.OwnerID, AssigneeID: row.Projection.AssigneeID, Source: row.Projection.Source, EventRole: row.Projection.EventRole, Tags: append([]string(nil), row.Projection.Dimensions.TagIDs...), TechnologyIDs: append([]string(nil), row.Projection.Dimensions.TechnologyIDs...), ProjectIDs: append([]string(nil), row.Projection.Dimensions.ProjectIDs...), TeamIDs: append([]string(nil), row.Projection.Dimensions.TeamIDs...), Health: row.Health.State, SchedulingMode: row.Projection.SchedulingMode, Capabilities: EventCapabilities{ViewSource: read, Schedule: schedule}}
	if len(row.Projection.Dimensions.Priorities) > 0 {
		view.Priority = row.Projection.Dimensions.Priorities[0]
	}
	if o.AllDay {
		view.StartsOn = o.StartsOn
		view.EndsOn = o.EndsOn
	} else {
		start, end := o.StartsAt, o.EndsAt
		view.StartsAt = &start
		if !end.IsZero() {
			view.EndsAt = &end
		}
	}
	if !read {
		view.ProjectionID = ""
		view.OccurrenceKey = ""
		view.SourceRevision = 0
		view.HealthReasons = nil
		view.HasConflict = false
		view.Recurrence = nil
		view.PlannedMinutes = 0
		view.CapacityBearing = false
		view.EventRole = ""
		view.Title = "Busy"
		view.Privacy = PrivacyBusy
		view.OwnerID = ""
		view.Source = SourceRef{}
		view.Tags = nil
		view.TechnologyIDs = nil
		view.ProjectIDs = nil
		view.TeamIDs = nil
		view.Priority = ""
		view.Health = ""
		view.SchedulingMode = ""
		view.Capabilities.ViewSource = false
	}
	sortAt := o.StartsAt
	if o.AllDay && o.StartsOn != nil {
		sortAt = *o.StartsOn
	}
	return queryItem{view: view, projection: row.Projection, occurrence: o, sortAt: sortAt}, true, nil
}

func (s *QueryService) FilterOptions(ctx context.Context, request QueryRequest) (FilterOptionCounts, error) {
	request.Cursor = ""
	request.Limit = maximumCalendarPage
	items, _, _, err := s.listItems(ctx, request)
	if err != nil {
		return FilterOptionCounts{}, err
	}
	r := FilterOptionCounts{Owners: map[string]int{}, Phases: map[string]int{}, SLAs: map[string]int{}, TicketTypes: map[string]int{}, Technicians: map[string]int{}, Clients: map[string]int{}, Tags: map[string]int{}, Technologies: map[string]int{}, Projects: map[string]int{}, Teams: map[string]int{}, Priorities: map[string]int{}, EventRoles: map[string]int{}}
	for _, item := range items {
		v := item.view
		if v.AssigneeID != "" {
			r.Technicians[v.AssigneeID]++
		}
		if v.Privacy != PrivacyFull {
			continue
		}
		if v.Source.ClientID != "" {
			r.Clients[v.Source.ClientID]++
		}
		if v.OwnerID != "" {
			r.Owners[v.OwnerID]++
		}
		addCounts(r.Phases, item.projection.Dimensions.PhaseIDs)
		if v.Source.Type == "phase" {
			r.Phases[v.Source.ID]++
		}
		addCounts(r.SLAs, item.projection.Dimensions.SLAIDs)
		addCounts(r.TicketTypes, item.projection.Dimensions.TicketTypes)
		addCounts(r.Tags, v.Tags)
		addCounts(r.Technologies, v.TechnologyIDs)
		addCounts(r.Projects, v.ProjectIDs)
		addCounts(r.Teams, v.TeamIDs)
		if v.Priority != "" {
			r.Priorities[v.Priority]++
		}
		if v.EventRole != "" {
			r.EventRoles[v.EventRole]++
		}
	}
	return r, nil
}
func (s *QueryService) Capacity(ctx context.Context, request QueryRequest) (map[string]CapacitySummary, error) {
	request.Cursor = ""
	request.Limit = maximumCalendarPage
	items, _, _, err := s.listItems(ctx, request)
	if err != nil {
		return nil, err
	}
	inputs := map[string]CapacityInput{}
	technicianSet := map[string]bool{}
	for _, item := range items {
		if item.projection.AssigneeID != "" && item.view.Capabilities.Schedule {
			technicianSet[item.projection.AssigneeID] = true
		}
	}
	technicianIDs := make([]string, 0, len(technicianSet))
	for id := range technicianSet {
		technicianIDs = append(technicianIDs, id)
	}
	sort.Strings(technicianIDs)
	if repository, ok := s.repository.(CalendarQueryAvailabilityRepository); ok && len(technicianIDs) > 0 {
		available, loadErr := repository.LoadCalendarQueryAvailability(ctx, request.Principal.Scope.MSPID, technicianIDs, request.Window)
		if loadErr != nil {
			return nil, loadErr
		}
		for id, resolved := range available {
			if !technicianSet[id] {
				continue
			}
			inputs[id] = CapacityInput{Window: request.Window, Available: resolved.Segments, TentativeUnavailableMinutes: resolved.TentativeMinutes}
		}
	}
	hiddenEventIDs := map[string]bool{}
	for _, item := range items {
		p := item.projection
		if p.AssigneeID == "" || !technicianSet[p.AssigneeID] || !p.CapacityBearing || p.SchedulingMode == Informational || p.PlannedMinutes <= 0 {
			continue
		}
		input := inputs[p.AssigneeID]
		input.Window = request.Window
		interval := TimeInterval{Start: request.Window.Start, End: request.Window.End}
		if !item.occurrence.AllDay {
			interval = TimeInterval{Start: item.occurrence.StartsAt, End: item.occurrence.EndsAt}
		}
		eventID := item.view.OccurrenceID
		input.Events = append(input.Events, CapacityEvent{ID: eventID, Assigned: true, Mode: p.SchedulingMode, PlannedMinutes: p.PlannedMinutes, Interval: interval})
		if item.view.Privacy == PrivacyBusy {
			hiddenEventIDs[eventID] = true
		}
		inputs[p.AssigneeID] = input
	}
	result := map[string]CapacitySummary{}
	for id, input := range inputs {
		summary := CalculateCapacity(input)
		visibleInput := input
		visibleInput.Events = make([]CapacityEvent, 0, len(input.Events))
		for _, event := range input.Events {
			if !hiddenEventIDs[event.ID] {
				visibleInput.Events = append(visibleInput.Events, event)
			}
		}
		visible := CalculateCapacity(visibleInput)
		// Committed/remaining/overbooked totals include Busy work, but the
		// mode-specific breakdown and event segments contain full events only.
		summary.FixedMinutes = visible.FixedMinutes
		summary.AllocatedMinutes = visible.AllocatedMinutes
		summary.Segments = visible.Segments
		result[id] = summary
	}
	return result, nil
}

// Busy events expose only technician and time. Filters over any other field
// are deliberately ignored so the result cannot be used as a hidden-source
// membership oracle.
func matchesBusyFilter(view EventView, f Filter) bool {
	return len(f.TechnicianIDs) == 0 || stringSlicesOverlap([]string{view.AssigneeID}, f.TechnicianIDs)
}

func intersectClientIDs(authorized, requested []string) []string {
	allowed := map[string]bool{}
	for _, v := range authorized {
		if v = strings.TrimSpace(v); v != "" {
			allowed[v] = true
		}
	}
	result := []string{}
	if requested == nil {
		for v := range allowed {
			result = append(result, v)
		}
	} else {
		seen := map[string]bool{}
		for _, v := range requested {
			if allowed[v] && !seen[v] {
				seen[v] = true
				result = append(result, v)
			}
		}
	}
	sort.Strings(result)
	return result
}
func stringSlicesOverlap(a, b []string) bool {
	if len(b) == 0 {
		return true
	}
	set := map[string]bool{}
	for _, v := range a {
		set[v] = true
	}
	for _, v := range b {
		if set[v] {
			return true
		}
	}
	return false
}
func matchesCalendarFilter(row QueryProjection, f Filter) bool {
	p := row.Projection
	d := p.Dimensions
	if f.ClientIDs != nil {
		if len(f.ClientIDs) == 0 || !stringSlicesOverlap([]string{p.Source.ClientID}, f.ClientIDs) {
			return false
		}
	}
	if len(f.TechnicianIDs) > 0 && !stringSlicesOverlap(append(append([]string{}, d.TechnicianIDs...), p.AssigneeID), f.TechnicianIDs) {
		return false
	}
	if len(f.OwnerIDs) > 0 && !stringSlicesOverlap([]string{p.OwnerID}, f.OwnerIDs) {
		return false
	}
	phaseIDs := append([]string(nil), d.PhaseIDs...)
	if p.Source.Type == "phase" {
		phaseIDs = append(phaseIDs, p.Source.ID)
	}
	if !stringSlicesOverlap(d.TeamIDs, f.TeamIDs) || !stringSlicesOverlap(d.TechnologyIDs, f.TechnologyIDs) || !stringSlicesOverlap(d.ProjectIDs, f.ProjectIDs) || !stringSlicesOverlap(phaseIDs, f.PhaseIDs) || !stringSlicesOverlap(d.SLAIDs, f.SLAIDs) || !stringSlicesOverlap(d.TicketTypes, f.TicketTypes) || !stringSlicesOverlap(d.TagIDs, f.TagIDs) || !stringSlicesOverlap(d.Priorities, f.Priorities) || !stringSlicesOverlap([]string{p.EventRole}, f.EventRoles) || !stringSlicesOverlap([]string{p.Source.Type}, f.SourceTypes) {
		return false
	}
	if len(f.SchedulingModes) > 0 {
		ok := false
		for _, mode := range f.SchedulingModes {
			ok = ok || mode == p.SchedulingMode
		}
		if !ok {
			return false
		}
	}
	if len(f.TerminalStates) > 0 {
		ok := false
		for _, state := range f.TerminalStates {
			ok = ok || state == p.TerminalState
		}
		if !ok {
			return false
		}
	}
	if len(f.HealthStates) > 0 {
		ok := false
		for _, v := range f.HealthStates {
			ok = ok || v == row.Health.State
		}
		if !ok {
			return false
		}
	}
	return !f.ConflictsOnly || row.HasConflict
}
func addCounts(m map[string]int, values []string) {
	for _, v := range values {
		if v != "" {
			m[v]++
		}
	}
}
func encodeAgendaCursor(c agendaCursor) string {
	raw, _ := json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(raw)
}
func decodeAgendaCursor(value string) (agendaCursor, error) {
	if value == "" {
		return agendaCursor{}, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return agendaCursor{}, ErrInvalidAgendaCursor
	}
	var c agendaCursor
	if json.Unmarshal(raw, &c) != nil || c.At.IsZero() || c.ID == "" {
		return agendaCursor{}, ErrInvalidAgendaCursor
	}
	return c, nil
}
func afterAgenda(item queryItem, c agendaCursor) bool {
	return c.ID == "" || item.sortAt.After(c.At) || item.sortAt.Equal(c.At) && item.view.OccurrenceID > c.ID
}
