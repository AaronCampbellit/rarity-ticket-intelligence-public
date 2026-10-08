package projects

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mutation"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/sales"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/tagging"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/tasks"
)

var (
	ErrInvalidConversion        = errors.New("invalid opportunity conversion")
	ErrInvalidConversionMapping = errors.New("invalid proposal line mapping")
	ErrAcceptedProposalRequired = errors.New("accepted proposal version required")
	ErrClientMatchRequired      = errors.New("explicit client match required")
	ErrStaleConversionPreview   = errors.New("stale conversion preview")
	ErrOpportunityConverted     = errors.New("opportunity already converted")
)

type ConversionRepository interface {
	LoadConversionSource(
		context.Context,
		scope.Target,
		sales.OpportunityID,
		string,
		[]tasks.ID,
	) (ConversionSource, error)
	FindOpportunityConversion(
		context.Context,
		scope.Target,
		sales.OpportunityID,
	) (ConversionRecord, bool, error)
	ConvertAtomic(context.Context, ConversionMutation) error
}

type ConversionService struct {
	repository ConversionRepository
	now        func() time.Time
	newID      func() string
	creation   *tagging.CreationPreparer
}

func NewConversionService(
	repository ConversionRepository,
	now func() time.Time,
	newID func() string,
	creation *tagging.CreationPreparer,
) *ConversionService {
	return &ConversionService{repository: repository, now: now, newID: newID, creation: creation}
}

func (s *ConversionService) Preview(
	ctx context.Context,
	command ConversionPreviewCommand,
) (ConversionPreview, error) {
	target, err := conversionTarget(command)
	if err != nil {
		return ConversionPreview{}, err
	}
	if err := authorization.Authorize(command.Principal, "opportunity.convert", target); err != nil {
		return ConversionPreview{}, err
	}
	source, err := s.repository.LoadConversionSource(
		ctx, target, command.OpportunityID,
		command.AcceptedProposalVersionID, command.SelectedTaskIDs,
	)
	if err != nil {
		return ConversionPreview{}, err
	}
	return buildConversionPreview(command, source, target)
}

