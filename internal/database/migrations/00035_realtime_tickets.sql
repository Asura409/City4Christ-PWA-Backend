-- +goose Up
CREATE TABLE realtime_tickets (
    token_hash BYTEA PRIMARY KEY,
    account_id UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    expires_at TIMESTAMPTZ NOT NULL,
    consumed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT realtime_tickets_hash_size CHECK (octet_length(token_hash) = 32)
);
CREATE INDEX realtime_tickets_expiry_idx ON realtime_tickets(expires_at);
CREATE INDEX realtime_tickets_account_idx ON realtime_tickets(account_id, created_at DESC);

-- +goose Down
DROP TABLE realtime_tickets;
