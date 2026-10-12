-- +goose Up
CREATE TABLE accounts (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    email text NOT NULL,
    first_name text NOT NULL,
    last_name text NOT NULL,
    password_hash text,
    email_verified_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT accounts_email_key UNIQUE (email),
    CONSTRAINT accounts_email_normalized CHECK (email = lower(btrim(email)) AND email <> ''),
    CONSTRAINT accounts_password_not_empty CHECK (password_hash IS NULL OR password_hash <> '')
);
