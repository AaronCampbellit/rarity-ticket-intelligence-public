package rtitools

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/aiassist"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/objectidentity"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/organizations"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/workrecords"
)

type WorkRecordAssignmentResolver interface {
	ResolveWorkRecordForAssignment(context.Context, scope.Target, string, int) ([]workrecords.Record, error)
}

type WorkRecordAssigner interface {
	Assign(context.Context, workrecords.AssignCommand) (workrecords.Record, error)
}

type ticketAssignRequest struct {
	Client          string `json:"client"`
	Ticket          string `json:"ticket"`
	Technician      string `json:"technician"`
	ExpectedVersion int64  `json:"expected_version"`
	Reason          string `json:"reason"`
}

type ticketAssignPrepared struct {
	ClientID                string `json:"client_id"`
	ClientReference         string `json:"client_reference"`
	ClientName              string `json:"client_name"`
	ClientDisplayID         string `json:"client_display_id"`
	ClientVersion           int64  `json:"client_version"`
	TicketID                string `json:"ticket_id"`
	TicketReference         string `json:"ticket_reference"`
	TicketDisplayID         string `json:"ticket_display_id"`
	TicketTitle             string `json:"ticket_title"`
	TicketVersion           int64  `json:"ticket_version"`
	CurrentOwnerID          string `json:"current_owner_id,omitempty"`
	CurrentOwnerDisplayName string `json:"current_owner_display_name,omitempty"`
	CurrentOwnerEmail       string `json:"current_owner_email,omitempty"`
	CurrentOwnerVersion     int64  `json:"current_owner_version,omitempty"`
	TechnicianID            string `json:"technician_id"`
	TechnicianReference     string `json:"technician_reference"`
	TechnicianDisplayName   string `json:"technician_display_name"`
	TechnicianEmail         string `json:"technician_email"`
	TechnicianVersion       int64  `json:"technician_version"`
	Reason                  string `json:"reason"`
}

func NewTicketAssignTool(
	directory ActiveClientResolver,
	resolver WorkRecordAssignmentResolver,
	technicians workrecords.AssignmentDirectory,
	assigner WorkRecordAssigner,
) aiassist.Tool {
	return &functionalTool{
		name: "ticket.assign", capability: "work_record.assign", kind: aiassist.ToolWrite,
		prepare: func(ctx context.Context, principal authorization.Principal, raw json.RawMessage) (json.RawMessage, error) {
			request, err := decode[ticketAssignRequest](raw)
			if err != nil || directory == nil || resolver == nil || technicians == nil || assigner == nil {
				return nil, aiassist.ErrInvalidTool
			}
			request.Client, request.Ticket = strings.TrimSpace(request.Client), strings.TrimSpace(request.Ticket)
			request.Technician, request.Reason = strings.TrimSpace(request.Technician), strings.TrimSpace(request.Reason)
			if request.Client == "" || objectidentity.Normalize(request.Ticket) == "" ||
				objectidentity.Normalize(request.Technician) == "" || request.ExpectedVersion < 1 || request.Reason == "" {
				return nil, aiassist.ErrInvalidTool
			}
			client, err := resolveActiveResourceClient(ctx, directory, principal, "work_record.assign", request.Client)
			if err != nil {
				return nil, err
			}
			target := target(principal, client.ID)
			ticket, err := resolveTicketAssignmentRecord(ctx, resolver, target, request.Ticket)
			if err != nil {
				return nil, err
			}
			if err := object.RequireVersion(ticket.Version, request.ExpectedVersion); err != nil {
				return nil, err
			}
			technician, err := resolveTicketAssignmentCandidate(ctx, technicians, target, request.Technician)
			if err != nil {
				return nil, err
			}
			if ticket.PrimaryOwnerID == technician.ID {
				return nil, workrecords.ErrInvalid
			}
			currentOwner, err := resolveTicketAssignmentOwner(ctx, technicians, target, ticket.PrimaryOwnerID)
			if err != nil {
				return nil, err
			}
			return marshalPrepared(ticketAssignPrepared{
				ClientID: client.ID, ClientReference: request.Client, ClientName: client.Name, ClientDisplayID: client.DisplayID, ClientVersion: client.Version,
				TicketID: ticket.ID, TicketReference: request.Ticket, TicketDisplayID: ticket.DisplayID, TicketTitle: ticket.Title,
				TicketVersion: ticket.Version, CurrentOwnerID: ticket.PrimaryOwnerID, CurrentOwnerDisplayName: currentOwner.DisplayName, CurrentOwnerEmail: currentOwner.Email, CurrentOwnerVersion: currentOwner.Version,
				TechnicianID: technician.ID, TechnicianReference: request.Technician, TechnicianDisplayName: technician.DisplayName,
				TechnicianEmail: technician.Email, TechnicianVersion: technician.Version, Reason: request.Reason,
			})
		},
		validate: validateTicketAssign,
		resolve:  resolveClientResourceScope,
		preview: func(ctx context.Context, principal authorization.Principal, raw json.RawMessage) (aiassist.Preview, error) {
			input, err := validTicketAssign(raw)
			if err != nil {
				return aiassist.Preview{}, err
			}
			client, err := resolveActiveResourceClient(ctx, directory, principal, "work_record.assign", input.ClientReference)
			if err != nil || !sameTicketAssignClient(input, client) {
				return aiassist.Preview{}, aiassist.ErrProposalStale
			}
			ticket, err := resolveTicketAssignmentRecord(ctx, resolver, target(principal, input.ClientID), input.TicketReference)
			if err != nil || !sameTicketAssignRecord(input, ticket) || ticket.PrimaryOwnerID != input.CurrentOwnerID || ticket.Version != input.TicketVersion {
				return aiassist.Preview{}, aiassist.ErrProposalStale
			}
			technician, err := resolveTicketAssignmentCandidate(ctx, technicians, target(principal, input.ClientID), input.TechnicianReference)
			if err != nil || !sameTicketAssignCandidate(input, technician) || ticket.PrimaryOwnerID == technician.ID {
				return aiassist.Preview{}, aiassist.ErrProposalStale
			}
			currentOwner, err := resolveTicketAssignmentOwner(ctx, technicians, target(principal, input.ClientID), ticket.PrimaryOwnerID)
			if err != nil || !sameTicketAssignOwner(input, currentOwner) {
				return aiassist.Preview{}, aiassist.ErrProposalStale
			}
			return ticketAssignPreview(input), nil
		},
		execute: func(ctx context.Context, principal authorization.Principal, raw json.RawMessage, correlationID string) (aiassist.ToolResult, error) {
			input, err := validTicketAssign(raw)
			if err != nil || strings.TrimSpace(correlationID) == "" || assigner == nil {
				return aiassist.ToolResult{}, aiassist.ErrInvalidTool
			}
			record, err := assigner.Assign(ctx, workrecords.AssignCommand{
				Principal: principal, Target: target(principal, input.ClientID), WorkRecordID: input.TicketID,
				ExpectedVersion: input.TicketVersion, ExpectedClientVersion: input.ClientVersion, ExpectedOwnerVersion: input.TechnicianVersion,
				OwnerID: input.TechnicianID, Reason: input.Reason,
				Actor: workrecords.Actor{Type: "technician", ID: principal.ID, Source: "ai_workspace"}, CausationID: correlationID,
			})
			if err != nil {
				return aiassist.ToolResult{}, err
			}
			return aiassist.ToolResult{Summary: "Assigned ticket " + input.TicketDisplayID + " to " + input.TechnicianDisplayName, Data: recordData(record)}, nil
		},
	}
}