func (s *ConversionService) Convert(
	ctx context.Context,
	command ConversionCommand,
) (ConversionResult, error) {
	if s == nil || s.repository == nil || s.now == nil || s.newID == nil || s.creation == nil {
		return ConversionResult{}, ErrInvalidConversion
	}
	target, err := conversionTarget(command.ConversionPreviewCommand)
	if err != nil || strings.TrimSpace(command.PreviewHash) == "" ||
		strings.TrimSpace(command.IdempotencyKey) == "" {
		return ConversionResult{}, ErrInvalidConversion
	}
	if err := authorization.Authorize(command.Principal, "opportunity.convert", target); err != nil {
		return ConversionResult{}, err
	}
	existing, found, err := s.repository.FindOpportunityConversion(ctx, target, command.OpportunityID)
	if err != nil {
		return ConversionResult{}, err
	}
	if found {
		if existing.RequestKey != command.IdempotencyKey ||
			existing.PreviewHash != command.PreviewHash {
			return ConversionResult{}, ErrOpportunityConverted
		}
		return ConversionResult{
			ProjectID: existing.ProjectID, ClientID: existing.ClientID,
			ConversionID: existing.ID, AlreadyExists: true,
		}, nil
	}

	source, err := s.repository.LoadConversionSource(
		ctx, target, command.OpportunityID,
		command.AcceptedProposalVersionID, command.SelectedTaskIDs,
	)
	if err != nil {
		return ConversionResult{}, err
	}
	preview, err := buildConversionPreview(command.ConversionPreviewCommand, source, target)
	if err != nil {
		return ConversionResult{}, err
	}
	if preview.Hash != command.PreviewHash {
		return ConversionResult{}, ErrStaleConversionPreview
	}

	now := s.now().UTC()
	clientID := preview.Client.ClientID
	var client *ClientSeed
	if preview.Client.Action == "create" {
		clientID = s.newID()
		client = &ClientSeed{
			ID: clientID, MSPID: target.MSPID, ProspectID: source.Prospect.ID,
			DisplayID: source.Prospect.DisplayID, Name: source.Prospect.Name,
			Email: source.Prospect.Email, Phone: source.Prospect.Phone,
			CreatedAt: now, CreatedBy: command.ActorID,
		}
	}
	projectID := ProjectID(s.newID())
	phases := make([]Phase, 0, len(command.Phases))
	for index, mapping := range command.Phases {
		phasePreview := preview.Phases[index]
		phases = append(phases, Phase{
			ID: PhaseID(s.newID()), ProjectID: projectID,
			MSPID: target.MSPID, ClientID: clientID,
			Position: index + 1, Name: strings.TrimSpace(mapping.Name), State: "planned",
			OwnerID:            mapping.OwnerID,
			ParticipatingTeams: append([]string(nil), mapping.ParticipatingTeams...),
			PlannedStart:       mapping.PlannedStart, PlannedEnd: mapping.PlannedEnd,
			PlannedMinutes: phasePreview.PlannedMinutes, Budget: phasePreview.Budget,
			Version: 1,
		})
	}
	project := Project{
		ID: projectID, MSPID: target.MSPID, ClientID: clientID,
		DisplayID:                 strings.TrimSpace(command.ProjectDisplayID),
		Name:                      strings.TrimSpace(command.ProjectName),
		OriginalProposalVersionID: source.ProposalVersion.ID,
		LifecycleState:            "planned", PlannedStart: command.PlannedStart,
		PlannedEnd:       command.PlannedEnd,
		OriginalBaseline: preview.OriginalBaseline,
		CurrentBaseline:  preview.OriginalBaseline,
		Version:          1, Phases: phases,
		SupportsMilestones: false, SupportsTaskDependencies: false,
		CreatedAt: now, CreatedBy: command.ActorID,
	}
	initial, err := s.initialTags(ctx, target, clientID, command)
	if err != nil {
		return ConversionResult{}, err
	}
	taskMoves := make([]TaskMove, 0, len(source.SelectedTasks))
	for _, task := range source.SelectedTasks {
		previous := task.Version
		task.Parent = tasks.Ref{
			Type: tasks.ParentProject, ID: string(projectID),
			MSPID: target.MSPID, ClientID: clientID,
		}
		task.WorkRecordID = ""
		task.Version++
		taskMoves = append(taskMoves, TaskMove{Task: task, PreviousVersion: previous})
	}
	opportunity := source.Opportunity
	opportunity.ClientID = clientID
	opportunity.ProspectID = ""
	opportunity.StageID = source.ClosedWonStageID
	opportunity.Version++
	opportunity.UpdatedAt, opportunity.UpdatedBy = now, command.ActorID

	conversionID, auditID, eventID, correlationID := s.newID(), s.newID(), s.newID(), s.newID()
	for index := range taskMoves {
		taskMoves[index].History = tasks.MovementHistory{
			TaskID: taskMoves[index].Task.ID,
			From: tasks.Ref{
				Type: tasks.ParentOpportunity, ID: string(opportunity.ID),
				MSPID: target.MSPID, ClientID: source.SelectedTasks[index].ClientID,
			},
			To:              taskMoves[index].Task.Parent,
			PreviousVersion: taskMoves[index].PreviousVersion,
			AcceptedVersion: taskMoves[index].Task.Version,
			MovedAt:         now, MovedBy: command.ActorID,
		}
	}
	record := ConversionRecord{
		ID: conversionID, OpportunityID: opportunity.ID,
		ProposalVersionID: source.ProposalVersion.ID, ProjectID: projectID,
		MSPID: target.MSPID, ClientID: clientID,
		RequestKey: command.IdempotencyKey, PreviewHash: preview.Hash,
		Snapshot: preview, ConvertedAt: now, ConvertedBy: command.ActorID,
	}
	baseline := preview.OriginalBaseline
	accepted := ConversionMutation{
		Client: client, Project: project,
		InitialTags:    initial.WithProvenance(tagging.InitialAssignmentProvenance{ActorType: "technician", ActorID: command.ActorID, OccurredAt: now, CorrelationID: correlationID}),
		OriginalBudget: baseline, CurrentBudget: baseline,
		Opportunity: opportunity, TaskMoves: taskMoves, Conversion: record,
		Audit: mutation.AuditRecord{
			ID: auditID, OccurredAt: now, MSPID: target.MSPID, ClientID: clientID,
			ActorType: "technician", ActorID: command.ActorID,
			Action: "opportunity.converted", SubjectType: "project",
			SubjectID: string(projectID), SubjectVersion: 1,
			Source: command.Source, CorrelationID: correlationID,
		},
		Event: mutation.EventRecord{
			EventID: eventID, EventType: "opportunity.converted", SchemaVersion: 1,
			OccurredAt: now, MSPID: target.MSPID, ClientID: clientID,
			ActorType: "technician", ActorID: command.ActorID,
			SubjectType: "project", SubjectID: string(projectID), SubjectVersion: 1,
			Source: command.Source, CorrelationID: correlationID,
		},
	}
	if err := s.repository.ConvertAtomic(ctx, accepted); err != nil {
		return ConversionResult{}, err
	}
	return ConversionResult{
		ProjectID: projectID, ClientID: clientID, ConversionID: conversionID,
	}, nil
}

