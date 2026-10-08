package datto

import (
	"context"
	"errors"
	"strings"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/tagging"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/workrecords"
)

type AlertWorkRecordFinder interface {
	Find(context.Context, scope.Target, string) (workrecords.Record, error)
}

type AlertWorkRecordCreator interface {
	CreateDattoIncident(
		context.Context,
		workrecords.CreateCommand,
	) (workrecords.Record, error)
}

type WorkRecordAlertIncidentWriter struct {
	finder  AlertWorkRecordFinder
	creator AlertWorkRecordCreator
}

func NewWorkRecordAlertIncidentWriter(
	finder AlertWorkRecordFinder,
	creator AlertWorkRecordCreator,
) *WorkRecordAlertIncidentWriter {
	return &WorkRecordAlertIncidentWriter{finder: finder, creator: creator}
}

func (w *WorkRecordAlertIncidentWriter) EnsureIncident(
	ctx context.Context,
	incident AlertIncident,
) error {
	if w == nil || w.finder == nil || w.creator == nil ||
		strings.TrimSpace(incident.ID) == "" ||
		strings.TrimSpace(incident.MSPID) == "" ||
		strings.TrimSpace(incident.ClientID) == "" ||
		strings.TrimSpace(incident.ActorID) == "" {
		return ErrInvalidSync
	}
	target := scope.Target{
		MSPID: incident.MSPID, ClientID: incident.ClientID,
	}
	if _, err := w.finder.Find(ctx, target, incident.ID); err == nil {
		return nil
	} else if !errors.Is(err, scope.ErrNotFound) {
		return err
	}
	principal := authorization.Principal{
		ID: incident.ActorID,
		Scope: scope.Principal{
			MSPID: incident.MSPID, ClientID: incident.ClientID,
		},
		Capabilities: authorization.NewCapabilitySet(
			"work_record.create",
		),
	}
	_, err := w.creator.CreateDattoIncident(
		ctx,
		workrecords.CreateCommand{
			RecordID: incident.ID, Principal: principal, Target: target,
			Actor: workrecords.Actor{
				Type: "integration", ID: incident.ActorID, Source: "datto",
			},
			DisplayID: incident.DisplayID, Type: workrecords.Incident,
			Title: incident.Title, Description: incident.Description,
			Status: "new", Priority: incident.Priority,
			ClassificationPolicy: tagging.CreationAllowFallback,
		},
	)
	return err
}
