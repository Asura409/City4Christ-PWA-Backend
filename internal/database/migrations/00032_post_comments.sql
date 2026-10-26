-- +goose Up
CREATE TABLE post_comments (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    post_id uuid NOT NULL REFERENCES church_posts(id) ON DELETE CASCADE,
    account_id uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    body text NOT NULL CHECK (char_length(body) BETWEEN 1 AND 2000),
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX post_comments_feed_idx ON post_comments(post_id, created_at DESC, id DESC);
CREATE INDEX post_comments_author_idx ON post_comments(account_id, created_at DESC);

-- +goose Down
DROP TABLE post_comments;
