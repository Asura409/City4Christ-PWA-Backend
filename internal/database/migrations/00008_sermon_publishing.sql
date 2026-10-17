-- +goose Up
CREATE TABLE account_roles (
    account_id uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    role text NOT NULL CHECK (role IN ('content_editor','system_administrator')),
    granted_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY(account_id,role)
);
-- No role is granted automatically. Assignment is an explicit operator action.
ALTER TABLE sermons ADD COLUMN video_url text NOT NULL DEFAULT '';
ALTER TABLE sermons ADD COLUMN version integer NOT NULL DEFAULT 1 CHECK (version > 0);
ALTER TABLE sermons ADD CONSTRAINT sermons_video_url_safe CHECK (
    video_url = '' OR video_url ~ '^https://www[.]youtube[.]com/watch[?]v=[A-Za-z0-9_-]{11}$'
);
CREATE TABLE sermon_audit_events (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    sermon_id uuid NOT NULL REFERENCES sermons(id),
    actor_id uuid NOT NULL REFERENCES accounts(id),
    action text NOT NULL CHECK (action IN ('created','updated','published','scheduled','unpublished')),
    version integer NOT NULL CHECK (version > 0),
    status text NOT NULL CHECK (status IN ('draft','published','scheduled')),
    occurred_at timestamptz NOT NULL,
    UNIQUE(sermon_id,version)
);
CREATE INDEX sermon_audit_events_actor ON sermon_audit_events(actor_id,occurred_at DESC);
