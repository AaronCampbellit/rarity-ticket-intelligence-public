package projects

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/sales"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/tagging"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/tasks"
)

type conversionRepository struct {
	source     ConversionSource
	conversion *ConversionRecord
	accepted   ConversionMutation
	fail       error
	calls      int
}

func (r *conversionRepository) LoadConversionSource(
	context.Context,
	scope.Target,
	sales.OpportunityID,
	string,
	[]tasks.ID,
) (ConversionSource, error) {
	return r.source, nil
}

func (r *conversionRepository) FindOpportunityConversion(
	context.Context,
	scope.Target,
	sales.OpportunityID,
) (ConversionRecord, bool, error) {
	if r.conversion == nil {
		return ConversionRecord{}, false, nil
	}
	return *r.conversion, true, nil
}

func (r *conversionRepository) ConvertAtomic(_ context.Context, mutation ConversionMutation) error {
	r.calls++
	if r.fail != nil {
		return r.fail
	}
	r.accepted = mutation
	r.conversion = &mutation.Conversion
	return nil
}

func TestConversionPreviewIsDeterministicAndRequiresEveryProposalLineMapping(t *testing.T) {
	repository := &conversionRepository{source: validConversionSource()}
	service := conversionTestService(repository)
	command := validPreviewCommand()

	first, err := service.Preview(context.Background(), command)
	if err != nil {
		t.Fatalf("Preview() error = %v", err)
	}
	second, err := service.Preview(context.Background(), command)
	if err != nil {
		t.Fatalf("Preview() second error = %v", err)
	}
	if first.Hash == "" || first.Hash != second.Hash {
		t.Fatalf("preview hash is not deterministic: %q != %q", first.Hash, second.Hash)
	}
	if first.OriginalBudget.Minor != 12000 || first.PlannedMinutes != 600 {
		t.Fatalf("unexpected proposal baseline: %+v", first)
	}

	command.Phases = command.Phases[:1]
	if _, err := service.Preview(context.Background(), command); !errors.Is(err, ErrInvalidConversionMapping) {
		t.Fatalf("Preview() error = %v, want ErrInvalidConversionMapping", err)
	}
}

func TestConvertIsAtomicAndIdempotent(t *testing.T) {
	repository := &conversionRepository{source: validConversionSource()}
	service := conversionTestService(repository)
	previewCommand := validPreviewCommand()
	preview, err := service.Preview(context.Background(), previewCommand)
	if err != nil {
		t.Fatalf("Preview() error = %v", err)
	}
	command := ConversionCommand{
		ConversionPreviewCommand: previewCommand,
		PreviewHash:              preview.Hash,
		IdempotencyKey:           "request-1",
	}

	first, err := service.Convert(context.Background(), command)
	if err != nil {
		t.Fatalf("Convert() error = %v", err)
	}
	second, err := service.Convert(context.Background(), command)
	if err != nil {
		t.Fatalf("Convert() retry error = %v", err)
	}
	if first.ProjectID != second.ProjectID || repository.calls != 1 {
		t.Fatalf("retry was not idempotent: first=%+v second=%+v calls=%d", first, second, repository.calls)
	}
	if repository.accepted.Opportunity.StageID != "won-stage" ||
		repository.accepted.Opportunity.Version != 8 ||
		repository.accepted.Opportunity.ProspectID != "" ||
		len(repository.accepted.TaskMoves) != 1 {
		t.Fatalf("conversion mutation incomplete: %+v", repository.accepted)
	}
	move := repository.accepted.TaskMoves[0]
	if move.History.From.Type != tasks.ParentOpportunity ||
		move.History.To.Type != tasks.ParentProject ||
		move.History.PreviousVersion != 3 || move.History.AcceptedVersion != 4 {
		t.Fatalf("conversion did not preserve task movement history: %+v", move)
	}
}

func TestConvertRejectsStalePreviewAndLeavesAtomicFailureUncommitted(t *testing.T) {
	repository := &conversionRepository{
		source: validConversionSource(),
		fail:   errors.New("task movement failed"),
	}
	service := conversionTestService(repository)
	command := validPreviewCommand()
	if _, err := service.Convert(context.Background(), ConversionCommand{
		ConversionPreviewCommand: command, PreviewHash: "stale", IdempotencyKey: "request-1",
	}); !errors.Is(err, ErrStaleConversionPreview) {
		t.Fatalf("Convert() stale error = %v", err)
	}
	preview, err := service.Preview(context.Background(), command)
	if err != nil {
		t.Fatalf("Preview() error = %v", err)
	}
	if _, err := service.Convert(context.Background(), ConversionCommand{
		ConversionPreviewCommand: command, PreviewHash: preview.Hash, IdempotencyKey: "request-1",
	}); err == nil {
		t.Fatal("Convert() succeeded despite repository failure")
	}
	if repository.conversion != nil || repository.accepted.Project.ID != "" {
		t.Fatal("failed atomic conversion exposed partial state")
	}
}

