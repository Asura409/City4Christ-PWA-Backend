-- +goose Up
CREATE TABLE email_verifications (
    account_id uuid PRIMARY KEY REFERENCES accounts(id) ON DELETE CASCADE,
    challenge_id uuid NOT NULL UNIQUE,
    code_digest bytea,
    expires_at timestamptz NOT NULL,
    last_sent_at timestamptz NOT NULL,
    attempts integer NOT NULL DEFAULT 0 CHECK (attempts BETWEEN 0 AND 5),
    consumed_at timestamptz,
    CHECK (expires_at > last_sent_at),
    CHECK (
        (consumed_at IS NULL AND code_digest IS NOT NULL AND octet_length(code_digest) = 32)
        OR (consumed_at IS NOT NULL AND code_digest IS NULL)
    )
);

-- Only the most recent unsent verification email is queued for an account.
-- Its body is authenticated ciphertext; no plaintext OTP is stored here.
CREATE TABLE verification_email_outbox (
    account_id uuid PRIMARY KEY REFERENCES accounts(id) ON DELETE CASCADE,
    challenge_id uuid NOT NULL UNIQUE,
    sealed_payload bytea NOT NULL CHECK (octet_length(sealed_payload) >= 28),
    created_at timestamptz NOT NULL DEFAULT now()
);
