package automation

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/webhooks"
)

const externalSecretEnvironmentPrefix = "RARITY_AUTOMATION_HTTP_SECRET_"

type ExternalConnection struct {
	ID               string `json:"id"`
	MSPID            string `json:"msp_id"`
	ClientID         string `json:"client_id"`
	Name             string `json:"name"`
	Endpoint         string `json:"endpoint"`
	SigningSecretRef string `json:"-"`
}

func ValidExternalSecretRef(ref string) bool {
	if !strings.HasPrefix(ref, "env://"+externalSecretEnvironmentPrefix) {
		return false
	}
	name := strings.TrimPrefix(ref, "env://")
	suffix := strings.TrimPrefix(name, externalSecretEnvironmentPrefix)
	return suffix != "" &&
		strings.Trim(suffix, "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_") == ""
}

type ExternalConnectionRepository interface {
	Load(
		context.Context,
		string,
		string,
		string,
	) (ExternalConnection, error)
}

type ExternalSecretResolver interface {
	Resolve(context.Context, string) ([]byte, error)
}

type RuntimeConnections struct {
	repository ExternalConnectionRepository
	secrets    ExternalSecretResolver
	sender     webhooks.Sender
	now        func() time.Time
}

var _ ConnectionGate = (*RuntimeConnections)(nil)
var _ ExternalActions = (*RuntimeConnections)(nil)

func NewRuntimeConnections(
	repository ExternalConnectionRepository,
	secrets ExternalSecretResolver,
	sender webhooks.Sender,
	now func() time.Time,
) *RuntimeConnections {
	return &RuntimeConnections{
		repository: repository, secrets: secrets, sender: sender, now: now,
	}
}

func (c *RuntimeConnections) Authorize(
	ctx context.Context,
	ref string,
	mspID string,
	clientID string,
	kind ActionKind,
) error {
	if c == nil || c.repository == nil || kind != ActionCallHTTP ||
		strings.TrimSpace(ref) == "" ||
		strings.TrimSpace(mspID) == "" ||
		strings.TrimSpace(clientID) == "" {
		return ErrActionFailed
	}
	connection, err := c.repository.Load(ctx, ref, mspID, clientID)
	if err != nil {
		return ErrActionFailed
	}
	if !validExternalConnection(connection, ref, mspID, clientID) {
		return ErrActionFailed
	}
	return nil
}

func (c *RuntimeConnections) Call(
	ctx context.Context,
	principal authorization.Principal,
	ref string,
	parameters map[string]string,
	snapshot map[string]string,
) (ActionResult, error) {
	if c == nil || c.repository == nil || c.secrets == nil ||
		c.sender == nil || c.now == nil {
		return ActionResult{}, ErrActionFailed
	}
	connection, err := c.repository.Load(
		ctx, ref, principal.Scope.MSPID, principal.Scope.ClientID,
	)
	if err != nil || !validExternalConnection(
		connection, ref, principal.Scope.MSPID, principal.Scope.ClientID,
	) {
		return ActionResult{}, ErrActionFailed
	}
	runID := strings.TrimSpace(snapshot["_automation_run_id"])
	if runID == "" || webhooks.ValidateDestination(connection.Endpoint) != nil {
		return ActionResult{}, ErrActionFailed
	}
	secret, err := c.secrets.Resolve(ctx, connection.SigningSecretRef)
	if err != nil || len(secret) == 0 {
		return ActionResult{}, ErrActionFailed
	}
	defer clearBytes(secret)
	body, err := json.Marshal(map[string]any{
		"automation_id": principal.ID,
		"client_id":     principal.Scope.ClientID,
		"connection_id": connection.ID,
		"input":         sanitizeSnapshot(snapshot),
		"parameters":    copyStringMap(parameters),
		"run_id":        runID,
	})
	if err != nil {
		return ActionResult{}, ErrActionFailed
	}
	now := c.now().UTC()
	status, err := c.sender.Send(ctx, webhooks.DeliveryRequest{
		URL: connection.Endpoint,
		Headers: map[string]string{
			"Content-Type":               "application/json",
			"X-Rarity-Automation-Run-ID": runID,
			"X-Rarity-Timestamp":         now.Format(time.RFC3339Nano),
			"X-Rarity-Signature":         webhooks.Sign(secret, now, runID, body),
		},
		Body: body,
	})
	if err != nil || status < 200 || status >= 300 {
		return ActionResult{}, ErrActionFailed
	}
	return ActionResult{}, nil
}

func validExternalConnection(
	connection ExternalConnection,
	ref string,
	mspID string,
	clientID string,
) bool {
	return connection.ID == strings.TrimSpace(ref) &&
		connection.MSPID == strings.TrimSpace(mspID) &&
		connection.ClientID == strings.TrimSpace(clientID) &&
		strings.TrimSpace(connection.Endpoint) != "" &&
		strings.TrimSpace(connection.SigningSecretRef) != ""
}

type EnvironmentExternalSecretResolver struct {
	lookup func(string) (string, bool)
}

func NewEnvironmentExternalSecretResolver(
	lookup func(string) (string, bool),
) *EnvironmentExternalSecretResolver {
	return &EnvironmentExternalSecretResolver{lookup: lookup}
}

func (r *EnvironmentExternalSecretResolver) Resolve(
	_ context.Context,
	ref string,
) ([]byte, error) {
	if r == nil || r.lookup == nil || !ValidExternalSecretRef(ref) {
		return nil, ErrActionFailed
	}
	name := strings.TrimPrefix(ref, "env://")
	value, ok := r.lookup(name)
	if !ok || strings.TrimSpace(value) == "" {
		return nil, ErrActionFailed
	}
	return []byte(value), nil
}

func clearBytes(value []byte) {
	for index := range value {
		value[index] = 0
	}
}
