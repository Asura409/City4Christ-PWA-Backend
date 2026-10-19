-- +goose Up
-- Index only the members eligible for directory listing; never opt accounts in.
CREATE INDEX accounts_directory_visible_id ON accounts(id)
WHERE directory_visible AND email_verified_at IS NOT NULL;