func (s *ConversionService) initialTags(ctx context.Context, target scope.Target, clientID string, command ConversionCommand) (tagging.InitialAssignmentSet, error) {
	return s.creation.Prepare(ctx, tagging.PrepareCreationCommand{MSPID: target.MSPID, ClientID: clientID, ObjectType: tagging.ObjectProject, TagIDs: command.TagIDs, Source: tagging.SourceAutomation, Policy: tagging.CreationAllowFallback})
}

func conversionTarget(command ConversionPreviewCommand) (scope.Target, error) {
	target := command.Target
	if target.MSPID == "" {
		target = scope.Target{
			MSPID:    command.Principal.Scope.MSPID,
			ClientID: command.Principal.Scope.ClientID,
		}
	}
	if target.MSPID == "" || command.OpportunityID == "" ||
		strings.TrimSpace(command.AcceptedProposalVersionID) == "" ||
		command.ExpectedOpportunityVersion < 1 ||
		strings.TrimSpace(command.ProjectDisplayID) == "" ||
		strings.TrimSpace(command.ProjectName) == "" ||
		len(command.Phases) == 0 || strings.TrimSpace(command.ActorID) == "" ||
		strings.TrimSpace(command.Source) == "" ||
		(!command.PlannedEnd.IsZero() && command.PlannedEnd.Before(command.PlannedStart)) {
		return scope.Target{}, ErrInvalidConversion
	}
	return target, nil
}

