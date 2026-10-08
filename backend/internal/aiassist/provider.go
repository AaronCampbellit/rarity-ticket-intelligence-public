package aiassist

import (
	"errors"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	minimumProviderTimeout = time.Second
	maximumProviderTimeout = time.Hour
	defaultRemoteTimeout   = 5 * time.Minute
	defaultLocalTimeout    = 15 * time.Minute

	minimumProviderPayloadBytes int64 = 1 << 10
	defaultRequestLimitBytes    int64 = 1 << 20
	maximumRequestLimitBytes    int64 = 5 << 20
	defaultResponseLimitBytes   int64 = 5 << 20
	maximumResponseLimitBytes   int64 = 10 << 20
)

var (
	ErrInvalidProviderConfiguration = errors.New("invalid AI provider configuration")
	ErrLocalAcknowledgementRequired = errors.New("local AI provider acknowledgement required")
)

// AdapterType is deliberately closed: administrator-defined HTTP templates are
// not a supported provider surface.
type AdapterType string

const (
	AdapterOllama           AdapterType = "ollama"
	AdapterOpenAICompatible AdapterType = "openai_compatible"
)

type NetworkMode string

const (
	NetworkLocal  NetworkMode = "local"
	NetworkRemote NetworkMode = "remote"
)

type HealthState string

const (
	HealthPending  HealthState = "pending"
	HealthHealthy  HealthState = "healthy"
	HealthDegraded HealthState = "degraded"
	HealthFailed   HealthState = "failed"
	HealthDisabled HealthState = "disabled"
)

// ProviderConnection is safe to return to a browser. Credential ciphertext,
// nonces, and plaintext are intentionally not domain fields.
type ProviderConnection struct {
	ID      string      `json:"id"`
	MSPID   string      `json:"-"`
	Name    string      `json:"name"`
	BaseURL string      `json:"base_url"`
	Adapter AdapterType `json:"adapter"`
	Network NetworkMode `json:"network"`

	CredentialConfigured bool          `json:"credential_configured"`
	Enabled              bool          `json:"enabled"`
	Timeout              time.Duration `json:"timeout"`
	RequestLimitBytes    int64         `json:"request_limit_bytes"`
	ResponseLimitBytes   int64         `json:"response_limit_bytes"`

	DisclosureAcceptedAt       *time.Time  `json:"disclosure_accepted_at,omitempty"`
	LocalNetworkAcknowledgedAt *time.Time  `json:"local_network_acknowledged_at,omitempty"`
	Health                     HealthState `json:"health"`
	LastTestedAt               *time.Time  `json:"last_tested_at,omitempty"`
	LastSucceededAt            *time.Time  `json:"last_succeeded_at,omitempty"`
	LastErrorCode              string      `json:"last_error_code,omitempty"`
	Version                    int64       `json:"version"`
}

type ModelProfile struct {
	ID                string    `json:"id"`
	MSPID             string    `json:"-"`
	ConnectionID      string    `json:"connection_id"`
	ProviderModelID   string    `json:"provider_model_id"`
	DisplayName       string    `json:"display_name"`
	SupportedFeatures []Feature `json:"supported_features"`
	ContextLimit      int64     `json:"context_limit"`
	OutputLimit       int64     `json:"output_limit"`
	ZeroCost          bool      `json:"zero_cost"`
	// Nil prices deliberately mean that paid use cannot be reserved safely.
	// Values are minor currency units per one million provider units.
	InputCostPerMillionMinor  *int64 `json:"input_cost_per_million_minor,omitempty"`
	OutputCostPerMillionMinor *int64 `json:"output_cost_per_million_minor,omitempty"`
	Enabled                   bool   `json:"enabled"`
	Version                   int64  `json:"version"`
}

type DiscoveredModel struct {
	ProviderModelID string `json:"provider_model_id"`
	DisplayName     string `json:"display_name"`
	ContextLimit    int64  `json:"context_limit"`
}

// ConnectionWithDefaults applies only the documented operational defaults;
// callers still validate the result before accepting a mutation.
func ConnectionWithDefaults(connection ProviderConnection) ProviderConnection {
	if connection.Timeout == 0 {
		if connection.Network == NetworkLocal {
			connection.Timeout = defaultLocalTimeout
		} else {
			connection.Timeout = defaultRemoteTimeout
		}
	}
	if connection.RequestLimitBytes == 0 {
		connection.RequestLimitBytes = defaultRequestLimitBytes
	}
	if connection.ResponseLimitBytes == 0 {
		connection.ResponseLimitBytes = defaultResponseLimitBytes
	}
	if connection.Health == "" {
		connection.Health = HealthPending
	}
	return connection
}

