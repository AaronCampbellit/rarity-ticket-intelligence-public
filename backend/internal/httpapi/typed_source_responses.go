package httpapi

import (
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/calendar"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/commitments"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/customfields"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/projects"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/workforce"
)

type customDateValueResponseDTO struct {
	ID             string                  `json:"id"`
	FieldID        string                  `json:"field_id"`
	ClientID       string                  `json:"client_id,omitempty"`
	ObjectID       string                  `json:"object_id"`
	ObjectType     customfields.ObjectType `json:"object_type"`
	Timezone       string                  `json:"timezone,omitempty"`
	SourceRevision int64                   `json:"source_revision"`
	Version        int64                   `json:"version"`
	DateValue      string                  `json:"date_value,omitempty"`
	TimestampValue string                  `json:"timestamp_value,omitempty"`
	CreatedAt      string                  `json:"created_at,omitempty"`
	UpdatedAt      string                  `json:"updated_at,omitempty"`
}

func customDateValueResponse(value customfields.DateValue) customDateValueResponseDTO {
	return customDateValueResponseDTO{ID: value.ID, FieldID: value.FieldID, ClientID: value.ClientID, ObjectID: value.ObjectID, ObjectType: value.ObjectType, Timezone: value.Timezone, SourceRevision: value.SourceRevision, Version: value.Version, DateValue: isoDatePointer(value.DateValue), TimestampValue: rfc3339Pointer(value.TimestampValue), CreatedAt: rfc3339(value.CreatedAt), UpdatedAt: rfc3339(value.UpdatedAt)}
}

func customDateValueResponses(values []customfields.DateValue) []customDateValueResponseDTO {
	result := make([]customDateValueResponseDTO, len(values))
	for i, value := range values {
		result[i] = customDateValueResponse(value)
	}
	return result
}

type scheduleWindowResponse struct {
	ID              string `json:"id"`
	Weekday         int    `json:"weekday"`
	StartsMinute    int    `json:"starts_minute"`
	EndsMinute      int    `json:"ends_minute"`
	CapacityPercent int    `json:"capacity_percent"`
}

type scheduleExceptionResponse struct {
	ID              string                   `json:"id"`
	ExceptionOn     string                   `json:"exception_on"`
	State           workforce.ExceptionState `json:"state"`
	AllDay          bool                     `json:"all_day"`
	StartsMinute    int                      `json:"starts_minute"`
	EndsMinute      int                      `json:"ends_minute"`
	CapacityPercent int                      `json:"capacity_percent"`
	Reason          string                   `json:"reason"`
	Version         int64                    `json:"version"`
}

type workforceScheduleResponse struct {
	ID               string                      `json:"id"`
	TechnicianID     string                      `json:"technician_id"`
	Timezone         string                      `json:"timezone"`
	EffectiveFrom    string                      `json:"effective_from"`
	EffectiveThrough string                      `json:"effective_through,omitempty"`
	Version          int64                       `json:"version"`
	Windows          []scheduleWindowResponse    `json:"windows"`
	Exceptions       []scheduleExceptionResponse `json:"exceptions"`
	CreatedAt        string                      `json:"created_at,omitempty"`
}

func scheduleResponse(value workforce.Schedule) workforceScheduleResponse {
	windows := make([]scheduleWindowResponse, len(value.Windows))
	for i, window := range value.Windows {
		windows[i] = scheduleWindowResponse{ID: window.ID, Weekday: int(window.Weekday), StartsMinute: window.StartsMinute, EndsMinute: window.EndsMinute, CapacityPercent: window.CapacityPercent}
	}
	exceptions := make([]scheduleExceptionResponse, len(value.Exceptions))
	for i, exception := range value.Exceptions {
		exceptions[i] = scheduleExceptionResponse{ID: exception.ID, ExceptionOn: isoDate(exception.ExceptionOn), State: exception.State, AllDay: exception.AllDay, StartsMinute: exception.StartsMinute, EndsMinute: exception.EndsMinute, CapacityPercent: exception.CapacityPercent, Reason: exception.Reason, Version: exception.Version}
	}
	return workforceScheduleResponse{ID: value.ID, TechnicianID: value.TechnicianID, Timezone: value.Timezone, EffectiveFrom: isoDate(value.EffectiveFrom), EffectiveThrough: isoDatePointer(value.EffectiveThrough), Version: value.Version, Windows: windows, Exceptions: exceptions, CreatedAt: rfc3339(value.CreatedAt)}
}

func scheduleResponses(values []workforce.Schedule) []workforceScheduleResponse {
	result := make([]workforceScheduleResponse, len(values))
	for i, value := range values {
		result[i] = scheduleResponse(value)
	}
	return result
}

