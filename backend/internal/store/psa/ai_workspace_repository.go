package psa

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/aiassist"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

type AIWorkspaceRepository struct {
	db database
}

var _ aiassist.ConversationStore = (*AIWorkspaceRepository)(nil)
var _ aiassist.ProductKnowledgeStore = (*AIWorkspaceRepository)(nil)
var _ aiassist.ProposalStore = (*AIWorkspaceRepository)(nil)

func NewAIWorkspaceRepository(db database) *AIWorkspaceRepository {
	return &AIWorkspaceRepository{db: db}
}

func (r *AIWorkspaceRepository) CreateConversation(ctx context.Context, conversation aiassist.Conversation) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO ai_conversations (
  id, msp_id, client_id, principal_id, title, version, created_at, updated_at
) VALUES ($1, $2, NULLIF($3, '')::uuid, $4, $5, $6, $7, $8)
`, conversation.ID, conversation.MSPID, conversation.ClientID, conversation.PrincipalID,
		conversation.Title, conversation.Version, conversation.CreatedAt, conversation.UpdatedAt); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	return nil
}

func (r *AIWorkspaceRepository) GetConversation(
	ctx context.Context,
	target scope.Target,
	id string,
) (aiassist.Conversation, error) {
	var conversation aiassist.Conversation
	err := r.db.QueryRow(ctx, `
SELECT conversation.id::text, conversation.msp_id::text,
       COALESCE(conversation.client_id::text, ''), conversation.principal_id::text,
       conversation.title, conversation.archived_at, conversation.version,
       conversation.created_at, conversation.updated_at
FROM ai_conversations conversation
WHERE conversation.id = $1
  AND conversation.msp_id = $2
  AND (
    conversation.client_id = NULLIF($3, '')::uuid
    OR (NULLIF($3, '')::uuid IS NULL AND conversation.client_id IS NULL)
  )
`, id, target.MSPID, target.ClientID).Scan(
		&conversation.ID, &conversation.MSPID, &conversation.ClientID,
		&conversation.PrincipalID, &conversation.Title, &conversation.ArchivedAt,
		&conversation.Version, &conversation.CreatedAt, &conversation.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return aiassist.Conversation{}, scope.ErrNotFound
	}
	return conversation, err
}

func (r *AIWorkspaceRepository) ListConversations(
	ctx context.Context,
	target scope.Target,
	principalID string,
	includeArchived bool,
	limit int,
) ([]aiassist.Conversation, error) {
	result, err := r.db.Query(ctx, `
SELECT conversation.id::text, conversation.msp_id::text,
       COALESCE(conversation.client_id::text, ''), conversation.principal_id::text,
       conversation.title, conversation.archived_at, conversation.version,
       conversation.created_at, conversation.updated_at
FROM ai_conversations conversation
WHERE conversation.msp_id = $1
  AND (
    conversation.client_id = NULLIF($2, '')::uuid
    OR (NULLIF($2, '')::uuid IS NULL AND conversation.client_id IS NULL)
  )
  AND conversation.principal_id = $3
  AND ($4 OR conversation.archived_at IS NULL)
ORDER BY conversation.updated_at DESC, conversation.id
LIMIT $5
`, target.MSPID, target.ClientID, principalID, includeArchived, limit)
	if err != nil {
		return nil, err
	}
	defer result.Close()
	conversations := make([]aiassist.Conversation, 0)
	for result.Next() {
		var conversation aiassist.Conversation
		if err := result.Scan(
			&conversation.ID, &conversation.MSPID, &conversation.ClientID,
			&conversation.PrincipalID, &conversation.Title, &conversation.ArchivedAt,
			&conversation.Version, &conversation.CreatedAt, &conversation.UpdatedAt,
		); err != nil {
			return nil, err
		}
		conversations = append(conversations, conversation)
	}
	return conversations, result.Err()
}

func (r *AIWorkspaceRepository) ArchiveConversation(
	ctx context.Context,
	target scope.Target,
	id string,
	principalID string,
	expectedVersion int64,
	archivedAt time.Time,
) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `
UPDATE ai_conversations
SET archived_at = $6, updated_at = $6, version = version + 1
WHERE id = $1 AND msp_id = $2
  AND (
    client_id = NULLIF($3, '')::uuid
    OR (NULLIF($3, '')::uuid IS NULL AND client_id IS NULL)
  )
  AND principal_id = $4 AND version = $5 AND archived_at IS NULL
