package psa

import (
	"context"
	"strings"
	"testing"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/views"
)

func TestViewRepositorySavesKindSpecificRecord(t *testing.T) {
	for _, test := range []struct {
		kind  views.Kind
		table string
	}{
		{kind: views.SavedSearch, table: "INSERT INTO saved_searches"},
		{kind: views.Dashboard, table: "INSERT INTO dashboards"},
		{kind: views.KindCalendarLens, table: "INSERT INTO saved_searches"},
	} {
		tx := &fakeSalesTx{}
		err := NewViewRepository(&fakeSalesDB{tx: tx}).Save(context.Background(), views.View{
			ID: "view", MSPID: "msp", OwnerID: "owner", Kind: test.kind,
			Name: "My view", Query: map[string]any{"status": "open"},
			Audience: views.Audience{Type: views.Private}, Version: 1,
		})
		if err != nil {
			t.Fatalf("Save(%s) error = %v", test.kind, err)
		}
		if len(tx.queries) != 1 || !strings.Contains(tx.queries[0], test.table) || !tx.committed {
			t.Fatalf("Save(%s) did not commit expected table: %+v", test.kind, tx)
		}
	}
}

func TestCalendarLensRepositoryPersistsInternalKindMarker(t *testing.T) {
	tx := &fakeSalesTx{}
	err := NewViewRepository(&fakeSalesDB{tx: tx}).Save(context.Background(), views.View{ID: "view", MSPID: "msp", OwnerID: "owner", Kind: views.KindCalendarLens, Name: "Calendar", Query: map[string]any{"lens": "week"}, Audience: views.Audience{Type: views.Private}, Version: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(tx.args) != 1 || !strings.Contains(string(tx.args[0][4].([]byte)), `"__view_kind":"calendar_lens"`) {
		t.Fatalf("args=%#v", tx.args)
	}
}
