-- +goose Up
CREATE TABLE groups (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 name text NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 120),
 description text NOT NULL DEFAULT '' CHECK (length(description) <= 2000),
 audience text NOT NULL CHECK (audience IN ('church','leadership')),
 join_policy text NOT NULL CHECK (join_policy IN ('open','approval','closed')),
 created_by uuid NOT NULL REFERENCES accounts(id),
 archived_at timestamptz,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX groups_single_leadership_audience ON groups(audience)
 WHERE audience='leadership' AND archived_at IS NULL;
CREATE TABLE group_memberships (
 group_id uuid NOT NULL REFERENCES groups(id),
 account_id uuid NOT NULL REFERENCES accounts(id),
 status text NOT NULL CHECK (status IN ('pending','active','declined','left')),
 requested_at timestamptz NOT NULL DEFAULT now(),
 decided_at timestamptz,
 decided_by uuid REFERENCES accounts(id),
 PRIMARY KEY (group_id,account_id)
);
CREATE INDEX group_memberships_active ON group_memberships(group_id) WHERE status='active';
CREATE INDEX group_memberships_pending ON group_memberships(group_id,requested_at) WHERE status='pending';
CREATE TABLE group_audit_events (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 group_id uuid NOT NULL REFERENCES groups(id),
 actor_id uuid NOT NULL REFERENCES accounts(id),
 target_id uuid REFERENCES accounts(id),
 action text NOT NULL CHECK (action IN ('requested','joined','approved','declined','left')),
 reason text NOT NULL DEFAULT '' CHECK (length(reason) <= 500),
 occurred_at timestamptz NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE group_audit_events;
DROP TABLE group_memberships;
DROP TABLE groups;
