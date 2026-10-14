-- +goose Up
CREATE TABLE auth_rate_limits (
 bucket bytea PRIMARY KEY CHECK(octet_length(bucket)=32),
 attempts integer NOT NULL CHECK(attempts>0),
 expires_at timestamptz NOT NULL
);
CREATE INDEX auth_rate_limits_expiry_idx ON auth_rate_limits(expires_at);
