package psa_test

import (
	"context"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/calendar"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/calendar/adapters"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/commitments"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/customfields"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/httpapi"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/id"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/notifications"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/projects"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/store/psa"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/views"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/workforce"
	"testing"
	"time"
)

var browserCalendarCapabilities = []string{"calendar.read", "calendar.schedule", "calendar.commitment.manage", "calendar.policy.manage", "calendar.workforce.manage", "project.read", "project.edit", "work_record.read", "work_record.edit", "view.save", "view.share"}

func attachCalendarBrowserServices(t *testing.T, ctx context.Context, pool *pgxpool.Pool, f classificationBrowserFixture, d *httpapi.Dependencies) {
	t.Helper()
	roleID := id.New()
	if _, err := pool.Exec(ctx, `INSERT INTO roles(id,msp_id,key,name) VALUES($1,$2,'calendar-browser','Calendar browser')`, roleID, f.MSPID); err != nil {
		t.Fatal(err)
	}
	for _, capability := range browserCalendarCapabilities {
		if _, err := pool.Exec(ctx, `INSERT INTO role_capabilities(role_id,msp_id,capability) VALUES($1,$2,$3)`, roleID, f.MSPID, capability); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(ctx, `INSERT INTO role_assignments(id,msp_id,technician_id,role_id,granted_by) VALUES($1,$2,$3,$4,$3)`, id.New(), f.MSPID, f.ActorID, roleID); err != nil {
		t.Fatal(err)
	}
	repository := psa.NewCalendarRepositoryFromPool(pool)
	roles, err := calendar.NewProductionRoleRegistry(nil)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := adapters.NewProductionAdapterRegistry(adapters.Dependencies{Work: repository, Projects: repository, Workforce: repository, Commitments: repository, CustomDates: repository})
	if err != nil {
		t.Fatal(err)
	}
	writes := calendar.NewWriteAdapterRegistry()
	for _, adapter := range []calendar.WriteAdapter{adapters.NewWorkRecordAdapter(repository), adapters.NewTaskAdapter(repository), adapters.NewProjectAdapter(repository), adapters.NewPhaseAdapter(repository), adapters.NewMilestoneAdapter(repository), adapters.NewResourcePlanAdapter(repository), adapters.NewScheduleAdapter(repository), adapters.NewPTOAdapter(repository), adapters.NewMaintenanceAdapter(repository), adapters.NewCommercialAdapter(repository)} {
		if err = writes.Register(adapter); err != nil {
			t.Fatal(err)
		}
	}
	d.CalendarQueries = calendar.NewQueryService(repository, repository, nil)
	d.CalendarLive = calendar.NewLiveService(repository, repository, nil)
	d.CalendarProposals = calendar.NewProposalService(calendar.ProposalServiceDependencies{Projections: repository, Workforce: repository, Impacts: calendar.NewSchedulingImpactService(repository), Store: repository, UnitOfWork: calendar.NewScheduleUnitOfWork(repository, writes, time.Now, id.New), RevisionValidator: repository, Adapters: writes, Roles: roles, Now: time.Now, NewID: id.New})
	d.CalendarDependencies = calendar.NewDependencyService(repository, time.Now, id.New, calendar.WithDependencyRoleRegistry(roles))
	d.CalendarConfiguration = calendar.NewConfigurationService(repository, time.Now, id.New)
	d.CalendarConfigurationQueries = repository
	d.CalendarCustomDateQueries = repository
	d.CalendarCustomDateValues = customfields.NewDateService(psa.NewCustomDateRepositoryFromPool(pool), time.Now, id.New)
	d.CalendarPreferences = notifications.NewCalendarPreferenceService(psa.NewCalendarNotificationRepositoryFromPool(pool, id.New), time.Now, id.New)
	workforceRepository := psa.NewWorkforceRepositoryFromPool(pool)
	schedule := workforce.NewScheduleService(workforceRepository, time.Now, id.New)
	d.WorkforceSchedules = schedule
	d.WorkforceScheduleExceptions = schedule
	d.WorkforcePTO = workforce.NewPTOService(workforceRepository, time.Now, id.New)
	d.WorkforceQueries = repository
	d.ProjectMilestones = projects.NewMilestoneService(psa.NewProjectRepositoryFromPool(pool), time.Now, id.New)
	d.ProjectMilestoneQueries = repository
	commitmentsRepository := psa.NewCommitmentRepositoryFromPool(pool)
	d.MaintenanceWindows = commitments.NewMaintenanceService(commitmentsRepository, time.Now, id.New)
	d.CommercialCommitments = commitments.NewCommercialService(commitmentsRepository, time.Now, id.New)
	d.CommitmentQueries = repository
	d.Views = views.NewService(psa.NewViewRepositoryFromPool(pool), id.New)
	worker := calendar.NewProjectionWorker(repository, registry, calendar.NewProjectionService(repository, roles), "calendar-browser")
	d.CalendarProjectionWorker = worker
	d.CalendarProjectionEvents = repository
	go func() {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				events, err := repository.ListCalendarProjectionEvents(ctx, f.MSPID, "calendar-browser", 100)
				if err != nil {
					continue
				}
				for _, event := range events {
					if err = worker.Handle(ctx, event); err != nil {
						break
					}
				}
			}
		}
	}()

}
