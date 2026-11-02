-- +goose Up
ALTER TABLE member_record_audit_events
  DROP CONSTRAINT member_record_audit_events_action_check,
  DROP CONSTRAINT member_record_audit_events_sensitive_fields_check;

ALTER TABLE member_record_audit_events
  ADD CONSTRAINT member_record_audit_events_action_check
    CHECK (action IN ('search_result','detail_view','audit_view')),
  ADD CONSTRAINT member_record_audit_events_sensitive_fields_check
    CHECK (
      (action='audit_view' AND cardinality(sensitive_fields)=0)
      OR
      (action IN ('search_result','detail_view')
       AND cardinality(sensitive_fields)>0
       AND sensitive_fields <@ ARRAY['email']::text[])
    );

-- +goose Down
DELETE FROM member_record_audit_events WHERE action='audit_view';

ALTER TABLE member_record_audit_events
  DROP CONSTRAINT member_record_audit_events_action_check,
  DROP CONSTRAINT member_record_audit_events_sensitive_fields_check;

ALTER TABLE member_record_audit_events
  ADD CONSTRAINT member_record_audit_events_action_check
    CHECK (action IN ('search_result','detail_view')),
  ADD CONSTRAINT member_record_audit_events_sensitive_fields_check
    CHECK (cardinality(sensitive_fields)>0 AND sensitive_fields <@ ARRAY['email']::text[]);
