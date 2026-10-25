-- +goose Up
CREATE TABLE event_coordinators (
    event_id uuid NOT NULL REFERENCES church_events(id) ON DELETE RESTRICT,
    group_id uuid NOT NULL REFERENCES groups(id) ON DELETE RESTRICT,
    account_id uuid NOT NULL REFERENCES accounts(id) ON DELETE RESTRICT,
    assigned_by uuid NOT NULL REFERENCES accounts(id) ON DELETE RESTRICT,
    assigned_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (event_id, account_id),
    FOREIGN KEY (group_id, account_id) REFERENCES group_memberships(group_id, account_id) ON DELETE RESTRICT
);
CREATE INDEX event_coordinators_group ON event_coordinators(group_id,account_id);

CREATE TABLE event_coordinator_audit_events (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    event_id uuid NOT NULL REFERENCES church_events(id) ON DELETE RESTRICT,
    group_id uuid NOT NULL REFERENCES groups(id) ON DELETE RESTRICT,
    account_id uuid NOT NULL REFERENCES accounts(id) ON DELETE RESTRICT,
    actor_id uuid NOT NULL REFERENCES accounts(id) ON DELETE RESTRICT,
    action text NOT NULL CHECK (action IN ('assigned','removed')),
    occurred_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX event_coordinator_audit_event ON event_coordinator_audit_events(event_id,id);

-- +goose Down
DROP TABLE event_coordinator_audit_events;
DROP TABLE event_coordinators;