type workforcePTOResponse struct {
	ID           string             `json:"id"`
	TechnicianID string             `json:"technician_id"`
	PTOType      string             `json:"pto_type"`
	Timezone     string             `json:"timezone,omitempty"`
	StartsOn     string             `json:"starts_on,omitempty"`
	EndsOn       string             `json:"ends_on,omitempty"`
	StartsAt     string             `json:"starts_at,omitempty"`
	EndsAt       string             `json:"ends_at,omitempty"`
	AllDay       bool               `json:"all_day"`
	State        workforce.PTOState `json:"state"`
	Version      int64              `json:"version"`
	CreatedAt    string             `json:"created_at,omitempty"`
	UpdatedAt    string             `json:"updated_at,omitempty"`
}

func ptoResponse(value workforce.PTORequest) workforcePTOResponse {
	return workforcePTOResponse{ID: value.ID, TechnicianID: value.TechnicianID, PTOType: value.PTOType, Timezone: value.Timezone, StartsOn: isoDatePointer(value.StartsOn), EndsOn: isoDatePointer(value.EndsOn), StartsAt: rfc3339Pointer(value.StartsAt), EndsAt: rfc3339Pointer(value.EndsAt), AllDay: value.AllDay, State: value.State, Version: value.Version, CreatedAt: rfc3339(value.CreatedAt), UpdatedAt: rfc3339(value.UpdatedAt)}
}

func ptoResponses(values []workforce.PTORequest) []workforcePTOResponse {
	result := make([]workforcePTOResponse, len(values))
	for i, value := range values {
		result[i] = ptoResponse(value)
	}
	return result
}

type milestoneResponseDTO struct {
	ID          string                   `json:"id"`
	ClientID    string                   `json:"client_id"`
	ProjectID   string                   `json:"project_id"`
	PhaseID     string                   `json:"phase_id,omitempty"`
	Name        string                   `json:"name"`
	Description string                   `json:"description"`
	Priority    string                   `json:"priority"`
	Status      string                   `json:"status"`
	OwnerID     string                   `json:"owner_id,omitempty"`
	DueOn       string                   `json:"due_on"`
	AllDay      bool                     `json:"all_day"`
	StartsOn    string                   `json:"starts_on,omitempty"`
	EndsOn      string                   `json:"ends_on,omitempty"`
	StartsAt    string                   `json:"starts_at,omitempty"`
	EndsAt      string                   `json:"ends_at,omitempty"`
	Timezone    string                   `json:"timezone,omitempty"`
	Recurrence  *calendar.RecurrenceRule `json:"recurrence,omitempty"`
	Version     int64                    `json:"version"`
	CreatedAt   string                   `json:"created_at,omitempty"`
	UpdatedAt   string                   `json:"updated_at,omitempty"`
}

func milestoneResponse(value projects.Milestone) milestoneResponseDTO {
	return milestoneResponseDTO{ID: value.ID, ClientID: value.ClientID, ProjectID: string(value.ProjectID), PhaseID: string(value.PhaseID), Name: value.Name, Description: value.Description, Priority: value.Priority, Status: value.Status, OwnerID: value.OwnerID, DueOn: isoDate(value.DueOn), AllDay: value.AllDay, StartsOn: isoDatePointer(value.StartsOn), EndsOn: isoDatePointer(value.EndsOn), StartsAt: rfc3339Pointer(value.StartsAt), EndsAt: rfc3339Pointer(value.EndsAt), Timezone: value.Timezone, Recurrence: value.Recurrence, Version: value.Version, CreatedAt: rfc3339(value.CreatedAt), UpdatedAt: rfc3339(value.UpdatedAt)}
}

func milestoneResponses(values []projects.Milestone) []milestoneResponseDTO {
	result := make([]milestoneResponseDTO, len(values))
	for i, value := range values {
		result[i] = milestoneResponse(value)
	}
	return result
}

type maintenanceScopeResponse struct {
	ID         string                `json:"id"`
	ClientID   string                `json:"client_id"`
	Type       commitments.ScopeType `json:"type"`
	ResourceID string                `json:"resource_id"`
}

type maintenanceResponseDTO struct {
	ID             string                     `json:"id"`
	Title          string                     `json:"title"`
	Description    string                     `json:"description"`
	Timezone       string                     `json:"timezone"`
	Status         string                     `json:"status"`
	OwnerID        string                     `json:"owner_id,omitempty"`
	StartsAt       string                     `json:"starts_at,omitempty"`
	EndsAt         string                     `json:"ends_at,omitempty"`
	AllDay         bool                       `json:"all_day"`
	StartsOn       string                     `json:"starts_on,omitempty"`
	EndsOn         string                     `json:"ends_on,omitempty"`
	Recurrence     *calendar.RecurrenceRule   `json:"recurrence,omitempty"`
	Protected      bool                       `json:"protected"`
	ConflictPolicy commitments.ConflictPolicy `json:"conflict_policy"`
	Version        int64                      `json:"version"`
	Scopes         []maintenanceScopeResponse `json:"scopes"`
	CreatedAt      string                     `json:"created_at,omitempty"`
	UpdatedAt      string                     `json:"updated_at,omitempty"`
}

