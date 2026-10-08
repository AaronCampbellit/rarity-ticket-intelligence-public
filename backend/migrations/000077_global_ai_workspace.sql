-- +goose Up
CREATE TABLE ai_conversations (
  id uuid PRIMARY KEY,
  msp_id uuid NOT NULL REFERENCES msp_organizations(id),
  client_id uuid,
  principal_id uuid NOT NULL,
  title text NOT NULL CHECK (length(btrim(title)) > 0),
  archived_at timestamptz,
  version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (id, msp_id),
  FOREIGN KEY (client_id, msp_id) REFERENCES client_organizations(id, msp_id),
  FOREIGN KEY (principal_id, msp_id) REFERENCES technicians(id, msp_id)
);

CREATE INDEX ai_conversations_principal_scope_idx
  ON ai_conversations (msp_id, principal_id, client_id, updated_at DESC)
  WHERE archived_at IS NULL;

CREATE TABLE ai_messages (
  id uuid PRIMARY KEY,
  msp_id uuid NOT NULL REFERENCES msp_organizations(id),
  client_id uuid,
  conversation_id uuid NOT NULL,
  role text NOT NULL CHECK (role IN ('user', 'assistant', 'tool', 'system')),
  safe_text text NOT NULL,
  referenced_objects jsonb NOT NULL DEFAULT '{}'::jsonb,
  provider_job_id uuid,
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (id, msp_id),
  FOREIGN KEY (client_id, msp_id) REFERENCES client_organizations(id, msp_id),
  FOREIGN KEY (conversation_id, msp_id)
    REFERENCES ai_conversations(id, msp_id) ON DELETE CASCADE,
  CHECK (jsonb_typeof(referenced_objects) = 'object')
);

CREATE INDEX ai_messages_conversation_time_idx
  ON ai_messages (msp_id, conversation_id, created_at, id);

CREATE TABLE ai_product_documents (
  id uuid PRIMARY KEY,
  source_key text NOT NULL,
  source_version text NOT NULL,
  section text NOT NULL,
  audience text NOT NULL CHECK (audience IN ('all_users', 'administrators')),
  body text NOT NULL,
  search_vector tsvector GENERATED ALWAYS AS
    (to_tsvector('english', coalesce(section, '') || ' ' || coalesce(body, ''))) STORED,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (source_key, source_version, section)
);

CREATE INDEX ai_product_documents_search_idx
  ON ai_product_documents USING gin (search_vector);

INSERT INTO ai_product_documents (
  id, source_key, source_version, section, audience, body
) VALUES
  (
    '77000000-0000-4000-8000-000000000001',
    'docs/02-platform/tickets.md', '2026-08-04', 'Work records and tickets',
    'all_users',
    'Incidents, service requests, problems, and changes share one work record foundation. Work is client scoped and includes deterministic routing, governed workflow transitions, ownership, collaborators, internal notes, client-visible replies, attachments, time, tasks, relationships, search, audit evidence, and optimistic version checks.'
  ),
  (
    '77000000-0000-4000-8000-000000000002',
    'docs/02-platform/queues.md', '2026-08-04', 'Queues and routing',
    'all_users',
    'Queues are operational routing destinations such as New Tickets, Backup Alerts, VIP Support, After-Hours, or Escalations. Until claimed, the queue is responsible for the work. Routing decisions are deterministic and explainable; claiming, returning, transferring, and escalation are audited.'
  ),
  (
    '77000000-0000-4000-8000-000000000003',
    'docs/02-platform/clients.md', '2026-08-04', 'Client boundaries',
    'all_users',
    'Clients are isolation, reporting, workflow, contract, and visibility boundaries. Locations, contacts, assets, services, and contracts belong to an active client context. Client lifecycle changes are governed because they affect isolation and retained operational history.'
  ),
  (
    '77000000-0000-4000-8000-000000000004',
    'docs/02-platform/projects.md', '2026-08-04', 'Projects and change orders',
    'all_users',
    'Projects are native PSA objects made of ordered phases and phase tasks. Accepted proposal conversion is atomic and preserves the original commercial baseline. Versioned change orders alter current scope, price, or planned hours without rewriting that baseline, and approvals or reasoned overrides retain immutable evidence.'
  ),
  (
    '77000000-0000-4000-8000-000000000005',
    'docs/02-platform/knowledge-base.md', '2026-08-04', 'Knowledge',
    'all_users',
    'Knowledge articles are versioned and permission aware. V1 knowledge is internal only, supports drafts and version history, and can be related to clients, assets, services, work, and problems. Search and AI may recommend only articles the technician is authorized to see.'
  ),
  (
    '77000000-0000-4000-8000-000000000006',
    'docs/02-platform/integrations.md', '2026-08-04', 'Integrations',
    'administrators',
    'Connections hold encrypted configuration, health, rate limits, and audit evidence. Microsoft Graph and forwarding provide email intake, webhooks provide signed delivery, Datto RMM is read and ingest only, and integration health exposes safe state and remediation evidence. Least privilege and explicit client scope are required.'
  ),
  (
    '77000000-0000-4000-8000-000000000007',
    'docs/09-roadmap/implementation-plan.md', '2026-08-04', 'Timers and timesheets',
    'all_users',
    'Technicians can run independent durable ticket timers, stop them into server-owned captures, and consume each capture once into a time entry with an immutable labor-role rate snapshot. Monday-based weekly timesheets show daily and weekly totals. Time Reviewers can amend pending entries, approve or reject, and reverse and replace approved entries.'
  ),
  (
    '77000000-0000-4000-8000-000000000008',
    'docs/07-ui-ux/first-run-and-help.md', '2026-08-04', 'Setup Center',
    'administrators',
    'The Setup Center remains available after onboarding. It distinguishes saved configuration from live verification, explains missing requirements in plain language, links to the owning administration page, and reports storage, backup, identity, and intake readiness without exposing protected values.'
  )
