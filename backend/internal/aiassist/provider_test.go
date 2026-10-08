package aiassist

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func validProviderConnection() ProviderConnection {
	return ProviderConnection{
		ID: "connection-id", MSPID: "msp-id", Name: "Remote models",
		Adapter: AdapterOpenAICompatible, Network: NetworkRemote,
		BaseURL: "https://models.example.test", Timeout: 5 * time.Minute,
		RequestLimitBytes: 1 << 20, ResponseLimitBytes: 5 << 20,
		Health: HealthPending, Version: 1,
	}
}

func TestValidateConnectionSeparatesRemoteAndAcknowledgedLocalModes(t *testing.T) {
	remote := validProviderConnection()
	if err := ValidateConnection(remote); err != nil {
		t.Fatalf("remote connection rejected: %v", err)
	}

	local := remote
	local.Adapter, local.Network, local.BaseURL = AdapterOllama, NetworkLocal, "http://127.0.0.1:11434"
	if err := ValidateConnection(local); !errors.Is(err, ErrLocalAcknowledgementRequired) {
		t.Fatalf("unacknowledged local error=%v", err)
	}
	now := time.Date(2026, time.July, 29, 12, 0, 0, 0, time.UTC)
	local.LocalNetworkAcknowledgedAt = &now
	if err := ValidateConnection(local); err != nil {
		t.Fatalf("acknowledged local connection rejected: %v", err)
	}
}

func TestValidateConnectionEnforcesAdapterURLAndExactLimits(t *testing.T) {
	base := validProviderConnection()
	cases := []struct {
		name   string
		change func(*ProviderConnection)
	}{
		{"unknown adapter", func(c *ProviderConnection) { c.Adapter = "custom_http" }},
		{"remote http", func(c *ProviderConnection) { c.BaseURL = "http://models.example.test" }},
		{"remote loopback", func(c *ProviderConnection) { c.BaseURL = "https://127.0.0.1" }},
		{"remote invalid hostname", func(c *ProviderConnection) { c.BaseURL = "https://model_name.example.test" }},
		{"remote port zero", func(c *ProviderConnection) { c.BaseURL = "https://models.example.test:0" }},
		{"remote port too high", func(c *ProviderConnection) { c.BaseURL = "https://models.example.test:65536" }},
		{"remote malformed port", func(c *ProviderConnection) { c.BaseURL = "https://models.example.test:abc" }},
		{"timeout below one second", func(c *ProviderConnection) { c.Timeout = time.Second - time.Nanosecond }},
		{"timeout over one hour", func(c *ProviderConnection) { c.Timeout = time.Hour + time.Nanosecond }},
		{"request below one kib", func(c *ProviderConnection) { c.RequestLimitBytes = 1023 }},
		{"request over five mib", func(c *ProviderConnection) { c.RequestLimitBytes = 5<<20 + 1 }},
		{"response below one kib", func(c *ProviderConnection) { c.ResponseLimitBytes = 1023 }},
		{"response over ten mib", func(c *ProviderConnection) { c.ResponseLimitBytes = 10<<20 + 1 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			connection := base
			tc.change(&connection)
			if err := ValidateConnection(connection); !errors.Is(err, ErrInvalidProviderConfiguration) {
				t.Fatalf("ValidateConnection() error=%v, want invalid configuration", err)
			}
		})
	}
}

func TestValidateConnectionRejectsInvalidLocalPorts(t *testing.T) {
	now := time.Now().UTC()
	for _, baseURL := range []string{"http://127.0.0.1:0", "http://127.0.0.1:65536", "http://127.0.0.1:abc"} {
		connection := validProviderConnection()
		connection.Adapter, connection.Network, connection.BaseURL = AdapterOllama, NetworkLocal, baseURL
		connection.LocalNetworkAcknowledgedAt = &now
		if err := ValidateConnection(connection); !errors.Is(err, ErrInvalidProviderConfiguration) {
			t.Fatalf("local URL %q error=%v, want invalid configuration", baseURL, err)
		}
	}
}

func TestConnectionDefaultsUseNetworkSafeTimeoutsAndBoundedPayloads(t *testing.T) {
	remote := validProviderConnection()
	remote.Timeout, remote.RequestLimitBytes, remote.ResponseLimitBytes = 0, 0, 0
	remote = ConnectionWithDefaults(remote)
	if remote.Timeout != 5*time.Minute || remote.RequestLimitBytes != 1<<20 || remote.ResponseLimitBytes != 5<<20 {
		t.Fatalf("remote defaults=%+v", remote)
	}
	local := remote
	local.Network = NetworkLocal
	local.Timeout = 0
	local.BaseURL = "http://localhost:11434"
	now := time.Now().UTC()
	local.LocalNetworkAcknowledgedAt = &now
	local = ConnectionWithDefaults(local)
	if local.Timeout != 15*time.Minute {
		t.Fatalf("local timeout default=%s, want 15m", local.Timeout)
	}
}

func TestProviderConnectionJSONIsCredentialFree(t *testing.T) {
	connection := validProviderConnection()
	connection.CredentialConfigured = true
	body, err := json.Marshal(connection)
	if err != nil {
		t.Fatalf("marshal connection: %v", err)
	}
	serialized := string(body)
	if !strings.Contains(serialized, `"credential_configured":true`) ||
		strings.Contains(serialized, "credential_ciphertext") ||
		strings.Contains(serialized, "credential_nonce") ||
		strings.Contains(serialized, "credential_version") ||
		strings.Contains(serialized, "api-key") {
		t.Fatalf("serialized connection leaked credential material: %s", serialized)
	}
}

func TestValidateModelProfileRequiresKnownFeaturesAndBounds(t *testing.T) {
	model := ModelProfile{
		ID: "model-id", MSPID: "msp-id", ConnectionID: "connection-id",
		ProviderModelID: "llama3.2", DisplayName: "Llama 3.2",
		SupportedFeatures: []Feature{FeatureSummary}, ContextLimit: 128_000,
		OutputLimit: 8_000, Version: 1,
	}
	if err := ValidateModelProfile(model); err != nil {
		t.Fatalf("valid model rejected: %v", err)
	}
	model.SupportedFeatures = []Feature{"execute"}
	if err := ValidateModelProfile(model); !errors.Is(err, ErrInvalidProviderConfiguration) {
		t.Fatalf("unknown feature error=%v", err)
	}
}
