-- +goose Up
ALTER TABLE prayer_requests
  ADD COLUMN expires_at timestamptz NOT NULL DEFAULT (now() + interval '180 days'),
  ADD COLUMN archive_reason text CHECK (archive_reason IN ('owner','moderation','expiry'));
CREATE INDEX prayer_requests_expiry ON prayer_requests(expires_at,id)
  WHERE status <> 'archived';

CREATE TABLE prayer_reports (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  request_id uuid NOT NULL REFERENCES prayer_requests(id) ON DELETE CASCADE,
  reporter_id uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  reason text NOT NULL CHECK (reason IN ('safety','spam','inappropriate','other')),
  details text NOT NULL DEFAULT '' CHECK (length(details) <= 4000),
  status text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','resolved','dismissed','expired')),
  created_at timestamptz NOT NULL DEFAULT now(),
  closed_at timestamptz,
  closed_by uuid REFERENCES accounts(id) ON DELETE SET NULL,
  UNIQUE(request_id,reporter_id)
);
CREATE INDEX prayer_reports_queue ON prayer_reports(status,created_at,id);
CREATE INDEX prayer_reports_request ON prayer_reports(request_id,status);

CREATE TABLE prayer_moderation_events (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  request_id uuid NOT NULL REFERENCES prayer_requests(id) ON DELETE CASCADE,
  report_id uuid REFERENCES prayer_reports(id) ON DELETE SET NULL,
  actor_id uuid REFERENCES accounts(id) ON DELETE SET NULL,
  action text NOT NULL CHECK (action IN ('hidden','dismissed','expired')),
  reason text NOT NULL DEFAULT '' CHECK (length(reason) <= 500),
  occurred_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX prayer_moderation_events_request ON prayer_moderation_events(request_id,id);

-- +goose Down
DROP TABLE prayer_moderation_events;
DROP TABLE prayer_reports;
DROP INDEX prayer_requests_expiry;
ALTER TABLE prayer_requests DROP COLUMN archive_reason;
ALTER TABLE prayer_requests DROP COLUMN expires_at;
