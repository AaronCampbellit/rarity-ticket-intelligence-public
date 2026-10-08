package organizations

import (
	"errors"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/clientidentity"
)

var (
	ErrClientReferenceNotFound  = errors.New("client reference not found")
	ErrClientReferenceAmbiguous = errors.New("client reference ambiguous")
)

// ResolveClientReference returns the one client in clients whose name or
// display ID exactly matches reference after normalization.
func ResolveClientReference(reference string, clients []Client) (Client, error) {
	normalizedReference := clientidentity.Normalize(reference)
	if normalizedReference == "" {
		return Client{}, ErrClientReferenceNotFound
	}

	matches := make(map[string]Client)
	for _, client := range clients {
		if normalizedReference == clientidentity.Normalize(client.Name) ||
			normalizedReference == clientidentity.Normalize(client.DisplayID) {
			matches[client.ID] = client
		}
	}

	switch len(matches) {
	case 0:
		return Client{}, ErrClientReferenceNotFound
	case 1:
		for _, client := range matches {
			return client, nil
		}
	}
	return Client{}, ErrClientReferenceAmbiguous
}
