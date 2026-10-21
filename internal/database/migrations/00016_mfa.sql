-- +goose Up
CREATE TABLE mfa_factors (
 account_id uuid PRIMARY KEY REFERENCES accounts(id),
 id uuid NOT NULL UNIQUE,
 sealed_secret bytea NOT NULL,
 enrollment_session_id uuid NOT NULL REFERENCES auth_sessions(id),
 pending_expires_at timestamptz NOT NULL,
 confirmed_at timestamptz,
 last_counter bigint NOT NULL DEFAULT -1 CHECK(last_counter >= -1)
);
CREATE TABLE mfa_recovery_codes (
 factor_id uuid NOT NULL REFERENCES mfa_factors(id) ON DELETE CASCADE,
 code_digest bytea NOT NULL CHECK(octet_length(code_digest)=32),
 consumed_at timestamptz,
 PRIMARY KEY(factor_id,code_digest)
);
CREATE TABLE mfa_session_proofs (
 session_id uuid PRIMARY KEY REFERENCES auth_sessions(id) ON DELETE CASCADE,
 factor_id uuid NOT NULL REFERENCES mfa_factors(id) ON DELETE CASCADE,
 verified_at timestamptz NOT NULL
);
CREATE TABLE mfa_audit_events (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 account_id uuid NOT NULL REFERENCES accounts(id),
 session_id uuid NOT NULL REFERENCES auth_sessions(id),
 action text NOT NULL CHECK(action IN ('enrollment_started','enabled','totp_verified','recovery_used','recovery_regenerated','disabled')),
 occurred_at timestamptz NOT NULL
);
CREATE INDEX mfa_audit_account ON mfa_audit_events(account_id,id DESC);
-- No factor, proof, code or role is seeded, and existing sessions are unchanged.
