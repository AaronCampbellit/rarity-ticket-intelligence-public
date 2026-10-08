package httpapi

import "time"

type IssueServiceKeyRequest struct {
	Name         string   `json:"name"`
	Capabilities []string `json:"capabilities"`
	DataScopes   []string `json:"data_scopes"`
	TTLSeconds   int64    `json:"ttl_seconds"`
}

type RotateServiceKeyRequest struct {
	TTLSeconds int64  `json:"ttl_seconds"`
	Reason     string `json:"reason"`
}

type RevokeServiceKeyRequest struct {
	Reason string `json:"reason"`
}

type ServiceKeyIssuedResponse struct {
	ID           string    `json:"id"`
	ClientID     string    `json:"client_id,omitempty"`
	Name         string    `json:"name"`
	Prefix       string    `json:"prefix"`
	Token        string    `json:"token"`
	Capabilities []string  `json:"capabilities"`
	DataScopes   []string  `json:"data_scopes"`
	ExpiresAt    time.Time `json:"expires_at"`
}

type ServiceKeyResponse struct {
	ID           string     `json:"id"`
	ClientID     string     `json:"client_id,omitempty"`
	Name         string     `json:"name"`
	Prefix       string     `json:"prefix"`
	Capabilities []string   `json:"capabilities"`
	DataScopes   []string   `json:"data_scopes"`
	CreatedAt    time.Time  `json:"created_at"`
	ExpiresAt    time.Time  `json:"expires_at"`
	RevokedAt    *time.Time `json:"revoked_at,omitempty"`
}
