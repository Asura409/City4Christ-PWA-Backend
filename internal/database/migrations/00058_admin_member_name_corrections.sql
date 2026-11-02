-- +goose Up
ALTER TABLE accounts
  ADD COLUMN profile_version integer NOT NULL DEFAULT 1 CHECK(profile_version > 0);

ALTER TABLE member_record_audit_events
  ADD COLUMN reason text;

ALTER TABLE member_record_audit_events
  DROP CONSTRAINT member_record_audit_events_action_check,
  DROP CONSTRAINT member_record_audit_events_sensitive_fields_check;

ALTER TABLE member_record_audit_events
  ADD CONSTRAINT member_record_audit_events_action_check
    CHECK (action IN ('search_result','detail_view','audit_view','name_update')),
  ADD CONSTRAINT member_record_audit_events_sensitive_fields_check
    CHECK (
      (action='audit_view' AND cardinality(sensitive_fields)=0 AND reason IS NULL)
      OR
      (action IN ('search_result','detail_view')
       AND cardinality(sensitive_fields)>0
       AND sensitive_fields <@ ARRAY['email']::text[]
       AND reason IS NULL)
      OR
      (action='name_update'
       AND cardinality(sensitive_fields)>0
       AND sensitive_fields <@ ARRAY['firstName','lastName']::text[]
       AND reason IS NOT NULL
       AND length(btrim(reason)) BETWEEN 1 AND 500)
    );

-- +goose Down
DO $$ BEGIN
  IF EXISTS(SELECT 1 FROM member_record_audit_events WHERE action='name_update') THEN
    RAISE EXCEPTION 'cannot remove member name correction audit while events exist';
  END IF;
END $$;

ALTER TABLE member_record_audit_events
  DROP CONSTRAINT member_record_audit_events_action_check,
  DROP CONSTRAINT member_record_audit_events_sensitive_fields_check;

ALTER TABLE member_record_audit_events
  DROP COLUMN reason,
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

ALTER TABLE accounts DROP COLUMN profile_version;