func resolveTicketAssignmentOwner(ctx context.Context, directory workrecords.AssignmentDirectory, target scope.Target, id string) (workrecords.AssignmentCandidate, error) {
	if id == "" {
		return workrecords.AssignmentCandidate{}, nil
	}
	owner, err := directory.FindAssignmentIdentity(ctx, target, id)
	if err != nil {
		return workrecords.AssignmentCandidate{}, err
	}
	if owner.ID != id || owner.MSPID != target.MSPID || strings.TrimSpace(owner.DisplayName) == "" || strings.TrimSpace(owner.Email) == "" || owner.Version < 1 {
		return workrecords.AssignmentCandidate{}, scope.ErrNotFound
	}
	return owner, nil
}

func resolveTicketAssignmentRecord(ctx context.Context, resolver WorkRecordAssignmentResolver, target scope.Target, reference string) (workrecords.Record, error) {
	if resolver == nil || target.MSPID == "" || target.ClientID == "" || objectidentity.Normalize(reference) == "" {
		return workrecords.Record{}, aiassist.ErrInvalidTool
	}
	records, err := resolver.ResolveWorkRecordForAssignment(ctx, target, objectidentity.Normalize(reference), 2)
	if err != nil {
		return workrecords.Record{}, err
	}
	byDisplayID, byTitle := make([]workrecords.Record, 0, 2), make([]workrecords.Record, 0, 2)
	for _, record := range records {
		if record.ID == "" || record.MSPID != target.MSPID || record.ClientID != target.ClientID ||
			record.LifecycleState != "active" || record.Version < 1 {
			return workrecords.Record{}, scope.ErrNotFound
		}
		if objectidentity.Normalize(record.DisplayID) == objectidentity.Normalize(reference) {
			byDisplayID = append(byDisplayID, record)
		} else if objectidentity.Normalize(record.Title) == objectidentity.Normalize(reference) {
			byTitle = append(byTitle, record)
		}
	}
	if len(byDisplayID) == 1 {
		return byDisplayID[0], nil
	}
	if len(byDisplayID) == 0 && len(byTitle) == 1 {
		return byTitle[0], nil
	}
	return workrecords.Record{}, scope.ErrNotFound
}

