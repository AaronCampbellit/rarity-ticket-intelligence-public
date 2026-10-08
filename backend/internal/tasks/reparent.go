package tasks

import (
	"context"
	"errors"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mutation"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

type ID = string

type ParentType string

const (
	ParentWorkRecord  ParentType = "work_record"
	ParentOpportunity ParentType = "opportunity"
	ParentProject     ParentType = "project"
	ParentPhase       ParentType = "phase"
)

type Ref struct {
	Type     ParentType `json:"type"`
	ID       string     `json:"id"`
	MSPID    string     `json:"msp_id"`
	ClientID string     `json:"client_id"`
}

type MoveCommand struct {
	From             Ref
	To               Ref
	Selected         []ID
	ExpectedVersions map[ID]int64
	Principal        authorization.Principal
	ActorID          string
	Source           string
}

type MovementHistory struct {
	TaskID          ID
	From            Ref
	To              Ref
	PreviousVersion int64
	AcceptedVersion int64
	MovedAt         time.Time
	MovedBy         string
}

type MoveMutation struct {
	Tasks               []Task
	Histories           []MovementHistory
	ReparentDescendants bool
	Audit               mutation.AuditRecord
	Event               mutation.EventRecord
}

var (
	ErrStaleTask   = errors.New("stale task")
	ErrInvalidMove = errors.New("invalid task movement")
)

func (s *Service) MoveIncomplete(ctx context.Context, command MoveCommand) ([]Task, error) {
	if !validMove(command) {
		return nil, ErrInvalidMove
	}
	if command.From.MSPID != command.To.MSPID ||
		command.From.ClientID != command.To.ClientID {
		return nil, scope.ErrNotFound
	}
	target := scope.Target{MSPID: command.From.MSPID, ClientID: command.From.ClientID}
	if err := authorization.Authorize(command.Principal, "task.move", target); err != nil {
		return nil, err
	}
	loaded, err := s.repository.LoadSelected(ctx, command.From, command.Selected)
	if err != nil {
		return nil, err
	}
	now := s.now().UTC()
	moved := make([]Task, 0, len(loaded))
	histories := make([]MovementHistory, 0, len(loaded))
	for _, task := range loaded {
		if task.MSPID != target.MSPID || task.ClientID != target.ClientID || task.Parent != command.From {
			return nil, scope.ErrNotFound
		}
		expected, exists := command.ExpectedVersions[task.ID]
		if !exists || expected != task.Version {
			return nil, ErrStaleTask
		}
		if task.Status == "completed" {
			continue
		}
		previousVersion := task.Version
		task.Parent = command.To
		task.WorkRecordID = ""
		task.Version++
		moved = append(moved, task)
		histories = append(histories, MovementHistory{
			TaskID: task.ID, From: command.From, To: command.To,
			PreviousVersion: previousVersion, AcceptedVersion: task.Version,
			MovedAt: now, MovedBy: command.ActorID,
		})
	}
	if len(moved) == 0 {
		return []Task{}, nil
	}
	auditID, eventID, correlationID := s.newID(), s.newID(), s.newID()
	accepted := MoveMutation{
		Tasks: moved, Histories: histories, ReparentDescendants: true,
		Audit: mutation.AuditRecord{
			ID: auditID, OccurredAt: now, MSPID: target.MSPID, ClientID: target.ClientID,
			ActorType: "technician", ActorID: command.ActorID,
			Action: "task.parent.changed", SubjectType: "task_move",
			SubjectID: moved[0].ID, SubjectVersion: moved[0].Version,
			Source: command.Source, CorrelationID: correlationID,
		},
		Event: mutation.EventRecord{
			EventID: eventID, EventType: "task.parent.changed", SchemaVersion: 1,
			OccurredAt: now, MSPID: target.MSPID, ClientID: target.ClientID,
			ActorType: "technician", ActorID: command.ActorID,
			SubjectType: "task_move", SubjectID: moved[0].ID,
			SubjectVersion: moved[0].Version, Source: command.Source,
			CorrelationID: correlationID,
		},
	}
	if err := s.repository.MoveAtomic(ctx, accepted); err != nil {
		return nil, err
	}
	return moved, nil
}

func validMove(command MoveCommand) bool {
	if command.From.Type != ParentOpportunity ||
		(command.To.Type != ParentProject && command.To.Type != ParentPhase) ||
		command.From.ID == "" || command.To.ID == "" ||
		command.From.MSPID == "" || command.From.ClientID == "" ||
		len(command.Selected) == 0 || command.ActorID == "" || command.Source == "" {
		return false
	}
	seen := map[ID]struct{}{}
	for _, id := range command.Selected {
		if id == "" {
			return false
		}
		if _, duplicate := seen[id]; duplicate {
			return false
		}
		seen[id] = struct{}{}
	}
	return true
}