func buildConversionPreview(
	command ConversionPreviewCommand,
	source ConversionSource,
	target scope.Target,
) (ConversionPreview, error) {
	if source.Opportunity.ID != command.OpportunityID ||
		source.Opportunity.MSPID != target.MSPID ||
		(target.ClientID != "" && source.Opportunity.ClientID != target.ClientID) {
		return ConversionPreview{}, scope.ErrNotFound
	}
	if err := object.RequireVersion(
		source.Opportunity.Version, command.ExpectedOpportunityVersion,
	); err != nil {
		return ConversionPreview{}, err
	}
	if source.ProposalRecord.ID != source.ProposalVersion.ProposalID ||
		source.ProposalRecord.OpportunityID != string(source.Opportunity.ID) ||
		source.ProposalRecord.MSPID != target.MSPID ||
		source.ProposalRecord.State != sales.ProposalAccepted ||
		source.ProposalVersion.ID != command.AcceptedProposalVersionID ||
		source.ProposalVersion.MSPID != target.MSPID ||
		source.ProposalVersion.State != sales.ProposalAccepted ||
		source.ProposalVersion.PDFSnapshotID == "" ||
		source.Acceptance.ID == "" ||
		source.Acceptance.MSPID != target.MSPID ||
		source.Acceptance.ProposalVersionID != source.ProposalVersion.ID ||
		source.Acceptance.PDFSnapshotID != source.ProposalVersion.PDFSnapshotID ||
		source.ClosedWonStageID == "" {
		return ConversionPreview{}, ErrAcceptedProposalRequired
	}
	client, err := resolveConversionClient(command, source)
	if err != nil {
		return ConversionPreview{}, err
	}
	phases, plannedMinutes, err := mapConversionPhases(command.Phases, source.ProposalVersion)
	if err != nil {
		return ConversionPreview{}, err
	}
	taskPreviews, err := validateConversionTasks(command, source, target)
	if err != nil {
		return ConversionPreview{}, err
	}
	baseline := BudgetBaseline{
		Currency: source.ProposalVersion.Currency, RevenueMinor: source.ProposalVersion.Total.Minor,
		CostMinor: source.ProposalVersion.Cost.Minor, PlannedMinutes: plannedMinutes,
	}
	var mappedRevenue int64
	for _, phase := range phases {
		mappedRevenue += phase.Budget.Minor
	}
	if len(baseline.Currency) != 3 || baseline.RevenueMinor < 0 ||
		baseline.CostMinor < 0 || baseline.CostMinor > baseline.RevenueMinor ||
		mappedRevenue != baseline.RevenueMinor {
		return ConversionPreview{}, ErrInvalidConversion
	}
	preview := ConversionPreview{
		OpportunityID: source.Opportunity.ID, ProposalVersionID: source.ProposalVersion.ID,
		Client: client, ProjectDisplayID: strings.TrimSpace(command.ProjectDisplayID),
		ProjectName:  strings.TrimSpace(command.ProjectName),
		PlannedStart: command.PlannedStart, PlannedEnd: command.PlannedEnd,
		Phases: phases, Tasks: taskPreviews, OriginalBaseline: baseline,
		OriginalBudget: Money{Minor: baseline.RevenueMinor, Currency: baseline.Currency},
		PlannedMinutes: baseline.PlannedMinutes,
	}
	encoded, err := json.Marshal(preview)
	if err != nil {
		return ConversionPreview{}, err
	}
	sum := sha256.Sum256(encoded)
	preview.Hash = hex.EncodeToString(sum[:])
	return preview, nil
}

func resolveConversionClient(
	command ConversionPreviewCommand,
	source ConversionSource,
) (ClientPreview, error) {
	if source.Opportunity.ClientID != "" {
		if command.CreateClientFromProspect ||
			command.ExistingClientID != source.Opportunity.ClientID {
			return ClientPreview{}, ErrClientMatchRequired
		}
		return ClientPreview{Action: "match", ClientID: source.Opportunity.ClientID}, nil
	}
	if source.Prospect == nil || source.Prospect.ID == "" {
		return ClientPreview{}, ErrClientMatchRequired
	}
	if command.ExistingClientID != "" {
		if !containsString(source.CandidateClientIDs, command.ExistingClientID) {
			return ClientPreview{}, ErrClientMatchRequired
		}
		return ClientPreview{
			Action: "match", ClientID: command.ExistingClientID,
			ProspectID: source.Prospect.ID, Name: source.Prospect.Name,
		}, nil
	}
	if !command.CreateClientFromProspect || len(source.CandidateClientIDs) != 0 {
		return ClientPreview{}, ErrClientMatchRequired
	}
	return ClientPreview{
		Action: "create", ProspectID: source.Prospect.ID, Name: source.Prospect.Name,
	}, nil
}

