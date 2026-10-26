-- +goose Up
CREATE TABLE post_reactions (
    post_id uuid NOT NULL REFERENCES church_posts(id) ON DELETE CASCADE,
    account_id uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    reaction text NOT NULL CHECK (reaction = 'amen'),
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (post_id, account_id)
);

CREATE INDEX post_reactions_account_idx ON post_reactions(account_id, post_id);

-- +goose Down
DROP TABLE post_reactions;
