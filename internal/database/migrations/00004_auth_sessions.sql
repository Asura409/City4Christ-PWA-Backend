-- +goose Up
CREATE TABLE auth_sessions (
 id uuid PRIMARY KEY,
 account_id uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
 expires_at timestamptz NOT NULL,
 revoked_at timestamptz,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX auth_sessions_account_idx ON auth_sessions(account_id);
CREATE TABLE refresh_tokens (
 token_digest bytea PRIMARY KEY CHECK(octet_length(token_digest)=32),
 session_id uuid NOT NULL REFERENCES auth_sessions(id) ON DELETE CASCADE,
 created_at timestamptz NOT NULL,
 consumed_at timestamptz
);
CREATE INDEX refresh_tokens_session_idx ON refresh_tokens(session_id);
CREATE INDEX auth_sessions_expiry_idx ON auth_sessions(expires_at);
