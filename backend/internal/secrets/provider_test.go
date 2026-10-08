package secrets

import (
	"bytes"
	"context"
	"errors"
	"testing"
)

func TestLocalProviderEncryptsAndBindsCiphertextToPurpose(t *testing.T) {
	key := bytes.Repeat([]byte{0x2a}, 32)
	provider, err := NewLocalProvider(key, func(size int) ([]byte, error) {
		return bytes.Repeat([]byte{0x11}, size), nil
	})
	if err != nil {
		t.Fatalf("NewLocalProvider() error = %v", err)
	}

	sealed, err := provider.Seal(context.Background(), "integration.graph", []byte("client-secret"))
	if err != nil {
		t.Fatalf("Seal() error = %v", err)
	}
	if bytes.Contains(sealed.Ciphertext, []byte("client-secret")) {
		t.Fatal("sealed value contains plaintext")
	}
	opened, err := provider.Open(context.Background(), "integration.graph", sealed)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if string(opened) != "client-secret" {
		t.Fatalf("Open() = %q", opened)
	}
	if _, err := provider.Open(context.Background(), "integration.datto", sealed); !errors.Is(err, ErrOpen) {
		t.Fatalf("cross-purpose Open() error = %v, want ErrOpen", err)
	}
}

func TestLocalProviderRejectsInvalidKeySizes(t *testing.T) {
	if _, err := NewLocalProvider([]byte("short"), nil); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("NewLocalProvider() error = %v, want ErrInvalidKey", err)
	}
}
