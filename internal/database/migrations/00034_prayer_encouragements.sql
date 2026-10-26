-- +goose Up
CREATE TABLE prayer_encouragements (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 request_id uuid NOT NULL REFERENCES prayer_requests(id) ON DELETE CASCADE,
 author_id uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
 body text NOT NULL CHECK (length(btrim(body)) BETWEEN 1 AND 1000),
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX prayer_encouragements_request_feed ON prayer_encouragements(request_id,created_at,id);

-- +goose Down
DROP TABLE prayer_encouragements;
