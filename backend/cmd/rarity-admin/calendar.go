package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/calendar"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/calendar/adapters"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/id"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/store/psa"
	"io"
	"strings"
	"time"
)

type calendarReconcileFunc func(context.Context, string, calendar.ReconcileRequest) (calendar.ReconcileReport, error)

func runCalendarReconcile(ctx context.Context, args []string, lookup func(string) string, stdout, stderr io.Writer, reconcile calendarReconcileFunc) error {
	flags := flag.NewFlagSet("calendar-reconcile", flag.ContinueOnError)
	flags.SetOutput(stderr)
	msp := flags.String("msp-id", "", "MSP UUID")
	limit := flags.Int("limit", 500, "maximum sources to inspect (1–5000)")
	repair := flags.Bool("repair", false, "repair missing, stale, and orphaned calendar projections")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 || !id.ValidCanonical(*msp) || *limit < 1 || *limit > 5000 {
		return errors.New("a canonical --msp-id and --limit from 1 to 5000 are required")
	}
	databaseURL := strings.TrimSpace(lookup("DATABASE_URL"))
	if databaseURL == "" {
		return errors.New("DATABASE_URL is required")
	}
	if reconcile == nil {
		return errors.New("calendar reconciliation is unavailable")
	}
	report, err := reconcile(ctx, databaseURL, calendar.ReconcileRequest{MSPID: *msp, Limit: *limit, Repair: *repair})
	if err != nil {
		return err
	}
	return json.NewEncoder(stdout).Encode(report)
}
func reconcileCalendarDatabase(ctx context.Context, databaseURL string, request calendar.ReconcileRequest) (calendar.ReconcileReport, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return calendar.ReconcileReport{}, err
	}
	defer pool.Close()
	repository := psa.NewCalendarRepositoryFromPool(pool)
	custom, err := repository.LoadCustomRoleDefinitions(ctx, request.MSPID)
	if err != nil {
		return calendar.ReconcileReport{}, err
	}
	definitions := make([]calendar.EventRoleDefinition, 0, len(custom))
	for _, definition := range custom {
		definitions = append(definitions, definition)
	}
	roles, err := calendar.NewProductionRoleRegistry(definitions)
	if err != nil {
		return calendar.ReconcileReport{}, err
	}
	registry, err := adapters.NewProductionAdapterRegistry(adapters.Dependencies{Work: repository, Projects: repository, Workforce: repository, Commitments: repository, CustomDates: repository, CustomRoles: custom})
	if err != nil {
		return calendar.ReconcileReport{}, err
	}
	return calendar.NewReconciliationService(repository, registry, calendar.NewProjectionService(repository, roles)).Scan(ctx, request)
}

// This command exports a deterministic fixture manifest. It deliberately has no
// database writer: development harnesses create its records through typed APIs.
func runCalendarDemoSeed(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("calendar-demo-seed", flag.ContinueOnError)
	flags.SetOutput(stderr)
	msp := flags.String("msp-id", "", "fixture MSP UUID")
	clients := flags.String("client-ids", "", "two comma-separated fixture client UUIDs")
	technicians := flags.String("technician-ids", "", "two comma-separated fixture technician UUIDs")
	at := flags.String("at", "", "RFC3339 reference time for reproducible scenarios")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected positional arguments")
	}
	now, err := time.Parse(time.RFC3339, *at)
	if err != nil {
		return errors.New("--at must be an RFC3339 reference time")
	}
	input := calendar.DemoSeedInput{MSPID: *msp, ClientIDs: strings.Split(*clients, ","), TechnicianIDs: strings.Split(*technicians, ","), Now: now}
	for _, value := range append(append([]string{input.MSPID}, input.ClientIDs...), input.TechnicianIDs...) {
		if !id.ValidCanonical(value) {
			return errors.New("fixture IDs must be canonical UUIDs")
		}
	}
	seed, err := calendar.BuildDemoSeed(input)
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(seed)
}
