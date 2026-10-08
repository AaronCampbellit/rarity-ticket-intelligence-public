package tagging

import (
	"context"
	"errors"
	"sort"
	"strings"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

// ClassificationVisibility is assigned by the trusted object loader. Only
// standard fields can cross the provider boundary.
type ClassificationVisibility string

const (
	ClassificationStandard   ClassificationVisibility = "standard"
	ClassificationRestricted ClassificationVisibility = "restricted"
	ClassificationSecret     ClassificationVisibility = "secret"
	ClassificationAttachment ClassificationVisibility = "attachment"
)

type ClassificationField struct {
	Name       string
	Value      string
	Visibility ClassificationVisibility
}

type ClassificationSuggestion struct {
	TagID       string  `json:"tag_id"`
	Confidence  float64 `json:"confidence"`
	Rationale   string  `json:"rationale"`
	Active      bool    `json:"active"`
	Disposition string  `json:"disposition,omitempty"`
}

var ErrInvalidClassificationContext = errors.New("invalid classification context")
var ErrInvalidClassificationPolicy = errors.New("invalid classification policy")

// ClassificationPolicy is MSP-global; it is intentionally separate from the
// provider policy so classification can remain disabled even where AI writing
// assistance is enabled.
type ClassificationPolicy struct {
	MSPID                 string                      `json:"msp_id"`
	Enabled               bool                        `json:"automatic_apply_enabled"`
	Threshold             float64                     `json:"automatic_apply_threshold"`
	ModelProfileID        string                      `json:"model_profile_id,omitempty"`
	Version               int64                       `json:"version"`
	UpdatedBy             string                      `json:"-"`
	ModelOptions          []ClassificationModelOption `json:"model_options"`
	RetainedRate          *float64                    `json:"retained_rate,omitempty"`
	ChangeRate            *float64                    `json:"change_rate,omitempty"`
	ProviderFailureHealth string                      `json:"provider_failure_health"`
}

type ClassificationModelOption struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

type UpdateClassificationPolicyCommand struct {
	Principal       authorization.Principal
	Enabled         bool
	Threshold       float64
	ModelProfileID  string
	ExpectedVersion int64
}

// ClassificationRepository separates the policy transaction from provider
// invocation. Implementations must optimistic-lock ExpectedVersion.
type ClassificationRepository interface {
	GetClassificationPolicy(context.Context, string) (ClassificationPolicy, error)
	SaveClassificationPolicy(context.Context, ClassificationPolicy) (ClassificationPolicy, error)
}

type ClassificationService struct {
	repository   ClassificationRepository
	associations *AssociationService
	newID        func() string
}

type SuggestionState string

const (
	SuggestionPending   SuggestionState = "pending"
	SuggestionCompleted SuggestionState = "completed"
	SuggestionAccepted  SuggestionState = "accepted"
	SuggestionRejected  SuggestionState = "rejected"
)

type ClassificationSuggestionRecord struct {
	ID             string                     `json:"id"`
	JobID          string                     `json:"job_id,omitempty"`
	Target         TargetRef                  `json:"target"`
	ObjectVersion  int64                      `json:"object_version"`
	ModelProfileID string                     `json:"model_profile_id,omitempty"`
	Status         SuggestionState            `json:"status"`
	Items          []ClassificationSuggestion `json:"suggestions"`
	Version        int64                      `json:"version"`
	RequestedBy    string                     `json:"-"`
	Automatic      bool                       `json:"-"`
	Threshold      float64                    `json:"-"`
	LeaseToken     string                     `json:"-"`
}
type ClassificationSuggestionStore interface {
	CreateClassificationSuggestion(context.Context, ClassificationSuggestionRecord) (ClassificationSuggestionRecord, error)
	GetClassificationSuggestion(context.Context, TargetRef, string) (ClassificationSuggestionRecord, error)
	FindClassificationSuggestion(context.Context, string, string, string) (ClassificationSuggestionRecord, error)
	DecideClassificationSuggestion(context.Context, ClassificationSuggestionRecord, string, string, string) (ClassificationSuggestionRecord, error)
}
type ClassificationSuggestionService struct {
	store        ClassificationSuggestionStore
	associations *AssociationService
	newID        func() string
}

func NewClassificationSuggestionService(store ClassificationSuggestionStore, associations *AssociationService, newID func() string) *ClassificationSuggestionService {
	return &ClassificationSuggestionService{store: store, associations: associations, newID: newID}
}
func (s *ClassificationSuggestionService) Request(ctx context.Context, principal authorization.Principal, target TargetRef) (ClassificationSuggestionRecord, error) {
	if s == nil || s.store == nil || s.associations == nil || s.newID == nil || !validTarget(target) {
		return ClassificationSuggestionRecord{}, ErrInvalidAssociation
	}
	if err := authorizeAssociation(principal, target); err != nil {
		return ClassificationSuggestionRecord{}, err
	}
	object, err := s.associations.Get(ctx, GetCommand{Principal: principal, Target: target})
	if err != nil {
		return ClassificationSuggestionRecord{}, err
	}
	return s.store.CreateClassificationSuggestion(ctx, ClassificationSuggestionRecord{ID: s.newID(), JobID: s.newID(), Target: target, ObjectVersion: object.ObjectVersion, Status: SuggestionPending, RequestedBy: principal.ID})
}
func (s *ClassificationSuggestionService) Get(ctx context.Context, principal authorization.Principal, target TargetRef, id string) (ClassificationSuggestionRecord, error) {
	if s == nil || s.store == nil || strings.TrimSpace(id) == "" {
		return ClassificationSuggestionRecord{}, ErrInvalidAssociation
	}
	if err := authorizeAssociation(principal, target); err != nil {
		return ClassificationSuggestionRecord{}, err
	}
	return s.store.GetClassificationSuggestion(ctx, target, id)
}

// GetByID resolves the target only from trusted persistence before applying
// object access checks. The public deep-link route deliberately accepts no
// client or object identifiers from the browser.
func (s *ClassificationSuggestionService) GetByID(ctx context.Context, principal authorization.Principal, id string) (ClassificationSuggestionRecord, error) {
	if s == nil || s.store == nil || s.associations == nil || strings.TrimSpace(id) == "" {
		return ClassificationSuggestionRecord{}, ErrInvalidAssociation
	}
	record, err := s.store.FindClassificationSuggestion(ctx, principal.Scope.MSPID, principal.Scope.ClientID, id)
	if err != nil {
		return ClassificationSuggestionRecord{}, err
	}
	if _, err = s.associations.Get(ctx, GetCommand{Principal: principal, Target: record.Target}); err != nil {
		return ClassificationSuggestionRecord{}, err
	}
	return record, nil
}
func (s *ClassificationSuggestionService) Decide(ctx context.Context, principal authorization.Principal, target TargetRef, id, tagID, decision string) (ClassificationSuggestionRecord, error) {
	if s == nil || s.store == nil || strings.TrimSpace(id) == "" || strings.TrimSpace(tagID) == "" || (decision != "accepted" && decision != "dismissed") {
		return ClassificationSuggestionRecord{}, ErrInvalidAssociation
	}
	if err := authorizeAssociation(principal, target); err != nil {
		return ClassificationSuggestionRecord{}, err
	}
	record, err := s.store.GetClassificationSuggestion(ctx, target, id)
	if err != nil {
		return ClassificationSuggestionRecord{}, err
	}
	return s.store.DecideClassificationSuggestion(ctx, record, tagID, decision, principal.ID)
}

func (s *ClassificationSuggestionService) DecideByID(ctx context.Context, principal authorization.Principal, id, tagID, decision string) (ClassificationSuggestionRecord, error) {
	record, err := s.GetByID(ctx, principal, id)
	if err != nil {
		return ClassificationSuggestionRecord{}, err
	}
	return s.Decide(ctx, principal, record.Target, id, tagID, decision)
}

func NewClassificationService(repository ClassificationRepository, associations *AssociationService, newID func() string) *ClassificationService {
	return &ClassificationService{repository: repository, associations: associations, newID: newID}
}

func (s *ClassificationService) Policy(ctx context.Context, principal authorization.Principal) (ClassificationPolicy, error) {
	if s == nil || s.repository == nil {
		return ClassificationPolicy{}, ErrInvalidClassificationPolicy
	}
	if err := authorization.Authorize(principal, "classification.ai.manage", scope.Target{MSPID: principal.Scope.MSPID}); err != nil {
		return ClassificationPolicy{}, err
	}
	return s.repository.GetClassificationPolicy(ctx, principal.Scope.MSPID)
}

func (s *ClassificationService) UpdatePolicy(ctx context.Context, command UpdateClassificationPolicyCommand) (ClassificationPolicy, error) {
	if s == nil || s.repository == nil || command.ExpectedVersion < 1 || command.Threshold < .5 || command.Threshold > 1 ||
		(command.Enabled && strings.TrimSpace(command.ModelProfileID) == "") {
		return ClassificationPolicy{}, ErrInvalidClassificationPolicy
	}
	if err := authorization.Authorize(command.Principal, "classification.ai.manage", scope.Target{MSPID: command.Principal.Scope.MSPID}); err != nil {
		return ClassificationPolicy{}, err
	}
	policy, err := s.repository.GetClassificationPolicy(ctx, command.Principal.Scope.MSPID)
	if err != nil {
		return ClassificationPolicy{}, err
	}
	if policy.Version != command.ExpectedVersion {
		return ClassificationPolicy{}, ErrInvalidClassificationPolicy
	}
	policy.Enabled = command.Enabled
	policy.Threshold = command.Threshold
	policy.ModelProfileID = strings.TrimSpace(command.ModelProfileID)
	policy.UpdatedBy = command.Principal.ID
	return s.repository.SaveClassificationPolicy(ctx, policy)
}

// BuildClassificationContext is a defensive boundary: callers may pass all
// known fields, but only explicit standard text is returned for provider use.
func BuildClassificationContext(fields []ClassificationField) ([]ClassificationField, error) {
	result := make([]ClassificationField, 0, len(fields))
	for _, field := range fields {
		if field.Visibility != ClassificationStandard {
			continue
		}
		field.Name = strings.TrimSpace(field.Name)
		field.Value = strings.TrimSpace(field.Value)
		if field.Name == "" || field.Value == "" {
			continue
		}
		if len(field.Value) > 32_000 || len(result) == 20 {
			return nil, ErrInvalidClassificationContext
		}
		result = append(result, field)
	}
	if len(result) == 0 {
		return nil, ErrInvalidClassificationContext
	}
	return result, nil
}

// EligibleAutomaticTagIDs returns only active candidates at or above the
// persisted threshold. Callers must still recheck policy and target versions
// immediately before applying the returned IDs.
func EligibleAutomaticTagIDs(threshold float64, suggestions []ClassificationSuggestion) []string {
	if threshold < .5 || threshold > 1 {
		return []string{}
	}
	seen := make(map[string]struct{}, len(suggestions))
	result := make([]string, 0, len(suggestions))
	for _, suggestion := range suggestions {
		if !suggestion.Active || suggestion.Confidence < threshold || strings.TrimSpace(suggestion.TagID) == "" {
			continue
		}
		if _, found := seen[suggestion.TagID]; found {
			continue
		}
		seen[suggestion.TagID] = struct{}{}
		result = append(result, suggestion.TagID)
	}
	sort.Strings(result)
	return result
}
