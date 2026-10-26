-- +goose Up
CREATE TABLE media_uploads (
    id uuid PRIMARY KEY,
    owner_id uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    purpose text NOT NULL CHECK (purpose IN ('post_attachment','sermon_attachment')),
    object_key text NOT NULL UNIQUE CHECK (length(object_key) BETWEEN 1 AND 1024),
    available_object_key text UNIQUE CHECK (available_object_key IS NULL OR length(available_object_key) BETWEEN 1 AND 1024),
    declared_content_type text NOT NULL CHECK (length(declared_content_type) BETWEEN 1 AND 127),
    declared_size bigint NOT NULL CHECK (declared_size > 0),
    actual_content_type text,
    actual_size bigint CHECK (actual_size IS NULL OR actual_size > 0),
    status text NOT NULL DEFAULT 'pending_upload' CHECK (status IN (
        'pending_upload','expired','scan_queued','scanning','available','rejected','scan_failed'
    )),
    CHECK ((status = 'available') = (available_object_key IS NOT NULL)),
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    uploaded_at timestamptz,
    scanned_at timestamptz,
    CHECK ((actual_content_type IS NULL) = (actual_size IS NULL)),
    CHECK ((status IN ('pending_upload','expired')) = (uploaded_at IS NULL)),
    CHECK (expires_at > created_at)
);
CREATE INDEX media_uploads_owner_recent ON media_uploads(owner_id, created_at DESC, id DESC);
CREATE INDEX media_uploads_scan_status ON media_uploads(status) WHERE status IN ('scan_queued','scanning');
CREATE INDEX media_uploads_pending_expiry ON media_uploads(expires_at,id) WHERE status='pending_upload';

CREATE TABLE media_scan_jobs (
    id uuid PRIMARY KEY,
    upload_id uuid NOT NULL UNIQUE REFERENCES media_uploads(id) ON DELETE CASCADE,
    status text NOT NULL DEFAULT 'queued' CHECK (status IN ('queued','leased','completed','dead')),
    attempts integer NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    available_at timestamptz NOT NULL DEFAULT now(),
    lease_token uuid,
    lease_expires_at timestamptz,
    last_error_code text CHECK (last_error_code IS NULL OR last_error_code IN ('scanner_unavailable','storage_unavailable')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    finished_at timestamptz,
    CHECK ((status = 'leased') = (lease_token IS NOT NULL AND lease_expires_at IS NOT NULL)),
    CHECK ((status IN ('completed','dead')) = (finished_at IS NOT NULL))
);
CREATE INDEX media_scan_jobs_ready ON media_scan_jobs(available_at, created_at, id) WHERE status='queued';
CREATE INDEX media_scan_jobs_expired_lease ON media_scan_jobs(lease_expires_at, id) WHERE status='leased';

-- +goose Down
DROP TABLE media_scan_jobs;
DROP TABLE media_uploads;