func resolveTicketAssignmentCandidate(ctx context.Context, directory workrecords.AssignmentDirectory, target scope.Target, reference string) (workrecords.AssignmentCandidate, error) {
	candidate, err := workrecords.ResolveAssignmentCandidate(ctx, directory, target, reference)
	if err != nil {
		return workrecords.AssignmentCandidate{}, err
	}
	if candidate.ID == "" || candidate.MSPID != target.MSPID || strings.TrimSpace(candidate.DisplayName) == "" || strings.TrimSpace(candidate.Email) == "" || candidate.Version < 1 {
		return workrecords.AssignmentCandidate{}, scope.ErrNotFound
	}
	normalized := objectidentity.Normalize(reference)
	if normalized != objectidentity.Normalize(candidate.DisplayName) && normalized != objectidentity.Normalize(candidate.Email) {
		return workrecords.AssignmentCandidate{}, scope.ErrNotFound
	}
	return candidate, nil
}

func validateTicketAssign(raw json.RawMessage) error { _, err := validTicketAssign(raw); return err }

func validTicketAssign(raw json.RawMessage) (ticketAssignPrepared, error) {
	input, err := decode[ticketAssignPrepared](raw)
	if err != nil || strings.TrimSpace(input.ClientID) == "" || strings.TrimSpace(input.ClientReference) == "" ||
		strings.TrimSpace(input.ClientName) == "" || strings.TrimSpace(input.ClientDisplayID) == "" || input.ClientVersion < 1 ||
		strings.TrimSpace(input.TicketID) == "" || objectidentity.Normalize(input.TicketReference) == "" ||
		strings.TrimSpace(input.TicketDisplayID) == "" || strings.TrimSpace(input.TicketTitle) == "" || input.TicketVersion < 1 ||
		strings.TrimSpace(input.TechnicianID) == "" || objectidentity.Normalize(input.TechnicianReference) == "" ||
		strings.TrimSpace(input.TechnicianDisplayName) == "" || strings.TrimSpace(input.TechnicianEmail) == "" || input.TechnicianVersion < 1 ||
		strings.TrimSpace(input.Reason) == "" ||
		(input.CurrentOwnerID == "" && (input.CurrentOwnerDisplayName != "" || input.CurrentOwnerEmail != "" || input.CurrentOwnerVersion != 0)) ||
		(input.CurrentOwnerID != "" && (strings.TrimSpace(input.CurrentOwnerDisplayName) == "" || strings.TrimSpace(input.CurrentOwnerEmail) == "" || input.CurrentOwnerVersion < 1)) {
		return ticketAssignPrepared{}, aiassist.ErrInvalidTool
	}
	return input, nil
}

func sameTicketAssignClient(input ticketAssignPrepared, client organizations.Client) bool {
	return client.ID == input.ClientID && client.Name == input.ClientName && client.DisplayID == input.ClientDisplayID && client.Version == input.ClientVersion
}

func sameTicketAssignOwner(input ticketAssignPrepared, owner workrecords.AssignmentCandidate) bool {
	if input.CurrentOwnerID == "" {
		return owner == (workrecords.AssignmentCandidate{})
	}
	return owner.ID == input.CurrentOwnerID && owner.DisplayName == input.CurrentOwnerDisplayName &&
		owner.Email == input.CurrentOwnerEmail && owner.Version == input.CurrentOwnerVersion
}

func sameTicketAssignRecord(input ticketAssignPrepared, record workrecords.Record) bool {
	return record.ID == input.TicketID && record.DisplayID == input.TicketDisplayID && record.Title == input.TicketTitle &&
		record.Version == input.TicketVersion && record.MSPID != "" && record.ClientID == input.ClientID
}

func sameTicketAssignCandidate(input ticketAssignPrepared, candidate workrecords.AssignmentCandidate) bool {
	return candidate.ID == input.TechnicianID && candidate.DisplayName == input.TechnicianDisplayName &&
		candidate.Email == input.TechnicianEmail && candidate.Version == input.TechnicianVersion
}

func ticketAssignPreview(input ticketAssignPrepared) aiassist.Preview {
	return aiassist.Preview{
		Summary:    "Assign ticket " + input.TicketDisplayID + " to " + input.TechnicianDisplayName,
		TargetType: "work_record", TargetID: input.TicketID, TargetVersion: input.TicketVersion,
		Changes: map[string]aiassist.Change{
			"client": {Before: nil, After: map[string]string{"display_id": input.ClientDisplayID, "name": input.ClientName}},
			"ticket": {Before: map[string]any{"display_id": input.TicketDisplayID, "title": input.TicketTitle, "version": input.TicketVersion}, After: map[string]any{"display_id": input.TicketDisplayID, "title": input.TicketTitle, "version": input.TicketVersion + 1}},
			"owner":  {Before: ticketAssignOwner(input.CurrentOwnerID, input.CurrentOwnerDisplayName, input.CurrentOwnerEmail, input.CurrentOwnerVersion), After: ticketAssignOwner(input.TechnicianID, input.TechnicianDisplayName, input.TechnicianEmail, input.TechnicianVersion)},
			"reason": {Before: nil, After: input.Reason},
		},
	}
}

func ticketAssignOwner(id, displayName, email string, version int64) any {
	if id == "" {
		return "Unassigned"
	}
	return map[string]any{"display_name": displayName, "email": email}
}
