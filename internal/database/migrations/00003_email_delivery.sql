-- +goose Up
ALTER TABLE verification_email_outbox
    ADD COLUMN delivery_attempts integer NOT NULL DEFAULT 0 CHECK (delivery_attempts BETWEEN 0 AND 5),
    ADD COLUMN next_attempt_at timestamptz NOT NULL DEFAULT now(),
    ADD COLUMN failed_at timestamptz,
    ADD COLUMN last_error_code text;

UPDATE verification_email_outbox SET next_attempt_at = created_at;

CREATE INDEX verification_email_outbox_due_idx ON verification_email_outbox(next_attempt_at)
    WHERE failed_at IS NULL;