func TestPreviewRequiresExactAcceptedProposalAndExplicitProspectMatch(t *testing.T) {
	source := validConversionSource()
	source.ProposalRecord.OpportunityID = "other-opportunity"
	service := conversionTestService(&conversionRepository{source: source})
	if _, err := service.Preview(context.Background(), validPreviewCommand()); !errors.Is(err, ErrAcceptedProposalRequired) {
		t.Fatalf("Preview() proposal relationship error = %v", err)
	}

	source = validConversionSource()
	source.Opportunity.ClientID = ""
	source.ProposalRecord.ClientID = ""
	source.ProposalVersion.ClientID = ""
	source.Acceptance.ClientID = ""
	source.Prospect = &sales.Prospect{ID: "prospect-id", MSPID: "msp-id", Name: "Possible Client"}
	source.CandidateClientIDs = []string{"existing-client"}
	service = conversionTestService(&conversionRepository{source: source})
	command := validPreviewCommand()
	command.Principal.Scope.ClientID = ""
	command.Target.ClientID = ""
	command.ExistingClientID = ""
	command.CreateClientFromProspect = true
	if _, err := service.Preview(context.Background(), command); !errors.Is(err, ErrClientMatchRequired) {
		t.Fatalf("Preview() duplicate-client error = %v", err)
	}
}

func TestPreviewRejectsProposalTotalsThatDoNotMatchMappedLines(t *testing.T) {
	source := validConversionSource()
	source.ProposalVersion.Total.Minor++
	service := conversionTestService(&conversionRepository{source: source})
	if _, err := service.Preview(context.Background(), validPreviewCommand()); !errors.Is(err, ErrInvalidConversion) {
		t.Fatalf("Preview() financial validation error = %v, want ErrInvalidConversion", err)
	}
}

func validConversionSource() ConversionSource {
	return ConversionSource{
		Opportunity: sales.Opportunity{
			ID: "opportunity-id", MSPID: "msp-id", ClientID: "client-id",
			DisplayID: "OPP-1", Name: "Managed migration", Version: 7,
		},
		ProposalRecord: sales.Proposal{
			ID: "proposal-id", MSPID: "msp-id", ClientID: "client-id",
			OpportunityID: "opportunity-id", State: sales.ProposalAccepted,
		},
		ProposalVersion: sales.ProposalVersion{
			ID: "proposal-version-id", ProposalID: "proposal-id",
			MSPID: "msp-id", ClientID: "client-id", State: sales.ProposalAccepted,
			Currency: "USD", Total: sales.Money{Minor: 12000, Currency: "USD"},
			Lines: []sales.ProposalLine{
				{ID: "line-1", Type: sales.FixedFee, Description: "Discover", Quantity: 1, UnitPrice: sales.Money{Minor: 2000, Currency: "USD"}, PlannedMinutes: 120},
				{ID: "line-2", Type: sales.TimeAndMaterials, Description: "Deliver", Quantity: 1, UnitPrice: sales.Money{Minor: 10000, Currency: "USD"}, PlannedMinutes: 480},
			},
			PDFSnapshotID: "snapshot-id",
		},
		Acceptance: sales.Acceptance{
			ID: "acceptance-id", ProposalVersionID: "proposal-version-id",
			MSPID: "msp-id", ClientID: "client-id", PDFSnapshotID: "snapshot-id",
		},
		ClosedWonStageID: "won-stage",
		SelectedTasks: []tasks.Task{{
			ID: "task-id", MSPID: "msp-id", ClientID: "client-id",
			Parent: tasks.Ref{
				Type: tasks.ParentOpportunity, ID: "opportunity-id",
				MSPID: "msp-id", ClientID: "client-id",
			},
			Status: "open", Version: 3,
		}},
	}
}

func validPreviewCommand() ConversionPreviewCommand {
	return ConversionPreviewCommand{
		Principal: authorization.Principal{
			Scope:        scope.Principal{MSPID: "msp-id", ClientID: "client-id"},
			Capabilities: authorization.NewCapabilitySet("opportunity.convert"),
		},
		Target:                     scope.Target{MSPID: "msp-id", ClientID: "client-id"},
		OpportunityID:              "opportunity-id",
		AcceptedProposalVersionID:  "proposal-version-id",
		ExpectedOpportunityVersion: 7,
		ExistingClientID:           "client-id",
		ProjectDisplayID:           "PRJ-1",
		ProjectName:                "Managed migration",
		PlannedStart:               time.Date(2026, time.August, 3, 0, 0, 0, 0, time.UTC),
		PlannedEnd:                 time.Date(2026, time.August, 28, 0, 0, 0, 0, time.UTC),
		Phases: []PhaseMapping{
			{Name: "Discover", ProposalLineIDs: []string{"line-1"}},
			{Name: "Deliver", ProposalLineIDs: []string{"line-2"}},
		},
		SelectedTaskIDs: []tasks.ID{"task-id"},
		TaskVersions:    map[tasks.ID]int64{"task-id": 3},
		ActorID:         "actor-id",
		Source:          "web",
	}
}

func conversionTestService(repository ConversionRepository) *ConversionService {
	ids := []string{
		"project-id", "phase-1", "phase-2", "conversion-id",
		"audit-id", "event-id", "correlation-id",
	}
	return NewConversionService(repository, func() time.Time {
		return time.Date(2026, time.July, 29, 12, 0, 0, 0, time.UTC)
	}, func() string {
		id := ids[0]
		ids = ids[1:]
		return id
	}, tagging.NewCreationPreparer(projectClassificationRepository{}))
}
