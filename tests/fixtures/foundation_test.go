package fixtures

import (
	"encoding/json"
	"os"
	"testing"
)

type foundationFixture struct {
	MSP struct {
		ID string `json:"id"`
	} `json:"msp"`
	Clients []struct {
		ID                  string `json:"id"`
		Name                string `json:"name"`
		CollidingExternalID string `json:"collidingExternalID"`
	} `json:"clients"`
	IntegrationCredentials []any `json:"integrationCredentials"`
}

func TestFoundationFixturesAreSyntheticAndCollisionReady(t *testing.T) {
	raw, err := os.ReadFile("foundation.json")
	if err != nil {
		t.Fatal(err)
	}

	var fixture foundationFixture
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}

	if fixture.MSP.ID != "msp-demo" {
		t.Fatalf("expected synthetic demo MSP, got %q", fixture.MSP.ID)
	}
	if len(fixture.Clients) != 2 {
		t.Fatalf("expected exactly two fixture clients, got %d", len(fixture.Clients))
	}
	if fixture.Clients[0].Name != "Alpha" || fixture.Clients[1].Name != "Bravo" {
		t.Fatalf("expected Alpha and Bravo fixtures, got %q and %q", fixture.Clients[0].Name, fixture.Clients[1].Name)
	}
	if fixture.Clients[0].CollidingExternalID != fixture.Clients[1].CollidingExternalID {
		t.Fatal("fixture clients must share a collision value")
	}
	if fixture.Clients[0].ID == fixture.Clients[1].ID {
		t.Fatal("fixture clients must have distinct durable IDs")
	}
	if len(fixture.IntegrationCredentials) != 0 {
		t.Fatal("fixtures must not contain integration credentials")
	}
}