`, id, target.MSPID, target.ClientID, principalID, expectedVersion, archivedAt)
	if err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if tag.RowsAffected() != 1 {
		_ = tx.Rollback(ctx)
		return scope.ErrNotFound
	}
	if err := tx.Commit(ctx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	return nil
}

func (r *AIWorkspaceRepository) AppendMessage(ctx context.Context, message aiassist.Message) error {
	if message.ReferencedObjects == nil {
		message.ReferencedObjects = map[string]any{}
	}
	references, err := json.Marshal(message.ReferencedObjects)
	if err != nil {
		return err
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO ai_messages (
  id, msp_id, client_id, conversation_id, role, safe_text,
  referenced_objects, provider_job_id, created_at
) VALUES (
  $1, $2, NULLIF($3, '')::uuid, $4, $5, $6,
  $7::jsonb, NULLIF($8, '')::uuid, $9
)
`, message.ID, message.MSPID, message.ClientID, message.ConversationID,
		message.Role, message.SafeText, references, message.ProviderJobID, message.CreatedAt); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	return nil
}

func (r *AIWorkspaceRepository) ListMessages(
	ctx context.Context,
	target scope.Target,
	conversationID string,
	limit int,
) ([]aiassist.Message, error) {
	result, err := r.db.Query(ctx, `
SELECT message.id::text, message.msp_id::text, COALESCE(message.client_id::text, ''),
       message.conversation_id::text, message.role, message.safe_text,
       message.referenced_objects, COALESCE(message.provider_job_id::text, ''),
       message.created_at
FROM ai_messages message
WHERE message.msp_id = $1
  AND (
    message.client_id = NULLIF($2, '')::uuid
    OR (NULLIF($2, '')::uuid IS NULL AND message.client_id IS NULL)
  )
  AND message.conversation_id = $3
ORDER BY message.created_at, message.id
LIMIT $4
`, target.MSPID, target.ClientID, conversationID, limit)
	if err != nil {
		return nil, err
	}
	defer result.Close()
	messages := make([]aiassist.Message, 0)
	for result.Next() {
		var message aiassist.Message
		var references []byte
		if err := result.Scan(
			&message.ID, &message.MSPID, &message.ClientID, &message.ConversationID,
			&message.Role, &message.SafeText, &references, &message.ProviderJobID,
			&message.CreatedAt,
		); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(references, &message.ReferencedObjects); err != nil {
			return nil, err
		}
		messages = append(messages, message)
	}
	return messages, result.Err()
}

func (r *AIWorkspaceRepository) SearchProductKnowledge(
	ctx context.Context,
	query string,
	audience aiassist.ProductAudience,
	limit int,
) ([]aiassist.Citation, error) {
	result, err := r.db.Query(ctx, `
WITH search AS (SELECT websearch_to_tsquery('english', $1) AS query)
SELECT document.source_key, document.source_version, document.section,
       left(document.body, 480)
FROM ai_product_documents document
CROSS JOIN search
WHERE document.search_vector @@ search.query
  AND (document.audience = 'all_users' OR document.audience = $2)
ORDER BY ts_rank_cd(document.search_vector, search.query) DESC,
         document.source_key, document.section
LIMIT $3
`, query, audience, limit)
	if err != nil {
		return nil, err
	}
	defer result.Close()
	citations := make([]aiassist.Citation, 0)
	for result.Next() {
		var citation aiassist.Citation
		if err := result.Scan(
			&citation.SourceKey, &citation.SourceVersion, &citation.Section, &citation.Excerpt,
		); err != nil {
			return nil, err
		}
		citations = append(citations, citation)
	}
	return citations, result.Err()
}

