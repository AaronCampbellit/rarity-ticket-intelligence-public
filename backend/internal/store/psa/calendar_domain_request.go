package psa

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"

	"github.com/jackc/pgx/v5"
	internalid "github.com/rarity-ticket-intelligence/rarity/backend/internal/id"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mutation"
)

func marshalOptionalJSON(value any) (any, error) {
	if value == nil || (reflect.ValueOf(value).Kind() == reflect.Ptr && reflect.ValueOf(value).IsNil()) {
		return nil, nil
	}
	return json.Marshal(value)
}

func claimCalendarDomainRequest(ctx context.Context, tx transaction, id, mspID, clientID, operation, key, fingerprint string, response any, createdAt any) error {
	body, err := json.Marshal(response)
	if err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `INSERT INTO calendar_domain_requests(id,msp_id,client_id,operation,idempotency_key,request_fingerprint,response,created_at) VALUES($1::uuid,$2::uuid,NULLIF($3,'')::uuid,$4,$5,$6,$7::jsonb,$8) ON CONFLICT (msp_id,client_id,operation,idempotency_key) DO NOTHING`, id, mspID, clientID, operation, key, fingerprint, body, createdAt)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 1 {
		return nil
	}
	var existingFingerprint string
	var existing []byte
	err = tx.QueryRow(ctx, `SELECT request_fingerprint,response FROM calendar_domain_requests WHERE msp_id=$1::uuid AND client_id IS NOT DISTINCT FROM NULLIF($2,'')::uuid AND operation=$3 AND idempotency_key=$4 FOR UPDATE`, mspID, clientID, operation, key).Scan(&existingFingerprint, &existing)
	if errors.Is(err, pgx.ErrNoRows) {
		return mutation.ErrIdempotencyConflict
	}
	if err != nil {
		return err
	}
	if existingFingerprint != fingerprint {
		return mutation.ErrIdempotencyConflict
	}
	return &mutation.ReplayError{Response: existing}
}

func validCalendarFactUUIDs(requestID string, audit mutation.AuditRecord, event mutation.EventRecord) bool {
	return internalid.ValidCanonical(requestID) && internalid.ValidCanonical(audit.ID) && internalid.ValidCanonical(audit.MSPID) && (audit.ClientID == "" || internalid.ValidCanonical(audit.ClientID)) && internalid.ValidCanonical(audit.ActorID) && internalid.ValidCanonical(audit.SubjectID) && internalid.ValidCanonical(audit.CorrelationID) &&
		internalid.ValidCanonical(event.EventID) && internalid.ValidCanonical(event.MSPID) && (event.ClientID == "" || internalid.ValidCanonical(event.ClientID)) && internalid.ValidCanonical(event.ActorID) && internalid.ValidCanonical(event.SubjectID) && internalid.ValidCanonical(event.CorrelationID) && (event.CausationID == "" || internalid.ValidCanonical(event.CausationID))
}
