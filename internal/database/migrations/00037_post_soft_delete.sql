-- +goose Up
ALTER TABLE church_posts ADD COLUMN deleted_at timestamptz;

-- +goose Down
ALTER TABLE church_posts DROP COLUMN deleted_at;