func (r *AIWorkspaceRepository) CreateProposal(
	ctx context.Context,
	proposal aiassist.ActionProposal,
) error {
	input, err := json.Marshal(proposal.NormalizedInput)
	if err != nil {
		return err
	}
	preview, err := json.Marshal(proposal.Preview)
	if err != nil {
		return err
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO ai_action_proposals (
  id, msp_id, client_id, target_client_id, conversation_id, message_id, principal_id,
  tool_name, tool_version, normalized_input, preview,
  target_type, target_id, target_version, required_capability,
  expires_at, state, correlation_id, version, created_at, updated_at
) VALUES (
  $1, $2, NULLIF($3, '')::uuid, NULLIF($4, '')::uuid, $5, NULLIF($6, '')::uuid, $7,
  $8, $9, $10::jsonb, $11::jsonb,
  $12, $13, NULLIF($14, 0), $15,
  $16, 'pending', $17, $18, $19, $20
)
`, proposal.ID, proposal.MSPID, proposal.ClientID, proposal.TargetClientID,
		proposal.ConversationID, proposal.MessageID, proposal.PrincipalID,
		proposal.ToolName, proposal.ToolVersion, input, preview,
		proposal.Preview.TargetType, proposal.Preview.TargetID,
		proposal.Preview.TargetVersion, proposal.RequiredCapability,
		proposal.ExpiresAt, proposal.CorrelationID, proposal.Version,
		proposal.CreatedAt, proposal.UpdatedAt); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	return nil
}

func (r *AIWorkspaceRepository) GetProposal(
	ctx context.Context,
	target scope.Target,
	id string,
	principalID string,
) (aiassist.ActionProposal, error) {
	var (
		proposal      aiassist.ActionProposal
		input         []byte
		preview       []byte
		result        []byte
		targetType    string
		targetID      string
		targetVersion int64
	)
	err := r.db.QueryRow(ctx, `
SELECT proposal.id::text, proposal.msp_id::text, COALESCE(proposal.client_id::text, ''),
       COALESCE(proposal.target_client_id::text, ''), proposal.conversation_id::text,
       COALESCE(proposal.message_id::text, ''),
       proposal.principal_id::text, proposal.tool_name, proposal.tool_version,
       proposal.normalized_input, proposal.preview, proposal.target_type,
       proposal.target_id::text, COALESCE(proposal.target_version, 0),
       proposal.required_capability, proposal.expires_at, proposal.state,
       proposal.confirmed_at, proposal.rejected_at, proposal.failed_at,
       proposal.result, COALESCE(proposal.error_code, ''),
       proposal.correlation_id::text, proposal.version,
       proposal.created_at, proposal.updated_at
FROM ai_action_proposals proposal
WHERE proposal.id = $1 AND proposal.msp_id = $2
  AND (
    proposal.client_id = NULLIF($3, '')::uuid
    OR (NULLIF($3, '')::uuid IS NULL AND proposal.client_id IS NULL)
  )
  AND proposal.principal_id = $4
`, id, target.MSPID, target.ClientID, principalID).Scan(
		&proposal.ID, &proposal.MSPID, &proposal.ClientID, &proposal.TargetClientID,
		&proposal.ConversationID, &proposal.MessageID, &proposal.PrincipalID,
		&proposal.ToolName, &proposal.ToolVersion, &input, &preview, &targetType,
		&targetID, &targetVersion, &proposal.RequiredCapability,
		&proposal.ExpiresAt, &proposal.State, &proposal.ConfirmedAt,
		&proposal.RejectedAt, &proposal.FailedAt, &result, &proposal.ErrorCode,
		&proposal.CorrelationID, &proposal.Version, &proposal.CreatedAt,
		&proposal.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return aiassist.ActionProposal{}, scope.ErrNotFound
	}
	if err != nil {
		return aiassist.ActionProposal{}, err
	}
	proposal.NormalizedInput = append(json.RawMessage(nil), input...)
	if err := json.Unmarshal(preview, &proposal.Preview); err != nil {
		return aiassist.ActionProposal{}, err
	}
	if proposal.Preview.TargetType != targetType || proposal.Preview.TargetID != targetID ||
		proposal.Preview.TargetVersion != targetVersion {
		return aiassist.ActionProposal{}, aiassist.ErrProposalConflict
	}
	if len(result) > 0 {
		var toolResult aiassist.ToolResult
		if err := json.Unmarshal(result, &toolResult); err != nil {
			return aiassist.ActionProposal{}, err
		}
		proposal.Result = &toolResult
	}
	return proposal, nil
}

func (r *AIWorkspaceRepository) ConfirmProposal(
	ctx context.Context,
	target scope.Target,
	id string,
	principalID string,
	expectedVersion int64,
	at time.Time,
) (aiassist.ActionProposal, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return aiassist.ActionProposal{}, err
	}
	var proposal aiassist.ActionProposal
	var input []byte
	err = tx.QueryRow(ctx, `
UPDATE ai_action_proposals
SET state = 'confirmed', confirmed_at = $6, updated_at = $6, version = version + 1
WHERE id = $1 AND msp_id = $2
  AND (
    client_id = NULLIF($3, '')::uuid
    OR (NULLIF($3, '')::uuid IS NULL AND client_id IS NULL)
  )
  AND principal_id = $4 AND version = $5
  AND state = 'pending' AND expires_at > $6
RETURNING id::text, msp_id::text, COALESCE(client_id::text, ''),
          COALESCE(target_client_id::text, ''), principal_id::text,
          tool_name, tool_version, normalized_input,
          required_capability, expires_at, state, confirmed_at,
          correlation_id::text, version, created_at, updated_at
`, id, target.MSPID, target.ClientID, principalID, expectedVersion, at).Scan(
		&proposal.ID, &proposal.MSPID, &proposal.ClientID, &proposal.TargetClientID,
		&proposal.PrincipalID, &proposal.ToolName, &proposal.ToolVersion, &input,
		&proposal.RequiredCapability, &proposal.ExpiresAt, &proposal.State,
		&proposal.ConfirmedAt, &proposal.CorrelationID, &proposal.Version,
		&proposal.CreatedAt, &proposal.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		_ = tx.Rollback(ctx)
		return aiassist.ActionProposal{}, aiassist.ErrProposalConflict
	}
	if err != nil {
		_ = tx.Rollback(ctx)
		return aiassist.ActionProposal{}, err
	}
	proposal.NormalizedInput = append(json.RawMessage(nil), input...)
	if err := tx.Commit(ctx); err != nil {
		_ = tx.Rollback(ctx)
		return aiassist.ActionProposal{}, err
	}
	return proposal, nil
}

func (r *AIWorkspaceRepository) CompleteProposal(
	ctx context.Context,
	target scope.Target,
	id string,
	expectedVersion int64,
	result aiassist.ToolResult,
	at time.Time,
) error {
	body, err := json.Marshal(result)
	if err != nil {
		return err
	}
	return r.updateProposal(ctx, `
UPDATE ai_action_proposals
SET result = $5::jsonb, updated_at = $6, version = version + 1
WHERE id = $1 AND msp_id = $2
  AND (
    client_id = NULLIF($3, '')::uuid
    OR (NULLIF($3, '')::uuid IS NULL AND client_id IS NULL)
  )
  AND version = $4 AND state = 'confirmed' AND result IS NULL
`, []any{id, target.MSPID, target.ClientID, expectedVersion, body, at})
}

func (r *AIWorkspaceRepository) FailProposal(
	ctx context.Context,
	target scope.Target,
	id string,
	expectedVersion int64,
	errorCode string,
	at time.Time,
) error {
	return r.updateProposal(ctx, `
UPDATE ai_action_proposals
SET state = 'failed', failed_at = $6, error_code = $5,
    updated_at = $6, version = version + 1
WHERE id = $1 AND msp_id = $2
  AND (
    client_id = NULLIF($3, '')::uuid
    OR (NULLIF($3, '')::uuid IS NULL AND client_id IS NULL)
  )
  AND version = $4 AND state = 'confirmed' AND result IS NULL
`, []any{id, target.MSPID, target.ClientID, expectedVersion, errorCode, at})
}

func (r *AIWorkspaceRepository) RejectProposal(
	ctx context.Context,
	target scope.Target,
	id string,
	principalID string,
	expectedVersion int64,
	at time.Time,
) error {
	return r.updateProposal(ctx, `
UPDATE ai_action_proposals
SET state = 'rejected', rejected_at = $6, updated_at = $6, version = version + 1
WHERE id = $1 AND msp_id = $2
  AND (
    client_id = NULLIF($3, '')::uuid
    OR (NULLIF($3, '')::uuid IS NULL AND client_id IS NULL)
  )
  AND principal_id = $4 AND version = $5 AND state = 'pending'
  AND expires_at > $6
`, []any{id, target.MSPID, target.ClientID, principalID, expectedVersion, at})
}

func (r *AIWorkspaceRepository) ExpireProposal(
	ctx context.Context,
	target scope.Target,
	id string,
	principalID string,
	expectedVersion int64,
	at time.Time,
) error {
	return r.updateProposal(ctx, `
UPDATE ai_action_proposals
SET state = 'expired', updated_at = $6, version = version + 1
WHERE id = $1 AND msp_id = $2
  AND (
    client_id = NULLIF($3, '')::uuid
    OR (NULLIF($3, '')::uuid IS NULL AND client_id IS NULL)
  )
  AND principal_id = $4 AND version = $5 AND state = 'pending'
  AND expires_at <= $6
`, []any{id, target.MSPID, target.ClientID, principalID, expectedVersion, at})
}

func (r *AIWorkspaceRepository) updateProposal(
	ctx context.Context,
	query string,
	args []any,
) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, query, args...)
	if err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if tag.RowsAffected() != 1 {
		_ = tx.Rollback(ctx)
		return aiassist.ErrProposalConflict
	}
	if err := tx.Commit(ctx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	return nil
}
