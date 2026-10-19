-- +goose Up
-- Empty preserves the original JSON callback behavior for existing flows.
ALTER TABLE google_oidc_flows ADD COLUMN return_to text NOT NULL DEFAULT '';
