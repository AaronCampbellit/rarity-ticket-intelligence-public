package calendar

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"sync"
)

var (
	ErrDuplicateRole = errors.New("duplicate calendar event role")
	ErrInvalidRole   = errors.New("invalid calendar event role")
)

var rolePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`)

type EventRoleDefinition struct {
	SourceType         string
	Role               string
	SourceRoleKey      string
	SchedulingMode     SchedulingMode
	CapacityBearing    bool
	DependencyEligible bool
	ReadOnly           bool
}

type RoleRegistry struct {
	mu          sync.RWMutex
	definitions map[string]EventRoleDefinition
}

func NewRoleRegistry() *RoleRegistry {
	return &RoleRegistry{definitions: make(map[string]EventRoleDefinition)}
}

func (r *RoleRegistry) Register(definition EventRoleDefinition) error {
	if r == nil {
		return fmt.Errorf("%w: nil registry", ErrInvalidRole)
	}
	if _, ok := sourceTypes[definition.SourceType]; !ok || !validRoleDefinition(definition.SourceType, definition.Role) ||
		(definition.SourceType == "custom_date" && !rolePattern.MatchString(definition.SourceRoleKey)) ||
		(definition.SourceType != "custom_date" && definition.SourceRoleKey != "") ||
		!validSchedulingMode(definition.SchedulingMode) || definition.CapacityBearing && definition.SchedulingMode == Informational ||
		(definition.DependencyEligible && definition.SchedulingMode == Informational && !(definition.SourceType == "milestone" && definition.Role == "milestone")) {
		return fmt.Errorf("%w: %+v", ErrInvalidRole, definition)
	}
	key := roleRegistryKey(definition.SourceType, definition.Role, definition.SourceRoleKey)
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.definitions == nil {
		r.definitions = make(map[string]EventRoleDefinition)
	}
	if _, exists := r.definitions[key]; exists {
		return fmt.Errorf("%w: %s/%s", ErrDuplicateRole, definition.SourceType, definition.Role)
	}
	r.definitions[key] = definition
	return nil
}

func (r *RoleRegistry) DefinitionForProjection(projection Projection) (EventRoleDefinition, bool) {
	if r == nil {
		return EventRoleDefinition{}, false
	}
	key := roleRegistryKey(projection.Source.Type, projection.EventRole, projection.SourceRoleKey)
	r.mu.RLock()
	definition, ok := r.definitions[key]
	r.mu.RUnlock()
	return definition, ok
}

// ValidateProjection checks both the projection's intrinsic source contract
// and that its source/role pair has been configured in this registry.
func (r *RoleRegistry) ValidateProjection(projection Projection) error {
	if err := projection.Validate(); err != nil {
		return err
	}
	if r == nil {
		return fmt.Errorf("%w: nil registry", ErrInvalidRole)
	}
	key := roleRegistryKey(projection.Source.Type, projection.EventRole, projection.SourceRoleKey)
	r.mu.RLock()
	_, exists := r.definitions[key]
	r.mu.RUnlock()
	if !exists {
		return fmt.Errorf("%w: unregistered projection role %s/%s", ErrInvalidRole, projection.Source.Type, projection.EventRole)
	}
	return nil
}

func roleRegistryKey(sourceType, role, sourceRoleKey string) string {
	key := sourceType + "\x00" + role
	if sourceType == "custom_date" {
		key += "\x00" + sourceRoleKey
	}
	return key
}

func (r *RoleRegistry) Definitions() []EventRoleDefinition {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	definitions := make([]EventRoleDefinition, 0, len(r.definitions))
	for _, definition := range r.definitions {
		definitions = append(definitions, definition)
	}
	sort.Slice(definitions, func(i, j int) bool {
		if definitions[i].SourceType == definitions[j].SourceType {
			if definitions[i].Role == definitions[j].Role {
				return definitions[i].SourceRoleKey < definitions[j].SourceRoleKey
			}
			return definitions[i].Role < definitions[j].Role
		}
		return definitions[i].SourceType < definitions[j].SourceType
	})
	return definitions
}
