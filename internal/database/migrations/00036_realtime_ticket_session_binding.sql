-- +goose Up
-- Existing tickets lack session binding and will fail closed when claimed.
ALTER TABLE realtime_tickets
    ADD COLUMN session_id UUID REFERENCES auth_sessions(id) ON DELETE CASCADE,
    ADD COLUMN access_expires_at TIMESTAMPTZ;

CREATE INDEX realtime_tickets_session_idx ON realtime_tickets(session_id);

-- +goose Down
DROP INDEX realtime_tickets_session_idx;
ALTER TABLE realtime_tickets
    DROP COLUMN access_expires_at,
    DROP COLUMN session_id;
