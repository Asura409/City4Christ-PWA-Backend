-- +goose Up
-- Existing and new accounts opt out of directory listing by default.
ALTER TABLE accounts ADD COLUMN directory_visible boolean NOT NULL DEFAULT false;
