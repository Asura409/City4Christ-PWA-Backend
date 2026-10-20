-- +goose Up
CREATE TABLE password_recoveries (
 account_id uuid PRIMARY KEY REFERENCES accounts(id) ON DELETE CASCADE,
 challenge_id uuid NOT NULL UNIQUE,
 token_digest bytea CHECK (octet_length(token_digest)=32),
 expires_at timestamptz NOT NULL,
 last_sent_at timestamptz NOT NULL,
 consumed_at timestamptz
);
CREATE UNIQUE INDEX password_recovery_token_idx ON password_recoveries(token_digest) WHERE token_digest IS NOT NULL;
CREATE TABLE password_recovery_outbox (
 id uuid PRIMARY KEY,
 account_id uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
 kind text NOT NULL CHECK (kind IN ('reset','changed')),
 sealed_payload bytea NOT NULL,
 expires_at timestamptz NOT NULL,
 created_at timestamptz NOT NULL,
 delivery_attempts integer NOT NULL DEFAULT 0 CHECK (delivery_attempts BETWEEN 0 AND 5),
 next_attempt_at timestamptz NOT NULL,
 failed_at timestamptz,
 last_error_code text
);
CREATE INDEX password_recovery_outbox_due_idx ON password_recovery_outbox(next_attempt_at) WHERE failed_at IS NULL;
