// Package tagging owns the governed classification catalog and associations.
package tagging

import "time"

type ObjectType string

const (
	ObjectWorkRecord       ObjectType = "work_record"
	ObjectTask             ObjectType = "task"
	ObjectProject          ObjectType = "project"
	ObjectAsset            ObjectType = "asset"
	ObjectKnowledgeArticle ObjectType = "knowledge_article"
	ObjectTimeEntry        ObjectType = "time_entry"
)

type Source string

const (
	SourceHuman          Source = "human"
	SourceAIConfirmed    Source = "ai_confirmed"
	SourceAIAutomatic    Source = "ai_automatic"
	SourceAutomation     Source = "automation"
	SourceIntegration    Source = "integration"
	SourceMigration      Source = "migration"
	SourceSystemFallback Source = "system_fallback"
)

type State string

const (
	StateActive   State = "active"
	StateMerged   State = "merged"
	StateArchived State = "archived"
)

type Group struct {
	ID            string `json:"id"`
	MSPID         string `json:"msp_id"`
	InternalKey   string `json:"internal_key"`
	Label         string `json:"label"`
	Description   string `json:"description"`
	Position      int    `json:"position"`
	State         State  `json:"state"`
	SystemManaged bool   `json:"system_managed"`
	Version       int64  `json:"version"`
}

type Tag struct {
	ID              string   `json:"id"`
	MSPID           string   `json:"msp_id"`
	InternalKey     string   `json:"internal_key"`
	Label           string   `json:"label"`
	GroupID         string   `json:"group_id"`
	Description     string   `json:"description"`
	Color           string   `json:"color,omitempty"`
	State           State    `json:"state"`
	SystemManaged   bool     `json:"system_managed"`
	MergedIntoTagID string   `json:"merged_into_tag_id,omitempty"`
	Synonyms        []string `json:"synonyms"`
	Version         int64    `json:"version"`
}

type Catalog struct {
	Groups []Group `json:"groups"`
	Tags   []Tag   `json:"tags"`
}

type ImpactOperation string

const (
	ImpactRename  ImpactOperation = "rename"
	ImpactMove    ImpactOperation = "move"
	ImpactMerge   ImpactOperation = "merge"
	ImpactArchive ImpactOperation = "archive"
)

type Impact struct {
	Operation            ImpactOperation      `json:"operation"`
	TagID                string               `json:"tag_id"`
	ReplacementTagID     string               `json:"replacement_tag_id,omitempty"`
	AffectedObjects      int64                `json:"affected_objects"`
	AffectedSavedViews   int64                `json:"affected_saved_views"`
	AffectedReports      int64                `json:"affected_reports"`
	AffectedAutomations  int64                `json:"affected_automations"`
	FallbackByObjectType map[ObjectType]int64 `json:"fallback_by_object_type"`
}

type ObjectHealth struct {
	Meaningful      int64 `json:"meaningful"`
	Unclassified    int64 `json:"unclassified"`
	ArchiveFallback int64 `json:"archive_fallback"`
}

type Health struct {
	ByObjectType map[ObjectType]ObjectHealth `json:"by_object_type"`
}

type MigrationRun struct {
	ID                    string     `json:"id"`
	MSPID                 string     `json:"msp_id"`
	StartedAt             time.Time  `json:"started_at"`
	CompletedAt           *time.Time `json:"completed_at,omitempty"`
	Status                string     `json:"status"`
	RowsDiscovered        int64      `json:"rows_discovered"`
	RowsMigrated          int64      `json:"rows_migrated"`
	FallbackAssignments   int64      `json:"fallback_assignments"`
	CategorySourcePresent bool       `json:"category_source_present"`
}
