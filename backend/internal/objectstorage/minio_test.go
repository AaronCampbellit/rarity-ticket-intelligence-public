package objectstorage

import (
	"strings"
	"testing"
)

func TestNewMinIOStoreParsesExplicitScopedConfiguration(t *testing.T) {
	store, err := NewMinIOStore(Config{
		Endpoint: "https://minio:9000", Bucket: "rarity-attachments",
		Region: "us-east-1", AccessKey: "attachment-writer",
		SecretKey: "synthetic-secret",
	})
	if err != nil {
		t.Fatalf("NewMinIOStore() error = %v", err)
	}
	if store.client == nil || store.bucket != "rarity-attachments" {
		t.Fatalf("unexpected store: %+v", store)
	}
}

func TestNewMinIOStoreRejectsMissingCredentialWithoutEchoingSecret(t *testing.T) {
	_, err := NewMinIOStore(Config{
		Endpoint: "https://minio:9000", Bucket: "rarity-attachments",
		Region: "us-east-1", AccessKey: "attachment-writer",
	})
	if err == nil || strings.Contains(err.Error(), "attachment-writer") {
		t.Fatalf("unexpected configuration error: %v", err)
	}
}
