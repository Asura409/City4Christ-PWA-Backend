-- +goose Up
CREATE TABLE sermon_playback_progress (
    account_id uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    sermon_id uuid NOT NULL REFERENCES sermons(id) ON DELETE CASCADE,
    position_seconds integer NOT NULL CHECK (position_seconds BETWEEN 0 AND 86400),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (account_id, sermon_id)
);

CREATE TABLE sermon_notes (
    account_id uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    sermon_id uuid NOT NULL REFERENCES sermons(id) ON DELETE CASCADE,
    body text NOT NULL CHECK (char_length(btrim(body)) BETWEEN 1 AND 10000),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (account_id, sermon_id)
);

-- +goose Down
DROP TABLE sermon_notes;
DROP TABLE sermon_playback_progress;
