-- +goose Up
CREATE TABLE notifications (
    sequence bigint GENERATED ALWAYS AS IDENTITY UNIQUE,
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    account_id uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    kind text NOT NULL CHECK (kind ~ '^[a-z][a-z0-9_.-]{1,63}$'),
    title text NOT NULL CHECK (length(title) BETWEEN 1 AND 120),
    body text NOT NULL DEFAULT '' CHECK (length(body) <= 500),
    resource_path text NOT NULL DEFAULT '' CHECK (length(resource_path) <= 500),
    dedupe_key text CHECK (dedupe_key IS NULL OR length(dedupe_key) BETWEEN 1 AND 255),
    created_at timestamptz NOT NULL DEFAULT now(),
    read_at timestamptz
);
CREATE UNIQUE INDEX notifications_dedupe ON notifications(account_id,dedupe_key) WHERE dedupe_key IS NOT NULL;
CREATE INDEX notifications_inbox ON notifications(account_id,sequence DESC);
CREATE INDEX notifications_unread ON notifications(account_id) WHERE read_at IS NULL;

-- +goose Down
DROP TABLE notifications;