func mapConversionPhases(
	mappings []PhaseMapping,
	proposal sales.ProposalVersion,
) ([]PhasePreview, int64, error) {
	lines := make(map[string]sales.ProposalLine, len(proposal.Lines))
	for _, line := range proposal.Lines {
		if line.ID == "" {
			return nil, 0, ErrInvalidConversionMapping
		}
		lines[line.ID] = line
	}
	mapped := make(map[string]struct{}, len(lines))
	phases := make([]PhasePreview, 0, len(mappings))
	var totalMinutes int64
	for index, mapping := range mappings {
		if strings.TrimSpace(mapping.Name) == "" || len(mapping.ProposalLineIDs) == 0 ||
			(!mapping.PlannedEnd.IsZero() && mapping.PlannedEnd.Before(mapping.PlannedStart)) {
			return nil, 0, ErrInvalidConversionMapping
		}
		phase := PhasePreview{
			Position: index + 1, Name: strings.TrimSpace(mapping.Name),
			ProposalLineIDs: append([]string(nil), mapping.ProposalLineIDs...),
			Budget:          Money{Currency: proposal.Currency},
		}
		for _, lineID := range mapping.ProposalLineIDs {
			line, exists := lines[lineID]
			if !exists {
				return nil, 0, ErrInvalidConversionMapping
			}
			if _, duplicate := mapped[lineID]; duplicate {
				return nil, 0, ErrInvalidConversionMapping
			}
			mapped[lineID] = struct{}{}
			lineRevenue := line.Quantity*line.UnitPrice.Minor - line.Discount.Minor + line.Tax.Minor
			if line.Quantity < 0 || line.PlannedMinutes < 0 ||
				line.UnitPrice.Minor < 0 || line.UnitCost.Minor < 0 ||
				line.Discount.Minor < 0 || line.Tax.Minor < 0 ||
				line.UnitPrice.Currency != proposal.Currency ||
				line.UnitCost.Currency != "" && line.UnitCost.Currency != proposal.Currency ||
				line.Discount.Currency != "" && line.Discount.Currency != proposal.Currency ||
				line.Tax.Currency != "" && line.Tax.Currency != proposal.Currency ||
				lineRevenue < 0 {
				return nil, 0, ErrInvalidConversionMapping
			}
			phase.PlannedMinutes += line.PlannedMinutes
			phase.Budget.Minor += lineRevenue
		}
		totalMinutes += phase.PlannedMinutes
		phases = append(phases, phase)
	}
	if len(mapped) != len(lines) {
		return nil, 0, ErrInvalidConversionMapping
	}
	return phases, totalMinutes, nil
}

func validateConversionTasks(
	command ConversionPreviewCommand,
	source ConversionSource,
	target scope.Target,
) ([]TaskPreview, error) {
	if len(source.SelectedTasks) != len(command.SelectedTaskIDs) {
		return nil, tasks.ErrInvalidMove
	}
	loaded := make(map[tasks.ID]tasks.Task, len(source.SelectedTasks))
	for _, task := range source.SelectedTasks {
		loaded[task.ID] = task
	}
	result := make([]TaskPreview, 0, len(command.SelectedTaskIDs))
	seen := make(map[tasks.ID]struct{}, len(command.SelectedTaskIDs))
	for _, taskID := range command.SelectedTaskIDs {
		if taskID == "" {
			return nil, tasks.ErrInvalidMove
		}
		if _, duplicate := seen[taskID]; duplicate {
			return nil, tasks.ErrInvalidMove
		}
		seen[taskID] = struct{}{}
		task, exists := loaded[taskID]
		expected, versionExists := command.TaskVersions[taskID]
		if !exists || !versionExists || task.Version != expected {
			return nil, tasks.ErrStaleTask
		}
		if task.Status == "completed" ||
			task.MSPID != target.MSPID ||
			task.Parent.Type != tasks.ParentOpportunity ||
			task.Parent.ID != string(command.OpportunityID) {
			return nil, tasks.ErrInvalidMove
		}
		if target.ClientID != "" && task.ClientID != target.ClientID {
			return nil, scope.ErrNotFound
		}
		result = append(result, TaskPreview{ID: taskID, Version: task.Version})
	}
	return result, nil
}

func containsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
