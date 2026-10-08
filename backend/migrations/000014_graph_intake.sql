-- +goose Up
CREATE TABLE graph_mailbox_connections (
  id uuid PRIMARY KEY,
  msp_id uuid NOT NULL REFERENCES msp_organizations(id),
  mailbox_address text NOT NULL,
  credential_secret_ref text NOT NULL,
  enabled boolean NOT NULL DEFAULT true,
  health_state text NOT NULL DEFAULT 'pending',
  last_success_at timestamptz,
  last_error_code text,
  version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
  created_at timestamptz NOT NULL DEFAULT now(),
  created_by uuid NOT NULL,
  updated_at timestamptz NOT NULL DEFAULT now(),
  updated_by uuid NOT NULL,
  UNIQUE (id, msp_id),
  UNIQUE (msp_id, mailbox_address),
  CHECK (health_state IN ('pending', 'healthy', 'degraded', 'failed', 'disabled'))
);

CREATE TABLE graph_subscriptions (
  id uuid PRIMARY KEY,
  connection_id uuid NOT NULL REFERENCES graph_mailbox_connections(id),
  graph_subscription_id text NOT NULL UNIQUE,
  client_state_secret_ref text NOT NULL,
  resource text NOT NULL,
  expires_at timestamptz NOT NULL,
  last_renewed_at timestamptz NOT NULL,
  last_lifecycle_event text,
  recovery_state text NOT NULL DEFAULT 'none',
  version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
  CHECK (expires_at > last_renewed_at),
  CHECK (recovery_state IN ('none', 'reauthorize', 'recreate_subscription', 'run_delta'))
);

CREATE TABLE graph_delta_cursors (
  id uuid PRIMARY KEY,
  connection_id uuid NOT NULL REFERENCES graph_mailbox_connections(id),
  folder_id text NOT NULL,
  cursor_secret_ref text NOT NULL,
  last_started_at timestamptz,
  last_completed_at timestamptz,
  last_error_code text,
  version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
  UNIQUE (connection_id, folder_id)
);

CREATE TABLE inbound_email_messages (
  id uuid PRIMARY KEY,
  connection_id uuid NOT NULL REFERENCES graph_mailbox_connections(id),
  msp_id uuid NOT NULL REFERENCES msp_organizations(id),
  client_id uuid,
  external_message_id text NOT NULL,
  graph_conversation_id text,
  internet_message_id text,
  in_reply_to text,
  reference_ids jsonb NOT NULL DEFAULT '[]'::jsonb,
  sender_address text NOT NULL,
  subject text NOT NULL,
  received_at timestamptz NOT NULL,
  raw_mime_ref text NOT NULL,
  attachment_refs jsonb NOT NULL DEFAULT '[]'::jsonb,
  normalized_payload jsonb NOT NULL DEFAULT '{}'::jsonb,
  authentication_result text NOT NULL,
  processing_state text NOT NULL DEFAULT 'received',
  error_code text,
  work_record_id uuid,
  thread_match_method text NOT NULL DEFAULT 'none',
  retention_until timestamptz NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (connection_id, external_message_id),
  FOREIGN KEY (client_id, msp_id)
    REFERENCES client_organizations(id, msp_id),
  CHECK (thread_match_method IN ('none', 'graph_conversation', 'internet_message_header', 'ticket_token')),
  CHECK (processing_state IN ('received', 'normalized', 'processed', 'quarantined', 'failed')),
  CHECK (retention_until > received_at)
);

CREATE INDEX inbound_email_thread_conversation_idx
  ON inbound_email_messages (connection_id, graph_conversation_id)
  WHERE graph_conversation_id IS NOT NULL;

CREATE INDEX inbound_email_thread_message_idx
  ON inbound_email_messages (connection_id, internet_message_id)
  WHERE internet_message_id IS NOT NULL;

CREATE INDEX inbound_email_retention_idx
  ON inbound_email_messages (retention_until);

-- +goose Down
DROP TABLE inbound_email_messages;
DROP TABLE graph_delta_cursors;
DROP TABLE graph_subscriptions;
DROP TABLE graph_mailbox_connections;