func ValidateConnection(connection ProviderConnection) error {
	if strings.TrimSpace(connection.ID) == "" ||
		strings.TrimSpace(connection.MSPID) == "" ||
		strings.TrimSpace(connection.Name) == "" ||
		!validAdapter(connection.Adapter) || !validNetwork(connection.Network) ||
		connection.Timeout < minimumProviderTimeout || connection.Timeout > maximumProviderTimeout ||
		connection.RequestLimitBytes < minimumProviderPayloadBytes || connection.RequestLimitBytes > maximumRequestLimitBytes ||
		connection.ResponseLimitBytes < minimumProviderPayloadBytes || connection.ResponseLimitBytes > maximumResponseLimitBytes ||
		connection.Version < 1 || !validHealth(connection.Health) {
		return ErrInvalidProviderConfiguration
	}
	endpoint, err := url.Parse(strings.TrimSpace(connection.BaseURL))
	if err != nil || endpoint.Scheme == "" || endpoint.Host == "" ||
		endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" ||
		(endpoint.Path != "" && endpoint.Path != "/") || !validURLPort(endpoint.Host) {
		return ErrInvalidProviderConfiguration
	}
	switch connection.Network {
	case NetworkRemote:
		if endpoint.Scheme != "https" || !validPublicHostname(endpoint.Hostname()) {
			return ErrInvalidProviderConfiguration
		}
	case NetworkLocal:
		if endpoint.Scheme != "http" && endpoint.Scheme != "https" {
			return ErrInvalidProviderConfiguration
		}
		if connection.LocalNetworkAcknowledgedAt == nil {
			return ErrLocalAcknowledgementRequired
		}
	}
	return nil
}

func validURLPort(host string) bool {
	if strings.HasPrefix(host, "[") {
		closingBracket := strings.LastIndex(host, "]")
		if closingBracket < 1 {
			return false
		}
		suffix := host[closingBracket+1:]
		if suffix == "" {
			return true
		}
		return strings.HasPrefix(suffix, ":") && validPortNumber(suffix[1:])
	}
	if strings.Count(host, ":") == 0 {
		return true
	}
	if strings.Count(host, ":") != 1 {
		return false
	}
	return validPortNumber(host[strings.LastIndex(host, ":")+1:])
}

func validPortNumber(port string) bool {
	value, err := strconv.Atoi(port)
	return err == nil && value >= 1 && value <= 65535
}

func ValidateModelProfile(model ModelProfile) error {
	if strings.TrimSpace(model.ID) == "" || strings.TrimSpace(model.MSPID) == "" ||
		strings.TrimSpace(model.ConnectionID) == "" || strings.TrimSpace(model.ProviderModelID) == "" ||
		strings.TrimSpace(model.DisplayName) == "" || model.ContextLimit < 1 ||
		model.OutputLimit < 1 || model.OutputLimit > model.ContextLimit || model.Version < 1 ||
		!validFeatures(model.SupportedFeatures) ||
		(model.InputCostPerMillionMinor != nil && *model.InputCostPerMillionMinor < 0) ||
		(model.OutputCostPerMillionMinor != nil && *model.OutputCostPerMillionMinor < 0) ||
		(model.ZeroCost && ((model.InputCostPerMillionMinor != nil && *model.InputCostPerMillionMinor != 0) || (model.OutputCostPerMillionMinor != nil && *model.OutputCostPerMillionMinor != 0))) {
		return ErrInvalidProviderConfiguration
	}
	return nil
}

func validAdapter(adapter AdapterType) bool {
	return adapter == AdapterOllama || adapter == AdapterOpenAICompatible
}

func validNetwork(network NetworkMode) bool {
	return network == NetworkLocal || network == NetworkRemote
}

func validHealth(health HealthState) bool {
	switch health {
	case HealthPending, HealthHealthy, HealthDegraded, HealthFailed, HealthDisabled:
		return true
	default:
		return false
	}
}

func validPublicHostname(hostname string) bool {
	hostname = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(hostname)), ".")
	if hostname == "" || net.ParseIP(hostname) != nil || hostname == "localhost" ||
		strings.HasSuffix(hostname, ".localhost") || strings.HasSuffix(hostname, ".local") ||
		!strings.Contains(hostname, ".") {
		return false
	}
	for _, label := range strings.Split(hostname, ".") {
		if label == "" || len(label) > 63 || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return false
		}
		for _, character := range label {
			if !(character >= 'a' && character <= 'z') &&
				!(character >= '0' && character <= '9') && character != '-' {
				return false
			}
		}
	}
	return true
}

func validFeatures(features []Feature) bool {
	seen := make(map[Feature]struct{}, len(features))
	for _, feature := range features {
		if feature != FeatureSummary && feature != FeatureReplyDraft && feature != FeatureSimilar && feature != FeatureCalendarRecommendation {
			return false
		}
		if _, duplicate := seen[feature]; duplicate {
			return false
		}
		seen[feature] = struct{}{}
	}
	return true
}
