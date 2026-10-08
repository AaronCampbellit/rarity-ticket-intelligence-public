// Package mentions defines internal structured mentions and recipient widget
// state. Plain text is never parsed into mentions; callers must provide tokens.
package mentions

import (
	"errors"
	"time"
)

var (
	ErrInvalidTokens    = errors.New("invalid mention tokens")
	ErrInvalidItemState = errors.New("invalid mention item state")
)

type TargetType string

const (
	TargetStaff TargetType = "staff"
	TargetTeam  TargetType = "team"
)

type ParentType string

const (
	ParentWorkRecord ParentType = "work_record"
	ParentTask       ParentType = "task"
	ParentProject    ParentType = "project"
)

type SourceKind string

const (
	SourceDetails SourceKind = "details"
	SourceComment SourceKind = "comment"
	SourceNote    SourceKind = "note"
)

// Token is a stable, structured mention embedded in versioned source content.
// Start and End are zero-based UTF-16 code-unit offsets, matching browser DOM
// selection and contenteditable conventions. End is exclusive.
type Token struct {
	ID         string     `json:"id"`
	TargetType TargetType `json:"target_type"`
	TargetID   string     `json:"target_id"`
	Label      string     `json:"label"`
	Start      int        `json:"start"`
	End        int        `json:"end"`
}

type TokenDiff struct {
	Added []Token `json:"added"`
}

type ItemState string

const (
	Unread   ItemState = "unread"
	Read     ItemState = "read"
	Archived ItemState = "archived"
)

type Item struct {
	ID                 string     `json:"id"`
	MSPID              string     `json:"msp_id"`
	ClientID           string     `json:"client_id"`
	RecipientID        string     `json:"recipient_id"`
	ParentType         ParentType `json:"parent_type"`
	ParentID           string     `json:"parent_id"`
	LatestOccurrenceID string     `json:"latest_occurrence_id"`
	State              ItemState  `json:"state"`
	ReadAt             *time.Time `json:"read_at,omitempty"`
	ArchivedAt         *time.Time `json:"archived_at,omitempty"`
	LastMentionedAt    time.Time  `json:"last_mentioned_at"`
	SuppressedAt       *time.Time `json:"suppressed_at,omitempty"`
	SuppressionReason  string     `json:"suppression_reason,omitempty"`
	Version            int64      `json:"version"`
	UpdatedAt          time.Time  `json:"updated_at"`
	UpdatedBy          string     `json:"updated_by"`
}
