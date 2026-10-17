-- +goose Up
CREATE TABLE access_scopes (
    id text PRIMARY KEY,
    kind text NOT NULL,
    CHECK ((id='' AND kind='global') OR (kind='ministry' AND id ~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'))
);
-- Structural scope only: no account or role is seeded.
INSERT INTO access_scopes(id,kind) VALUES('','global');
ALTER TABLE account_roles DROP CONSTRAINT account_roles_role_check;
ALTER TABLE account_roles DROP CONSTRAINT account_roles_pkey;
ALTER TABLE account_roles ADD COLUMN scope text NOT NULL DEFAULT '' REFERENCES access_scopes(id);
ALTER TABLE account_roles ADD PRIMARY KEY(account_id,role,scope);
ALTER TABLE account_roles ADD CONSTRAINT account_roles_role_scope CHECK (
 (role IN ('content_editor','system_administrator','access_administrator') AND scope='') OR
 (role='ministry_leader' AND scope<>'')
);
CREATE TABLE access_audit_events (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 actor_id uuid NOT NULL REFERENCES accounts(id),
 target_id uuid NOT NULL REFERENCES accounts(id),
 role text NOT NULL,
 scope text NOT NULL REFERENCES access_scopes(id),
 action text NOT NULL CHECK(action IN ('granted','revoked')),
 reason text NOT NULL CHECK(length(btrim(reason)) BETWEEN 1 AND 500),
 occurred_at timestamptz NOT NULL DEFAULT now(),
 CHECK(actor_id<>target_id)
);
CREATE INDEX access_audit_events_target ON access_audit_events(target_id,id DESC);
