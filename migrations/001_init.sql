CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TYPE execution_status AS ENUM (
  'pending', 'ready', 'running', 'completed', 'failed', 'cancelled'
);

CREATE TYPE event_type AS ENUM (
  'submitted', 'ready', 'claimed', 'heartbeat',
  'completed', 'failed', 'lease_expired', 'stale_rejected',
  'retry_scheduled', 'cancelled', 'duplicate_detected'
);

-- Tool registry (what tools this server exposes)
CREATE TABLE tools (
  id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  name          TEXT NOT NULL UNIQUE,
  description   TEXT NOT NULL,
  input_schema  JSONB NOT NULL,
  max_attempts  INT NOT NULL DEFAULT 3,
  lease_seconds INT NOT NULL DEFAULT 30,
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- One row per tool call attempt
CREATE TABLE executions (
  id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  namespace        TEXT NOT NULL,
  tool_name        TEXT NOT NULL REFERENCES tools(name),
  idempotency_key  TEXT NOT NULL,
  input_args       JSONB NOT NULL,
  status           execution_status NOT NULL DEFAULT 'pending',
  attempts         INT NOT NULL DEFAULT 0,
  max_attempts     INT NOT NULL DEFAULT 3,
  result           JSONB,
  error_message    TEXT,
  created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (namespace, tool_name, idempotency_key)
);

-- Active lease per execution (at most one row per execution)
CREATE TABLE execution_leases (
  execution_id   UUID PRIMARY KEY REFERENCES executions(id),
  worker_id      TEXT NOT NULL,
  fencing_token  BIGINT NOT NULL,
  lease_expires  TIMESTAMPTZ NOT NULL,
  claimed_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Immutable event log -- never update, only insert
CREATE TABLE execution_events (
  id             BIGSERIAL PRIMARY KEY,
  execution_id   UUID NOT NULL REFERENCES executions(id),
  event_type     event_type NOT NULL,
  worker_id      TEXT,
  fencing_token  BIGINT,
  payload        JSONB,
  occurred_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Retry schedule
CREATE TABLE retry_schedule (
  execution_id  UUID PRIMARY KEY REFERENCES executions(id),
  retry_at      TIMESTAMPTZ NOT NULL,
  attempt       INT NOT NULL
);

CREATE INDEX idx_executions_status ON executions(status);
CREATE INDEX idx_executions_idempotency ON executions(namespace, tool_name, idempotency_key);
CREATE INDEX idx_events_execution ON execution_events(execution_id, occurred_at);
CREATE INDEX idx_retry_schedule_at ON retry_schedule(retry_at);
