-- +goose Up
CREATE TABLE church_posts (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 author_id uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
 body text NOT NULL CHECK (length(btrim(body)) BETWEEN 1 AND 5000),
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX church_posts_feed ON church_posts(created_at DESC,id DESC);

-- +goose Down
DROP TABLE church_posts;
