-- +goose Up
CREATE TABLE member_record_audit_events(
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 actor_id uuid NOT NULL REFERENCES accounts(id),
 member_id uuid NOT NULL REFERENCES accounts(id),
 action text NOT NULL CHECK(action IN ('search_result','detail_view')),
 sensitive_fields text[] NOT NULL CHECK(cardinality(sensitive_fields)>0 AND sensitive_fields <@ ARRAY['email']::text[]),
 occurred_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX member_record_audit_by_member ON member_record_audit_events(member_id,occurred_at DESC,id DESC);
CREATE INDEX member_record_audit_by_actor ON member_record_audit_events(actor_id,occurred_at DESC,id DESC);

-- +goose Down
DROP TABLE member_record_audit_events;
