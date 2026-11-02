-- +goose Up
CREATE INDEX sermons_editorial_order ON sermons(created_at DESC,id DESC);

-- +goose Down
DROP INDEX sermons_editorial_order;
