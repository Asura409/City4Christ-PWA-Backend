-- +goose Up
ALTER TABLE post_comments ADD COLUMN deleted_at timestamptz;

CREATE TABLE post_content_reports (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    reporter_id uuid NOT NULL REFERENCES accounts(id) ON DELETE RESTRICT,
    target_type text NOT NULL CHECK (target_type IN ('post','comment')),
    target_id uuid NOT NULL,
    post_id uuid NOT NULL,
    content_snapshot text NOT NULL,
    reason text NOT NULL CHECK (reason IN ('spam','harassment','inappropriate','hate','threats','other')),
    details text NOT NULL DEFAULT '' CHECK (char_length(details) <= 1000),
    status text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','resolved','dismissed')),
    created_at timestamptz NOT NULL DEFAULT now(),
    closed_at timestamptz,
    closed_by uuid REFERENCES accounts(id) ON DELETE RESTRICT,
    CHECK ((status='pending' AND closed_at IS NULL AND closed_by IS NULL) OR
           (status IN ('resolved','dismissed') AND closed_at IS NOT NULL AND closed_by IS NOT NULL)),
    CHECK ((target_type='post' AND target_id=post_id) OR target_type='comment')
);
CREATE UNIQUE INDEX post_content_reports_pending_reporter_idx
    ON post_content_reports(target_type,target_id,reporter_id) WHERE status='pending';
CREATE INDEX post_content_reports_queue_idx ON post_content_reports(status,created_at,id);
CREATE INDEX post_content_reports_post_idx ON post_content_reports(post_id,status);

CREATE TABLE post_moderation_events (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    report_id uuid NOT NULL REFERENCES post_content_reports(id) ON DELETE RESTRICT,
    actor_id uuid NOT NULL REFERENCES accounts(id) ON DELETE RESTRICT,
    target_type text NOT NULL CHECK (target_type IN ('post','comment')),
    target_id uuid NOT NULL,
    action text NOT NULL CHECK (action IN ('hidden','dismissed')),
    reason text NOT NULL CHECK (char_length(btrim(reason)) BETWEEN 1 AND 500),
    occurred_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX post_moderation_events_target_idx ON post_moderation_events(target_type,target_id,occurred_at,id);

-- +goose Down
DROP TABLE post_moderation_events;
DROP TABLE post_content_reports;
ALTER TABLE post_comments DROP COLUMN deleted_at;
