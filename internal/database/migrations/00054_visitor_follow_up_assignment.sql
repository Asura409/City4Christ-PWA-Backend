-- +goose Up
ALTER TABLE visitor_intakes ADD COLUMN assigned_to uuid REFERENCES accounts(id) ON DELETE SET NULL;
ALTER TABLE visitor_audit_events DROP CONSTRAINT visitor_audit_events_action_check;
ALTER TABLE visitor_audit_events ADD CONSTRAINT visitor_audit_events_action_check
  CHECK (action IN ('submitted','listed','viewed','status_changed','withdrawn','expired','assigned','unassigned'));
ALTER TABLE visitor_audit_events ADD COLUMN previous_assignee_id uuid REFERENCES accounts(id) ON DELETE SET NULL;
ALTER TABLE visitor_audit_events ADD COLUMN assigned_to uuid REFERENCES accounts(id) ON DELETE SET NULL;
ALTER TABLE visitor_audit_events ADD COLUMN reason text NOT NULL DEFAULT '' CHECK (length(reason) <= 500);
CREATE INDEX visitor_intakes_assignee_queue ON visitor_intakes(assigned_to,status,created_at,id)
  WHERE assigned_to IS NOT NULL AND sealed_payload IS NOT NULL;

-- +goose Down
DROP INDEX visitor_intakes_assignee_queue;
ALTER TABLE visitor_audit_events DROP COLUMN reason;
ALTER TABLE visitor_audit_events DROP COLUMN assigned_to;
ALTER TABLE visitor_audit_events DROP COLUMN previous_assignee_id;
ALTER TABLE visitor_audit_events DROP CONSTRAINT visitor_audit_events_action_check;
ALTER TABLE visitor_audit_events ADD CONSTRAINT visitor_audit_events_action_check
  CHECK (action IN ('submitted','listed','viewed','status_changed','withdrawn','expired'));
ALTER TABLE visitor_intakes DROP COLUMN assigned_to;
