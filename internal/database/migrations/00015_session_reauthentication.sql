-- +goose Up
-- Preserve sessions and assignments. Existing sessions require confirmation;
-- session creation and token issuance are not evidence of fresh authentication.
ALTER TABLE auth_sessions ADD COLUMN authenticated_at timestamptz;
ALTER TABLE auth_sessions ADD COLUMN reauthenticated_at timestamptz;
ALTER TABLE auth_sessions ADD COLUMN reauthentication_method text
    CHECK (reauthentication_method IN ('password','google'));
ALTER TABLE auth_sessions ADD CONSTRAINT auth_sessions_reauthentication_pair
    CHECK ((reauthenticated_at IS NULL) = (reauthentication_method IS NULL));

ALTER TABLE google_oidc_flows ADD COLUMN reauth_session_id uuid
    REFERENCES auth_sessions(id) ON DELETE CASCADE;
ALTER TABLE google_oidc_flows ADD COLUMN started_at timestamptz;
ALTER TABLE google_oidc_flows ADD CONSTRAINT google_oidc_reauth_purpose CHECK (
    (reauth_session_id IS NULL AND started_at IS NULL) OR
    (reauth_session_id IS NOT NULL AND started_at IS NOT NULL AND link_account_id IS NULL AND link_session_id IS NULL)
);

CREATE TABLE reauthentication_audit_events (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    account_id uuid NOT NULL REFERENCES accounts(id),
    session_id uuid NOT NULL REFERENCES auth_sessions(id),
    method text NOT NULL CHECK (method IN ('password','google')),
    occurred_at timestamptz NOT NULL
);
CREATE INDEX reauthentication_audit_account ON reauthentication_audit_events(account_id,id DESC);
