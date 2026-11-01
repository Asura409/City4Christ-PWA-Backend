-- +goose Up
CREATE TABLE visitor_intakes (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 submission_id uuid NOT NULL UNIQUE,
 sealed_payload bytea,
 key_id text,
 request_digest bytea,
 status text NOT NULL DEFAULT 'new' CHECK(status IN ('new','contacted','closed','withdrawn','expired')),
 version integer NOT NULL DEFAULT 1 CHECK(version>0),
 consent_version text NOT NULL,
 consent_text text NOT NULL,
 created_at timestamptz NOT NULL,
 updated_at timestamptz NOT NULL,
 expires_at timestamptz NOT NULL,
 CHECK(expires_at=created_at+interval '2160 hours'),
 CHECK((status IN ('withdrawn','expired'))=(sealed_payload IS NULL)),
 CHECK((sealed_payload IS NULL)=(key_id IS NULL)),
 CHECK((sealed_payload IS NULL)=(request_digest IS NULL)),
 CHECK(request_digest IS NULL OR octet_length(request_digest)=32)
);
CREATE INDEX visitor_intakes_queue ON visitor_intakes(status,created_at,id) WHERE sealed_payload IS NOT NULL;
CREATE INDEX visitor_intakes_expiry ON visitor_intakes(expires_at,id) WHERE sealed_payload IS NOT NULL;
CREATE TABLE visitor_audit_events (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 visitor_id uuid NOT NULL REFERENCES visitor_intakes(id),
 actor_id uuid REFERENCES accounts(id),
 action text NOT NULL CHECK(action IN ('submitted','listed','viewed','status_changed','withdrawn','expired')),
 previous_status text NOT NULL DEFAULT '',
 status text NOT NULL,
 version integer NOT NULL,
 occurred_at timestamptz NOT NULL
);