func maintenanceResponse(value commitments.MaintenanceWindow) maintenanceResponseDTO {
	scopes := make([]maintenanceScopeResponse, len(value.Scopes))
	for i, item := range value.Scopes {
		scopes[i] = maintenanceScopeResponse{ID: item.ID, ClientID: item.ClientID, Type: item.Type, ResourceID: item.ResourceID}
	}
	return maintenanceResponseDTO{ID: value.ID, Title: value.Title, Description: value.Description, Timezone: value.Timezone, Status: value.Status, OwnerID: value.OwnerID, StartsAt: rfc3339(value.StartsAt), EndsAt: rfc3339(value.EndsAt), AllDay: value.AllDay, StartsOn: isoDatePointer(value.StartsOn), EndsOn: isoDatePointer(value.EndsOn), Recurrence: value.Recurrence, Protected: value.Protected, ConflictPolicy: value.ConflictPolicy, Version: value.Version, Scopes: scopes, CreatedAt: rfc3339(value.CreatedAt), UpdatedAt: rfc3339(value.UpdatedAt)}
}

func maintenanceResponses(values []commitments.MaintenanceWindow) []maintenanceResponseDTO {
	result := make([]maintenanceResponseDTO, len(values))
	for i, value := range values {
		result[i] = maintenanceResponse(value)
	}
	return result
}

type commercialResponseDTO struct {
	ID                string                     `json:"id"`
	ClientID          string                     `json:"client_id"`
	Type              commitments.CommercialType `json:"type"`
	Title             string                     `json:"title"`
	Description       string                     `json:"description"`
	Vendor            string                     `json:"vendor"`
	OwnerID           string                     `json:"owner_id,omitempty"`
	ExternalReference string                     `json:"external_reference,omitempty"`
	Currency          string                     `json:"currency,omitempty"`
	Status            string                     `json:"status"`
	EffectiveOn       string                     `json:"effective_on"`
	NoticeOn          string                     `json:"notice_on,omitempty"`
	RenewalOn         string                     `json:"renewal_on,omitempty"`
	ExpirationOn      string                     `json:"expiration_on"`
	QuantityUnits     int64                      `json:"quantity_units"`
	CostMinor         int64                      `json:"cost_minor"`
	HasCost           bool                       `json:"has_cost"`
	Recurrence        *calendar.RecurrenceRule   `json:"recurrence,omitempty"`
	ServiceID         string                     `json:"service_id,omitempty"`
	AssetID           string                     `json:"asset_id,omitempty"`
	ContractID        string                     `json:"contract_id,omitempty"`
	Version           int64                      `json:"version"`
	CreatedAt         string                     `json:"created_at,omitempty"`
	UpdatedAt         string                     `json:"updated_at,omitempty"`
}

func commercialResponse(value commitments.CommercialCommitment) commercialResponseDTO {
	return commercialResponseDTO{ID: value.ID, ClientID: value.ClientID, Type: value.Type, Title: value.Title, Description: value.Description, Vendor: value.Vendor, OwnerID: value.OwnerID, ExternalReference: value.ExternalReference, Currency: value.Currency, Status: value.Status, EffectiveOn: isoDate(value.EffectiveOn), NoticeOn: isoDate(value.NoticeOn), RenewalOn: isoDate(value.RenewalOn), ExpirationOn: isoDate(value.ExpirationOn), QuantityUnits: value.QuantityUnits, CostMinor: value.CostMinor, HasCost: value.HasCost, Recurrence: value.Recurrence, ServiceID: value.ServiceID, AssetID: value.AssetID, ContractID: value.ContractID, Version: value.Version, CreatedAt: rfc3339(value.CreatedAt), UpdatedAt: rfc3339(value.UpdatedAt)}
}

func commercialResponses(values []commitments.CommercialCommitment) []commercialResponseDTO {
	result := make([]commercialResponseDTO, len(values))
	for i, value := range values {
		result[i] = commercialResponse(value)
	}
	return result
}

func isoDate(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.Format("2006-01-02")
}

func isoDatePointer(value *time.Time) string {
	if value == nil {
		return ""
	}
	return isoDate(*value)
}

func rfc3339(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.Format(time.RFC3339Nano)
}

func rfc3339Pointer(value *time.Time) string {
	if value == nil {
		return ""
	}
	return rfc3339(*value)
}
