-- +goose Up
ALTER TABLE account_deletion_requests DROP CONSTRAINT account_deletion_requests_account_id_key;
ALTER TABLE account_deletion_requests DROP CONSTRAINT account_deletion_requests_status_check;
ALTER TABLE account_deletion_requests ADD CONSTRAINT account_deletion_requests_status_check CHECK(status IN ('requested','reviewing','restored'));
CREATE UNIQUE INDEX account_deletion_one_active_request ON account_deletion_requests(account_id) WHERE status IN ('requested','reviewing');
ALTER TABLE account_deletion_audit_events DROP CONSTRAINT account_deletion_audit_events_action_check;
ALTER TABLE account_deletion_audit_events ADD CONSTRAINT account_deletion_audit_events_action_check CHECK(action IN ('requested','listed','viewed','reviewing','restored'));
ALTER TABLE account_deletion_audit_events DROP CONSTRAINT account_deletion_audit_events_status_check;
ALTER TABLE account_deletion_audit_events ADD CONSTRAINT account_deletion_audit_events_status_check CHECK(status IN ('requested','reviewing','restored'));
ALTER TABLE account_deletion_audit_events ADD COLUMN reason text NOT NULL DEFAULT '' CHECK(length(reason)<=500);

-- +goose Down
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM account_deletion_requests WHERE status='restored') OR EXISTS(SELECT 1 FROM account_deletion_requests GROUP BY account_id HAVING count(*)>1) THEN
  RAISE EXCEPTION 'cannot roll back account deletion restoration after requests have been restored or resubmitted';
 END IF;
END $$;
DROP INDEX account_deletion_one_active_request;
ALTER TABLE account_deletion_audit_events DROP COLUMN reason;
ALTER TABLE account_deletion_audit_events DROP CONSTRAINT account_deletion_audit_events_status_check;
ALTER TABLE account_deletion_audit_events ADD CONSTRAINT account_deletion_audit_events_status_check CHECK(status IN ('requested','reviewing'));
ALTER TABLE account_deletion_audit_events DROP CONSTRAINT account_deletion_audit_events_action_check;
ALTER TABLE account_deletion_audit_events ADD CONSTRAINT account_deletion_audit_events_action_check CHECK(action IN ('requested','listed','viewed','reviewing'));
ALTER TABLE account_deletion_requests DROP CONSTRAINT account_deletion_requests_status_check;
ALTER TABLE account_deletion_requests ADD CONSTRAINT account_deletion_requests_status_check CHECK(status IN ('requested','reviewing'));
ALTER TABLE account_deletion_requests ADD CONSTRAINT account_deletion_requests_account_id_key UNIQUE(account_id);