ON CONFLICT (source_key, source_version, section) DO NOTHING;

CREATE TABLE ai_action_proposals (
  id uuid PRIMARY KEY,
  msp_id uuid NOT NULL REFERENCES msp_organizations(id),
  client_id uuid,
  conversation_id uuid NOT NULL,
  message_id uuid,
  principal_id uuid NOT NULL,
  tool_name text NOT NULL CHECK (length(btrim(tool_name)) > 0),
  tool_version integer NOT NULL CHECK (tool_version > 0),
  normalized_input jsonb NOT NULL,
  preview jsonb NOT NULL,
  target_type text NOT NULL CHECK (length(btrim(target_type)) > 0),
  target_id uuid NOT NULL,
  target_version bigint CHECK (target_version > 0),
  required_capability text NOT NULL CHECK (length(btrim(required_capability)) > 0),
  expires_at timestamptz NOT NULL,
  state text NOT NULL DEFAULT 'pending'
    CHECK (state IN ('pending', 'confirmed', 'rejected', 'expired', 'failed')),
  confirmed_at timestamptz,
  rejected_at timestamptz,
  failed_at timestamptz,
  result jsonb,
  error_code text,
  correlation_id uuid NOT NULL,
  version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (id, msp_id),
  FOREIGN KEY (client_id, msp_id) REFERENCES client_organizations(id, msp_id),
  FOREIGN KEY (conversation_id, msp_id)
    REFERENCES ai_conversations(id, msp_id) ON DELETE CASCADE,
  FOREIGN KEY (message_id, msp_id)
    REFERENCES ai_messages(id, msp_id),
  FOREIGN KEY (principal_id, msp_id) REFERENCES technicians(id, msp_id),
  CHECK (
    (state = 'confirmed' AND confirmed_at IS NOT NULL)
    OR (state = 'rejected' AND rejected_at IS NOT NULL)
    OR (state = 'failed' AND failed_at IS NOT NULL)
    OR (state IN ('pending', 'expired'))
  )
);

CREATE INDEX ai_action_proposals_pending_idx
  ON ai_action_proposals (msp_id, principal_id, expires_at)
  WHERE state = 'pending';

CREATE INDEX ai_action_proposals_conversation_idx
  ON ai_action_proposals (msp_id, conversation_id, created_at DESC);

-- +goose Down
DROP TABLE ai_action_proposals;
DROP TABLE ai_product_documents;
DROP TABLE ai_messages;
DROP TABLE ai_conversations;
