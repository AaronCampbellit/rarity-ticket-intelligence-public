package psa

import (
	"context"
	"strings"
	"testing"
)

func TestIntegrationHealthRepositoryListsOnlyRequestedMSP(t *testing.T) {
	db := &fakeSalesDB{queryRows: &fakeRows{}}
	signals, err := NewIntegrationHealthRepository(db).List(
		context.Background(), "msp",
	)
	if err != nil || len(signals) != 0 {
		t.Fatalf("List() signals=%+v error=%v", signals, err)
	}
	if !strings.Contains(db.query, "FROM integration_health_signals") ||
		!strings.Contains(db.query, "WHERE msp_id = $1") ||
		!strings.Contains(db.query, "ORDER BY integration_kind, connection_id") {
		t.Fatalf("integration health query is not MSP scoped: %s", db.query)
	}
}
