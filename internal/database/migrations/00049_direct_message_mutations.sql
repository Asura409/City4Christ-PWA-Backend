-- +goose Up
ALTER TABLE direct_messages DROP CONSTRAINT direct_messages_body_check;
ALTER TABLE direct_messages
    ADD COLUMN edited_at timestamptz,
    ADD COLUMN deleted_at timestamptz,
    ADD COLUMN deleted_by uuid REFERENCES accounts(id) ON DELETE RESTRICT,
    ADD CONSTRAINT direct_messages_body_valid CHECK (
        (deleted_at IS NULL AND deleted_by IS NULL AND char_length(body) BETWEEN 1 AND 4000 AND octet_length(body) <= 16384)
        OR (deleted_at IS NOT NULL AND deleted_by=sender_id AND body='')
    );

CREATE TABLE direct_message_events (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    message_id uuid NOT NULL REFERENCES direct_messages(id) ON DELETE CASCADE,
    actor_id uuid NOT NULL REFERENCES accounts(id) ON DELETE RESTRICT,
    action text NOT NULL CHECK (action IN ('edited','deleted')),
    occurred_at timestamptz NOT NULL
);
CREATE INDEX direct_message_events_message ON direct_message_events(message_id,id);

-- +goose StatementBegin
DO $$
DECLARE constraint_name text;
BEGIN
    SELECT conname INTO constraint_name FROM pg_constraint
    WHERE conrelid='media_uploads'::regclass AND contype='c'
      AND pg_get_constraintdef(oid) LIKE '%pending_upload%scan_queued%';
    IF constraint_name IS NOT NULL THEN
        EXECUTE format('ALTER TABLE media_uploads DROP CONSTRAINT %I', constraint_name);
    END IF;
    SELECT conname INTO constraint_name FROM pg_constraint
    WHERE conrelid='media_uploads'::regclass AND contype='c'
      AND pg_get_constraintdef(oid) LIKE '%available_object_key%'
      AND pg_get_constraintdef(oid) LIKE '%status%available%';
    IF constraint_name IS NOT NULL THEN
        EXECUTE format('ALTER TABLE media_uploads DROP CONSTRAINT %I', constraint_name);
    END IF;
END $$;
-- +goose StatementEnd

ALTER TABLE media_uploads ADD CONSTRAINT media_uploads_status_check CHECK (status IN (
    'pending_upload','expired','scan_queued','scanning','available','rejected','scan_failed','deleting','deleted'
));
ALTER TABLE media_uploads ADD CONSTRAINT media_uploads_object_status_check CHECK (
    (status IN ('available','deleting')) = (available_object_key IS NOT NULL)
);

CREATE TABLE media_object_cleanup_jobs (
    id uuid PRIMARY KEY,
    upload_id uuid NOT NULL UNIQUE REFERENCES media_uploads(id) ON DELETE CASCADE,
    object_key text NOT NULL UNIQUE CHECK (length(object_key) BETWEEN 1 AND 1024),
    status text NOT NULL DEFAULT 'queued' CHECK (status IN ('queued','leased','dead')),
    attempts integer NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    available_at timestamptz NOT NULL DEFAULT now(),
    lease_token uuid,
    lease_expires_at timestamptz,
    last_error_code text CHECK (last_error_code IS NULL OR last_error_code='storage_unavailable'),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CHECK ((status='leased') = (lease_token IS NOT NULL AND lease_expires_at IS NOT NULL))
);
CREATE INDEX media_object_cleanup_ready ON media_object_cleanup_jobs(available_at,created_at,id) WHERE status='queued';
CREATE INDEX media_object_cleanup_expired_lease ON media_object_cleanup_jobs(lease_expires_at,id) WHERE status='leased';

-- +goose Down
DROP TABLE media_object_cleanup_jobs;
ALTER TABLE media_uploads DROP CONSTRAINT media_uploads_object_status_check;
ALTER TABLE media_uploads DROP CONSTRAINT media_uploads_status_check;
UPDATE media_uploads SET status='scan_failed' WHERE status IN ('deleting','deleted');
ALTER TABLE media_uploads ADD CONSTRAINT media_uploads_status_check CHECK (status IN (
    'pending_upload','expired','scan_queued','scanning','available','rejected','scan_failed'
));
ALTER TABLE media_uploads ADD CONSTRAINT media_uploads_available_check CHECK (
    (status='available') = (available_object_key IS NOT NULL)
);
DROP TABLE direct_message_events;
ALTER TABLE direct_messages DROP CONSTRAINT direct_messages_body_valid;
ALTER TABLE direct_messages DROP COLUMN deleted_by;
ALTER TABLE direct_messages DROP COLUMN deleted_at;
ALTER TABLE direct_messages DROP COLUMN edited_at;
ALTER TABLE direct_messages ADD CONSTRAINT direct_messages_body_check
    CHECK (char_length(body) BETWEEN 1 AND 4000 AND octet_length(body) <= 16384);
