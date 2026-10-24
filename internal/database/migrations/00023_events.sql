-- +goose Up
CREATE TABLE church_events (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    title text NOT NULL CHECK (length(title) BETWEEN 1 AND 160),
    description text NOT NULL DEFAULT '' CHECK (length(description) <= 5000),
    location text NOT NULL DEFAULT '' CHECK (length(location) <= 300),
    starts_at timestamptz NOT NULL,
    time_zone text NOT NULL,
    capacity integer CHECK (capacity IS NULL OR capacity BETWEEN 1 AND 100000),
    status text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','published','cancelled')),
    version integer NOT NULL DEFAULT 1 CHECK (version > 0),
    created_by uuid NOT NULL REFERENCES accounts(id) ON DELETE RESTRICT,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX church_events_public_schedule ON church_events(starts_at,id) WHERE status='published';
CREATE INDEX church_events_admin_schedule ON church_events(starts_at,id);

CREATE TABLE event_rsvps (
    event_id uuid NOT NULL REFERENCES church_events(id) ON DELETE RESTRICT,
    account_id uuid NOT NULL REFERENCES accounts(id) ON DELETE RESTRICT,
    attendee_count smallint NOT NULL CHECK (attendee_count BETWEEN 1 AND 10),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY(event_id,account_id)
);
CREATE INDEX event_rsvps_by_account ON event_rsvps(account_id,event_id);

CREATE TABLE event_audit_events (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    event_id uuid NOT NULL REFERENCES church_events(id) ON DELETE RESTRICT,
    actor_id uuid NOT NULL REFERENCES accounts(id) ON DELETE RESTRICT,
    action text NOT NULL CHECK (action IN ('created','edited','draft','published','cancelled')),
    version integer NOT NULL CHECK (version > 0),
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX event_audit_events_event ON event_audit_events(event_id,id);

CREATE TABLE event_rsvp_audit_events (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    event_id uuid NOT NULL REFERENCES church_events(id) ON DELETE RESTRICT,
    account_id uuid NOT NULL REFERENCES accounts(id) ON DELETE RESTRICT,
    action text NOT NULL CHECK (action IN ('confirmed','cancelled')),
    attendee_count smallint NOT NULL CHECK (attendee_count BETWEEN 0 AND 10),
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX event_rsvp_audit_events_event ON event_rsvp_audit_events(event_id,id);

-- +goose Down
DROP TABLE event_rsvp_audit_events;
DROP TABLE event_audit_events;
DROP TABLE event_rsvps;
DROP TABLE church_events;
