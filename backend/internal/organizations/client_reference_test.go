package organizations

import (
	"errors"
	"testing"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
)

func TestResolveClientReference(t *testing.T) {
	clients := []Client{
		clientReferenceClient("client-alpha", "Alpha Managed Services", "ALPHA-100"),
		clientReferenceClient("client-bravo", "Bravo Systems", "BRAVO-200"),
	}

	tests := []struct {
		name      string
		reference string
		clients   []Client
		wantID    string
		wantErr   error
	}{
		{
			name:      "matches exact name",
			reference: "Alpha Managed Services",
			clients:   clients,
			wantID:    "client-alpha",
		},
		{
			name:      "matches exact display ID",
			reference: "BRAVO-200",
			clients:   clients,
			wantID:    "client-bravo",
		},
		{
			name:      "normalizes case and whitespace",
			reference: "  alpha   managed\tservices  ",
			clients: []Client{
				clientReferenceClient("client-alpha", " Alpha  Managed Services ", "ALPHA-100"),
			},
			wantID: "client-alpha",
		},
		{
			name:      "deduplicates a client matching both fields",
			reference: "northwind",
			clients: []Client{
				clientReferenceClient("client-northwind", "Northwind", "NORTHWIND"),
			},
			wantID: "client-northwind",
		},
		{
			name:      "returns not found for an absent reference",
			reference: "Contoso",
			clients:   clients,
			wantErr:   ErrClientReferenceNotFound,
		},
		{
			name:      "returns ambiguous for clients sharing a normalized name",
			reference: "  shared   client ",
			clients: []Client{
				clientReferenceClient("client-one", "Shared Client", "ONE-100"),
				clientReferenceClient("client-two", " shared\tclient ", "TWO-200"),
			},
			wantErr: ErrClientReferenceAmbiguous,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := ResolveClientReference(tt.reference, tt.clients)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("ResolveClientReference() error = %v, want %v", err, tt.wantErr)
			}
			if client.ID != tt.wantID {
				t.Fatalf("ResolveClientReference() client ID = %q, want %q", client.ID, tt.wantID)
			}
		})
	}
}

func clientReferenceClient(id, name, displayID string) Client {
	return Client{
		Envelope: object.Envelope{ID: id, DisplayID: displayID},
		Name:     name,
	}
}
