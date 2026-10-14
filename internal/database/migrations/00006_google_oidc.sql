-- +goose Up
CREATE TABLE external_identities (
 provider text NOT NULL CHECK(provider='google'),
 subject text NOT NULL,
 account_id uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(provider,subject),
 UNIQUE(provider,account_id)
);
CREATE TABLE google_oidc_flows (
 state_digest bytea PRIMARY KEY CHECK(octet_length(state_digest)=32),
 browser_digest bytea NOT NULL CHECK(octet_length(browser_digest)=32),
 nonce text NOT NULL,
 pkce_verifier text NOT NULL,
 link_account_id uuid REFERENCES accounts(id) ON DELETE CASCADE,
 link_session_id uuid REFERENCES auth_sessions(id) ON DELETE CASCADE,
 expires_at timestamptz NOT NULL,
 CHECK((link_account_id IS NULL)=(link_session_id IS NULL))
);
CREATE INDEX google_oidc_flows_expiry_idx ON google_oidc_flows(expires_at);
