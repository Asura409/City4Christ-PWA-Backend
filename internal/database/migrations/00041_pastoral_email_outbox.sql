-- +goose Up
-- Metadata only: recipient addresses and care details are not copied into the queue.
CREATE TABLE pastoral_email_outbox (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 booking_id uuid NOT NULL REFERENCES pastor_bookings(id),
 recipient_id uuid NOT NULL REFERENCES accounts(id),
 booking_version integer NOT NULL CHECK(booking_version>0),
 event text NOT NULL CHECK(event IN ('requested','approved','declined','cancelled','reminder_24h','reminder_1h')),
 scheduled_at timestamptz NOT NULL,
 expires_at timestamptz NOT NULL CHECK(expires_at>scheduled_at),
 next_attempt_at timestamptz NOT NULL,
 delivery_attempts integer NOT NULL DEFAULT 0 CHECK(delivery_attempts BETWEEN 0 AND 5),
 sent_at timestamptz,
 failed_at timestamptz,
 discarded_at timestamptz,
 last_error_code text,
 UNIQUE(booking_id,booking_version,event,recipient_id)
);
CREATE INDEX pastoral_email_due ON pastoral_email_outbox(next_attempt_at,id)
 WHERE sent_at IS NULL AND failed_at IS NULL AND discarded_at IS NULL;
